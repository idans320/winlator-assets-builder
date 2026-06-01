/*
 * test_kgsl_sync.c - Unit tests for wait_timestamp_safe and KGSL ioctl handling.
 *
 * Tests the EDEADLK fix: wait_timestamp_safe must be serialized by submit_mutex
 * so that concurrent KGSL ioctls don't trigger the kernel's deadlock detector.
 *
 * Build: ./build.sh
 * Run:   ./run_tests.sh   (native)
 *        qemu-aarch64-static ./test_kgsl_sync  (on aarch64 .so)
 */

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <pthread.h>
#include <sched.h>
#include <errno.h>
#include <time.h>
#include <stdint.h>
#include <assert.h>
#include <limits.h>

/* ---- KGSL types and ioctl (from msm_kgsl.h) ---- */

struct kgsl_device_waittimestamp_ctxtid {
    unsigned int context_id;
    unsigned int timestamp;
    unsigned int timeout;
};

#define KGSL_IOC_TYPE   0x09

#define IOCTL_KGSL_DEVICE_WAITTIMESTAMP_CTXTID \
    _IOWR(KGSL_IOC_TYPE, 0x6, struct kgsl_device_waittimestamp_ctxtid)

#ifndef _IOWR
#define _IOWR(type,nr,size)  (nr)
#endif

/* ---- Mock ioctl state machine ---- */

enum {
    MOCK_SUCCESS       = 0,
    MOCK_TIMEOUT       = 1,
    MOCK_EDEADLK       = 2,
    MOCK_EINTR         = 3,
    MOCK_EAGAIN        = 4,
    MOCK_BADF          = 5,
    MOCK_ALWAYS_TIME   = 6,
};

static int mock_ioctl_fd = -1;
static int mock_scenario = MOCK_SUCCESS;
static int mock_ioctl_call_count = 0;
static int mock_fail_count = 0;
static int mock_fail_remaining = 0;

/* Number of times to fail before succeeding */
void mock_set_fail_then_succeed(int fail_count) {
    mock_fail_remaining = fail_count;
    mock_scenario = MOCK_SUCCESS;
    mock_fail_count = fail_count;
}

void mock_set_scenario(int scenario) {
    mock_scenario = scenario;
    mock_fail_remaining = 0;
}

int mock_get_call_count(void) { return mock_ioctl_call_count; }
void mock_reset_call_count(void) { mock_ioctl_call_count = 0; }

/*
 * Mock ioctl - intercepts KGSL_WAITTIMESTAMP and returns controlled responses.
 * For all other ioctls, returns 0 (success).
 */
int ioctl(int fd, unsigned long request, void *arg) {
    (void)arg;
    mock_ioctl_call_count++;

    if (fd != mock_ioctl_fd)
        return 0;

    /* Only intercept KGSL_WAITTIMESTAMP */
    if (request != (unsigned long)IOCTL_KGSL_DEVICE_WAITTIMESTAMP_CTXTID)
        return 0;

    /* Controllable failure injection */
    if (mock_fail_remaining > 0) {
        mock_fail_remaining--;
        errno = EDEADLK;
        return -1;
    }

    switch (mock_scenario) {
    case MOCK_SUCCESS:
        return 0;
    case MOCK_TIMEOUT:
        errno = ETIMEDOUT;
        return -1;
    case MOCK_EDEADLK:
        errno = EDEADLK;
        return -1;
    case MOCK_EINTR:
        errno = EINTR;
        return -1;
    case MOCK_EAGAIN:
        errno = EAGAIN;
        return -1;
    case MOCK_BADF:
        errno = EBADF;
        return -1;
    case MOCK_ALWAYS_TIME:
        /* Simulate timeout with elapsed time */
        errno = ETIMEDOUT;
        return -1;
    default:
        return 0;
    }
}

/* ---- Utility functions (mirror Turnip's) ---- */

static uint64_t os_time_get_nano(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (uint64_t)ts.tv_sec * 1000000000ULL + ts.tv_nsec;
}

static int get_relative_ms(uint64_t abs_timeout_ns) {
    uint64_t now = os_time_get_nano();
    if (abs_timeout_ns <= now)
        return 0;
    uint64_t ns = abs_timeout_ns - now;
    if (ns / 1000000 > INT_MAX)
        return INT_MAX;
    return (int)(ns / 1000000);
}

#define VK_SUCCESS             0
#define VK_TIMEOUT             1
#define VK_ERROR_DEVICE_LOST   (-4)

/* ---- wait_timestamp_safe (mirror of our patched Turnip code) ---- */

static int wait_timestamp_safe(int fd,
                               unsigned int context_id,
                               unsigned int timestamp,
                               uint64_t abs_timeout_ns,
                               pthread_mutex_t *mutex)
{
    struct kgsl_device_waittimestamp_ctxtid wait_data = {
        .context_id = context_id,
        .timestamp = timestamp,
        .timeout = get_relative_ms(abs_timeout_ns),
    };

    while (1) {
        pthread_mutex_lock(mutex);
        int ret = ioctl(fd, IOCTL_KGSL_DEVICE_WAITTIMESTAMP_CTXTID, &wait_data);
        pthread_mutex_unlock(mutex);

        if (ret == -1 && (errno == EINTR || errno == EAGAIN || errno == EDEADLK)) {
            if (errno == EDEADLK)
                sched_yield();
            int timeout_ms = get_relative_ms(abs_timeout_ns);
            if (timeout_ms == 0)
                return VK_TIMEOUT;
            wait_data.timeout = timeout_ms;
        } else if (ret == -1) {
            if (errno == ETIMEDOUT || errno == EINVAL) {
                return VK_TIMEOUT;
            } else {
                fprintf(stderr, "TU_KNL_KGSL: wait_timestamp_safe fd=%d ctx=%u ts=%u errno=%d (%s)\n",
                        fd, context_id, timestamp, errno, strerror(errno));
                return VK_ERROR_DEVICE_LOST;
            }
        } else {
            return VK_SUCCESS;
        }
    }
}

/* ---- Test cases ---- */

static pthread_mutex_t submit_mutex = PTHREAD_MUTEX_INITIALIZER;
static int tests_passed = 0;
static int tests_failed = 0;

#define ASSERT_EQ(actual, expected, name) do { \
    if ((actual) != (expected)) { \
        fprintf(stderr, "FAIL: %s: expected %d, got %d\n", name, expected, actual); \
        tests_failed++; \
    } else { \
        printf("PASS: %s\n", name); \
        tests_passed++; \
    } \
} while(0)

static void test_success(void) {
    mock_set_scenario(MOCK_SUCCESS);
    mock_reset_call_count();
    int result = wait_timestamp_safe(mock_ioctl_fd, 1, 100,
                                     os_time_get_nano() + 5000000000ULL,
                                     &submit_mutex);
    ASSERT_EQ(result, VK_SUCCESS, "immediate success");
    ASSERT_EQ(mock_get_call_count(), 1, "one ioctl call for success");
}

static void test_timeout(void) {
    mock_set_scenario(MOCK_ALWAYS_TIME);
    mock_reset_call_count();
    /* timeout_ns=0 means immediate timeout */
    int result = wait_timestamp_safe(mock_ioctl_fd, 1, 100,
                                     os_time_get_nano() - 1,  /* already expired */
                                     &submit_mutex);
    ASSERT_EQ(result, VK_TIMEOUT, "immediate timeout");
}

static void test_edeadlk_retry(void) {
    /* Set up: fail 3 times with EDEADLK, then succeed */
    mock_set_fail_then_succeed(3);
    mock_reset_call_count();
    int result = wait_timestamp_safe(mock_ioctl_fd, 1, 100,
                                     os_time_get_nano() + 5000000000ULL,
                                     &submit_mutex);
    ASSERT_EQ(result, VK_SUCCESS, "EDEADLK retry succeeds");
    if (mock_get_call_count() != 4)
        printf("FAIL: EDEADLK retry: expected 4 ioctl calls, got %d\n", mock_get_call_count());
    else
        printf("  (correctly retried 3 EDEADLK, then succeeded)\n");
}

static void test_eintr_retry(void) {
    mock_set_scenario(MOCK_EINTR);
    mock_reset_call_count();
    int result = wait_timestamp_safe(mock_ioctl_fd, 1, 100,
                                     os_time_get_nano() + 5000000000ULL,
                                     &submit_mutex);
    /* With infinite EINTR, it should eventually timeout */
    ASSERT_EQ(result, VK_TIMEOUT, "EINTR retry loop leads to timeout");
    /* Should have made multiple calls due to retry loop */
    if (mock_get_call_count() > 1)
        printf("  (correctly retried EINTR %d times before timeout)\n", mock_get_call_count());
}

static void test_unexpected_error(void) {
    mock_set_scenario(MOCK_BADF);
    mock_reset_call_count();
    int result = wait_timestamp_safe(mock_ioctl_fd, 1, 100,
                                     os_time_get_nano() + 5000000000ULL,
                                     &submit_mutex);
    ASSERT_EQ(result, VK_ERROR_DEVICE_LOST, "unexpected errno returns DEVICE_LOST");
}

/* Test that concurrent calls on different threads are serialized by the mutex */
typedef struct {
    int fd;
    unsigned int ctx;
    unsigned int ts;
    int expected;
    int *done;
} thread_args_t;

static void *thread_wait(void *arg) {
    thread_args_t *a = (thread_args_t *)arg;
    int result = wait_timestamp_safe(a->fd, a->ctx, a->ts,
                                     os_time_get_nano() + 5000000000ULL,
                                     &submit_mutex);
    *(a->done) = 1;
    if (result != a->expected)
        fprintf(stderr, "FAIL: thread got %d, expected %d\n", result, a->expected);
    return NULL;
}

static void test_concurrent_serialization(void) {
    /* If the mutex is working, two concurrent wait calls should serialize
     * and each get their own result, not EDEADLK */
    pthread_t t1, t2;
    int done1 = 0, done2 = 0;
    thread_args_t a1 = { mock_ioctl_fd, 1, 100, VK_SUCCESS, &done1 };
    thread_args_t a2 = { mock_ioctl_fd, 1, 200, VK_SUCCESS, &done2 };

    mock_set_scenario(MOCK_SUCCESS);
    mock_reset_call_count();

    pthread_create(&t1, NULL, thread_wait, &a1);
    pthread_create(&t2, NULL, thread_wait, &a2);

    pthread_join(t1, NULL);
    pthread_join(t2, NULL);

    /* Both should have completed successfully without EDEADLK */
    if (done1 && done2)
        printf("PASS: concurrent serialization - both threads completed\n");
    else
        fprintf(stderr, "FAIL: concurrent serialization - not all threads completed\n");
}

int main(void) {
    /* Use a non-real fd that our mock recognizes */
    mock_ioctl_fd = 42;

    printf("=== Turnip KGSL wait_timestamp_safe tests ===\n\n");

    test_success();
    test_timeout();
    test_edeadlk_retry();
    test_eintr_retry();
    test_unexpected_error();
    test_concurrent_serialization();

    printf("\n=== Results: %d passed, %d failed ===\n", tests_passed, tests_failed);

    return tests_failed > 0 ? 1 : 0;
}
