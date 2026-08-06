# Change 04: KGSL Wait Timestamp Robustness

**File:** `tu_knl_kgsl.cc` (+24/-12)

## The Problem

`wait_timestamp_safe()` is the core GPU synchronization primitive — it blocks
until the GPU completes a specific command (timestamp). Vanilla Mesa has two issues:

### 1. No Mutex Protection
Multiple threads can simultaneously call `wait_timestamp_safe()` on the same
device, creating a race on the KGSL ioctl. The ioctl modifies the timeout
in-place (`wait.timeout = timeout_ms`) — if two threads interleave:

```
Thread A: ioctl(fd, WAITTIMESTAMP, &wait)  →  GPU not done, wait.timeout -= 5ms
Thread B: ioctl(fd, WAITTIMESTAMP, &wait)  →  reads Thread A's modified timeout!
```

### 2. Crash on Unexpected Errors
```c
// Vanilla:
assert(errno == ETIMEDOUT);  // CRASH on any other error
return VK_TIMEOUT;
```

If the KGSL ioctl returns `EINVAL`, `EBADF`, `ENODEV`, or any unexpected errno,
the driver asserts and crashes the application.

## Fork Fixes

### Mutex-Guarded ioctl
All call sites pass `&device->submit_mutex`:

```c
static VkResult
wait_timestamp_safe(int fd, unsigned int context_id,
                    unsigned int timestamp, uint64_t abs_timeout_ns,
                    pthread_mutex_t *mutex)  // NEW parameter
{
    while (true) {
        pthread_mutex_lock(mutex);           // HOLD during ioctl
        int ret = ioctl(fd, IOCTL_KGSL_DEVICE_WAITTIMESTAMP_CTXTID, &wait);
        pthread_mutex_unlock(mutex);

        if (ret == -1 && (errno == EINTR || errno == EAGAIN || errno == EDEADLK)) {
            if (errno == EDEADLK)
                sched_yield();              // mutex protocol: yield and retry
            // ... update timeout ...
        } else if (ret == -1) {
            // ... handle error ...
        }
    }
}
```

### Graceful Error Handling
```c
// Fork: log error and return DEVICE_LOST instead of crashing
if (errno == ETIMEDOUT || errno == EINVAL) {
    return VK_TIMEOUT;
} else {
    fprintf(stderr, "TU_KNL_KGSL: wait_timestamp_safe "
            "fd=%d ctx=%u ts=%u errno=%d (%s)\n",
            fd, context_id, timestamp, errno, strerror(errno));
    return VK_ERROR_DEVICE_LOST;
}
```

### EDEADLK Handling
When a thread holds `submit_mutex` and another thread is waiting on the ioctl,
the kernel returns `EDEADLK` to break the deadlock. The fork handles this by:
1. Calling `sched_yield()` to give the mutex holder CPU time
2. Recalculating the remaining timeout
3. Retrying the ioctl

## Call Sites Changed

All 4 call sites updated to pass the mutex:
- `tu_queue_wait_for_idle` → `&queue->device->submit_mutex`
- `tu_syncobj_wait_one` → `&device->submit_mutex`
- `tu_WaitForFences` → `&device->submit_mutex`
- `tu_knl_signal_by_timestamp` → `&device->submit_mutex`

## Why This Matters on Android

Android's KGSL driver is more aggressive about returning errors than desktop
Linux DRM drivers:
- **`EINVAL`** can occur when the GPU context is in a bad state (e.g., after
  a GPU hang recovery)
- **`EDEADLK`** is Android-specific — desktop DRM doesn't use this protocol
- **Race conditions** between submission and waiting are more common due to
  Android's multi-threaded rendering model (separate render/upload threads)
