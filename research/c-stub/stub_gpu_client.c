/*
 * stub_gpu_client.c — LD_PRELOAD ioctl interceptor for Turnip gen8
 *
 * Usage:
 *   export TU_STUB_SOCKET=/tmp/tu_stub_gpu.sock
 *   export TU_STUB_GPU=1
 *   export TU_STUB_TRACE_DWORDS=0   # set to 1 to trace actual PM4 dwords
 *   LD_PRELOAD=./libstub_gpu_client.so vulkan.turnip.so
 *
 * Architecture:
 *   Turnip → ioctl() → [stub intercept] → Unix socket → Go GPU Engine
 *                                                           │
 *                    Returns success + fence values ←───────┘
 *
 * Tracked ioctls:
 *   IOCTL_KGSL_GPU_COMMAND        → send metadata to Go engine, fake success
 *   IOCTL_KGSL_SUBMIT_COMMANDS    → send metadata to Go engine, fake success
 *   IOCTL_KGSL_GPUMEM_ALLOC_ID    → track BO for future dword reading
 *   IOCTL_KGSL_DRAWCTXT_CREATE    → track context id
 *   IOCTL_KGSL_DEVICE_WAITTIMESTAMP_CTXTID → fake completion
 *   IOCTL_KGSL_SYNCIOBJ_WAIT      → fake completion
 *   IOCTL_KGSL_DEVICE_GETPROPERTY → fake device info
 *   IOCTL_KGSL_GPUMEM_ALLOC       → track BO
 *   IOCTL_KGSL_GPUMEM_FREE        → untrack BO
 *   others → pass through or return success
 */

#define _GNU_SOURCE
#include <dlfcn.h>
#include <errno.h>
#include <fcntl.h>
#include <stdarg.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/ioctl.h>
#include <sys/mman.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <sys/un.h>
#include <time.h>
#include <unistd.h>

/* KGSL ioctl definitions (mirrored from msm_kgsl.h) */
#define KGSL_IOC_TYPE 0x09

struct kgsl_command_object {
    uint64_t offset;
    uint64_t gpuaddr;
    uint64_t size;
    unsigned int flags;
    unsigned int id;
};

struct kgsl_gpu_command {
    uint64_t flags;
    uint64_t cmdlist;
    unsigned int cmdsize;
    unsigned int numcmds;
    uint64_t objlist;
    unsigned int objsize;
    unsigned int numobjs;
    uint64_t synclist;
    unsigned int syncsize;
    unsigned int numsyncs;
    unsigned int context_id;
    unsigned int timestamp;
};

struct kgsl_submit_commands {
    uint64_t flags;
    uint64_t cmdlist;
    unsigned int cmdsize;
    unsigned int numcmds;
    uint64_t synclist;
    unsigned int syncsize;
    unsigned int numsyncs;
    unsigned int context_id;
};

struct kgsl_gpumem_alloc_id {
    uint64_t size;
    unsigned int flags;
    int id;
    uint64_t gpuaddr;
    unsigned int padding;
};

/* Wait timestamp */
struct kgsl_wait_timestamp {
    unsigned int context_id;
    unsigned int timestamp;
    unsigned int timeout;
};

/* KGSL ioctl numbers */
#define _KGSL_IOC(nr, type, size) _IOC(_IOC_READ|_IOC_WRITE, KGSL_IOC_TYPE, nr, size)
#define IOCTL_KGSL_DEVICE_GETPROPERTY      _KGSL_IOC(0x02, struct { int type; void *value; unsigned int sizebytes; }, 16)
#define IOCTL_KGSL_DRAWCTXT_CREATE         _KGSL_IOC(0x13, struct { unsigned int flags; unsigned int drawctxt_id; }, 8)
#define IOCTL_KGSL_DRAWCTXT_DESTROY        _KGSL_IOC(0x14, struct { unsigned int drawctxt_id; }, 4)
#define IOCTL_KGSL_GPUMEM_ALLOC_ID         _KGSL_IOC(0x34, struct kgsl_gpumem_alloc_id, 32)
#define IOCTL_KGSL_GPUMEM_ALLOC            _KGSL_IOC(0x2f, struct { uint64_t gpuaddr; uint64_t size; unsigned int flags; int id; unsigned int mmapsize; uint64_t padding; }, 48)
#define IOCTL_KGSL_GPUMEM_FREE             _KGSL_IOC(0x30, struct { uint64_t gpuaddr; unsigned int padding; }, 12)
#define IOCTL_KGSL_DEVICE_WAITTIMESTAMP_CTXTID _KGSL_IOC(0x06, struct kgsl_wait_timestamp, 12)
#define IOCTL_KGSL_GPU_COMMAND             _KGSL_IOC(0x4A, struct kgsl_gpu_command, 72)
#define IOCTL_KGSL_SUBMIT_COMMANDS         _KGSL_IOC(0x3D, struct kgsl_submit_commands, 64)

/* ------------------------------------------------------------------ */
/* State tracking */
/* ------------------------------------------------------------------ */

#define MAX_TRACKED_BOS 4096
#define MAX_SOCKET_BUF (4 * 1024 * 1024)

static struct {
    int  in_use;
    uint64_t gpuaddr;
    uint64_t size;
    void  *userptr;   /* mmap'd user-space mapping, if known */
} g_tracked_bos[MAX_TRACKED_BOS];

static int   g_stub_enabled = 0;
static int   g_trace_dwords = 0;
static int   g_socket_fd = -1;
static int   g_kgsl_fd = -1;
static unsigned int g_context_id = 0;
static unsigned int g_fake_timestamp = 1;
static int   (*real_ioctl)(int, unsigned long, ...) = NULL;
static FILE  *g_logfile = NULL;

/* Forward decls */
static void stub_log(const char *fmt, ...);
static int  connect_to_engine(void);
static void track_bo(uint64_t gpuaddr, uint64_t size);
static void *find_bo_userptr(uint64_t gpuaddr);

/* ------------------------------------------------------------------ */
/* Constructor — called on LD_PRELOAD */
/* ------------------------------------------------------------------ */

__attribute__((constructor))
static void stub_init(void)
{
    real_ioctl = dlsym(RTLD_NEXT, "ioctl");
    if (!real_ioctl) {
        fprintf(stderr, "stub_gpu_client: dlsym ioctl failed\n");
        return;
    }

    const char *stub_env = getenv("TU_STUB_GPU");
    if (!stub_env || stub_env[0] != '1') return;

    g_stub_enabled = 1;

    if (getenv("TU_STUB_TRACE_DWORDS"))
        g_trace_dwords = atoi(getenv("TU_STUB_TRACE_DWORDS"));

    g_logfile = fopen("/tmp/stub_gpu_client.log", "a");
    stub_log("=== stub_gpu_client init ===\n");
    stub_log("trace_dwords=%d\n", g_trace_dwords);

    if (connect_to_engine() < 0) {
        stub_log("WARN: Go engine not reachable, using local stub\n");
    }
}

__attribute__((destructor))
static void stub_fini(void)
{
    if (g_socket_fd >= 0) close(g_socket_fd);
    if (g_logfile) fclose(g_logfile);
}

/* ------------------------------------------------------------------ */
/* ioctl() interceptor */
/* ------------------------------------------------------------------ */

int ioctl(int fd, unsigned long request, ...)
{
    va_list ap;
    va_start(ap, request);
    void *arg = va_arg(ap, void*);
    va_end(ap);
    /* Pass through non-stub path */
    if (!g_stub_enabled) {
        if (real_ioctl)
            return real_ioctl(fd, request, arg);
        errno = ENOSYS;
        return -1;
    }

    /* Non-KGSL fd or non-KGSL ioctl → pass through */
    if (_IOC_TYPE(request) != KGSL_IOC_TYPE) {
        return real_ioctl(fd, request, arg);
    }

    /* Track KGSL fd */
    if (g_kgsl_fd < 0) g_kgsl_fd = fd;

    int nr = _IOC_NR(request);

    stub_log("KGSL ioctl nr=%d (0x%02lx) fd=%d\n", nr, request & 0xFF, fd);

    switch (nr) {
    case 0x02: /* DEVICE_GETPROPERTY */
        return 0; /* success */

    case 0x13: { /* DRAWCTXT_CREATE */
        struct { unsigned int flags; unsigned int drawctxt_id; } *p = arg;
        g_context_id = p->drawctxt_id = 1;
        return 0;
    }

    case 0x14: /* DRAWCTXT_DESTROY */
        return 0;

    case 0x06: { /* WAITTIMESTAMP_CTXTID */
        /* Fake: timestamp is always immediately ready */
        struct kgsl_wait_timestamp *w = arg;
        w->timestamp = g_fake_timestamp;
        return 0;
    }

    case 0x34: { /* GPUMEM_ALLOC_ID */
        struct kgsl_gpumem_alloc_id *alloc = arg;
        int ret = real_ioctl(fd, request, arg);
        if (ret == 0) {
            track_bo(alloc->gpuaddr, alloc->size);
            stub_log("  ALLOC_ID gpuaddr=0x%lx size=%lu\n",
                     (unsigned long)alloc->gpuaddr, (unsigned long)alloc->size);
        }
        return ret;
    }

    case 0x2f: { /* GPUMEM_ALLOC */
        struct { uint64_t gpuaddr; uint64_t size; unsigned int flags; int id; unsigned int mmapsize; uint64_t padding; } *alloc = arg;
        int ret = real_ioctl(fd, request, arg);
        if (ret == 0) {
            track_bo(alloc->gpuaddr, alloc->size);
            stub_log("  ALLOC gpuaddr=0x%lx size=%lu\n",
                     (unsigned long)alloc->gpuaddr, (unsigned long)alloc->size);
        }
        return ret;
    }

    case 0x30: /* GPUMEM_FREE */
        return real_ioctl(fd, request, arg);

    case 0x4A: { /* GPU_COMMAND */
        struct kgsl_gpu_command *cmd = arg;
        stub_log("  GPU_COMMAND numcmds=%u context=%u\n",
                 cmd->numcmds, cmd->context_id);

        if (g_socket_fd >= 0) {
            int n = cmd->numcmds;
            if (n > 256) n = 256;

            struct kgsl_command_object cmds[256];
            if (cmd->cmdlist) {
                memcpy(cmds, (void*)(uintptr_t)cmd->cmdlist,
                       n * sizeof(struct kgsl_command_object));
            }

            char *buf = malloc(MAX_SOCKET_BUF);
            if (buf) {
                char *p = buf;
                p += sprintf(p, "{\"submit_id\":%u,\"entries\":[", g_fake_timestamp);

                for (int i = 0; i < n; i++) {
                    uint32_t dword_count = (uint32_t)(cmds[i].size / 4);
                    if (dword_count > 0x10000) dword_count = 0x10000;

                    if (i > 0) *p++ = ',';
                    p += sprintf(p, "{\"size\":%u", dword_count * 4);

                    if (g_trace_dwords) {
                        void *userptr = find_bo_userptr(cmds[i].gpuaddr);
                        if (userptr) {
                            p += sprintf(p, ",\"dwords\":[");
                            uint32_t *dw = (uint32_t*)userptr;
                            for (unsigned int j = 0; j < dword_count && j < 512; j++) {
                                if (j > 0) *p++ = ',';
                                p += sprintf(p, "%u", dw[j]);
                            }
                            *p++ = ']';
                        }
                    }
                    *p++ = '}';
                }

                p += sprintf(p, "],\"fences\":[{\"id\":0,\"seqno\":%u}]}",
                            g_fake_timestamp);
                send(g_socket_fd, buf, p - buf, 0);

                char resp[4096];
                recv(g_socket_fd, resp, sizeof(resp)-1, MSG_DONTWAIT);
                free(buf);
            }
        }

        cmd->timestamp = g_fake_timestamp++;
        return 0;
    }

    case 0x3D: /* SUBMIT_COMMANDS */
        return 0; /* success, fake timestamp */

    /* Sync object operations — pass through or fake */
    case 0x40: /* SYNCIOBJ_CREATE */
    case 0x41: /* SYNCIOBJ_DESTROY */
    case 0x42: /* SYNCIOBJ_WAIT */
    case 0x43: /* SYNCIOBJ_SIGNAL */
        return real_ioctl(fd, request, arg);

    default:
        stub_log("  unhandled KGSL ioctl nr=%d, passing through\n", nr);
        return real_ioctl(fd, request, arg);
    }

    return 0;
}

/* ------------------------------------------------------------------ */
/* Engine communication */
/* ------------------------------------------------------------------ */

static int connect_to_engine(void)
{
    const char *sock_path = getenv("TU_STUB_SOCKET");
    if (!sock_path) sock_path = "/tmp/tu_stub_gpu.sock";

    g_socket_fd = socket(AF_UNIX, SOCK_STREAM, 0);
    if (g_socket_fd < 0) return -1;

    struct sockaddr_un addr;
    memset(&addr, 0, sizeof(addr));
    addr.sun_family = AF_UNIX;
    strncpy(addr.sun_path, sock_path, sizeof(addr.sun_path)-1);

    if (connect(g_socket_fd, (struct sockaddr*)&addr, sizeof(addr)) < 0) {
        close(g_socket_fd);
        g_socket_fd = -1;
        return -1;
    }

    stub_log("Connected to Go engine at %s\n", sock_path);
    return 0;
}

/* ------------------------------------------------------------------ */
/* BO tracking */
/* ------------------------------------------------------------------ */

static void track_bo(uint64_t gpuaddr, uint64_t size)
{
    for (int i = 0; i < MAX_TRACKED_BOS; i++) {
        if (!g_tracked_bos[i].in_use) {
            g_tracked_bos[i].in_use = 1;
            g_tracked_bos[i].gpuaddr = gpuaddr;
            g_tracked_bos[i].size = size;
            g_tracked_bos[i].userptr = NULL;
            return;
        }
    }
}

static void *find_bo_userptr(uint64_t gpuaddr)
{
    for (int i = 0; i < MAX_TRACKED_BOS; i++) {
        if (g_tracked_bos[i].in_use &&
            g_tracked_bos[i].gpuaddr == gpuaddr) {
            return g_tracked_bos[i].userptr;
        }
    }
    return NULL;
}

/* Override mmap to track user-space mappings of GPU BOs */
void *mmap(void *addr, size_t length, int prot, int flags, int fd, off_t offset)
{
    static void *(*real_mmap)(void*, size_t, int, int, int, off_t) = NULL;
    if (!real_mmap) real_mmap = dlsym(RTLD_NEXT, "mmap");

    void *result = real_mmap(addr, length, prot, flags, fd, offset);

    if (g_stub_enabled && result != MAP_FAILED && fd == g_kgsl_fd) {
        /* This is a GPU BO mmap — track its user-space address */
        for (int i = 0; i < MAX_TRACKED_BOS; i++) {
            if (g_tracked_bos[i].in_use &&
                g_tracked_bos[i].userptr == NULL &&
                g_tracked_bos[i].size <= length) {
                g_tracked_bos[i].userptr = result;
                stub_log("  mmap tracked BO[%d] gpuaddr=0x%lx → userptr=%p\n",
                         i, (unsigned long)g_tracked_bos[i].gpuaddr, result);
                break;
            }
        }
    }
    return result;
}

/* ------------------------------------------------------------------ */
/* Logging */
/* ------------------------------------------------------------------ */

static void stub_log(const char *fmt, ...)
{
    if (!g_logfile) return;
    va_list ap;
    va_start(ap, fmt);
    vfprintf(g_logfile, fmt, ap);
    fflush(g_logfile);
    va_end(ap);
}
