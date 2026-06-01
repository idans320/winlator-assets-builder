/*
 * libstub_kgsl.so - LD_PRELOAD ioctl interceptor for Turnip KGSL testing.
 *
 * Intercepts ioctl() from vulkan.turnip.so and provides a fake GPU.
 * Controlled via environment variables:
 *   STUB_KGSL_WAIT_MODE=success|timeout|edeadlk|badf
 *   STUB_KGSL_WAIT_EDEADLK_COUNT=3   (fail N times, then succeed)
 */
#define _GNU_SOURCE
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <dlfcn.h>
#include <errno.h>
#include <stdarg.h>
#include <unistd.h>
#include <pthread.h>

/* KGSL ioctl magic */
#define KGSL_IOC_TYPE   0x09

#ifndef _IOWR
#define _IOWR(type,nr,size)  (((size) << 16) | ((type) << 8) | (nr) | 0xC0000000)
#define _IOW(type,nr,size)   (((size) << 16) | ((type) << 8) | (nr) | 0x40000000)
#endif

/* ---- Stub state ---- */
static int kgsl_fd              = -1;
static int kgsl_ioctl_counts[256] = {0};
static int edeadlk_remaining    = 0;
static int wait_mode            = 0;
static int fake_ts              = 1000;

/* ---- Real ioctl function pointer ---- */
#ifdef __BIONIC__
  typedef int ioctl_req_t;
  #define IOCTL_REAL_DECL  int fd, ioctl_req_t request, ...
  #define IOCTL_OVERRIDE   int fd, int request, ...
  #define IOCTL_CALL(fd,req,arg) real_ioctl(fd, (ioctl_req_t)(req), arg)
#else
  typedef unsigned long ioctl_req_t;
  #define IOCTL_REAL_DECL  int fd, ioctl_req_t request, ...
  #define IOCTL_OVERRIDE   int fd, unsigned long request, ...
  #define IOCTL_CALL(fd,req,arg) real_ioctl(fd, req, arg)
#endif

typedef int (*real_ioctl_t)(IOCTL_REAL_DECL);
static real_ioctl_t real_ioctl;

/* ---- Initialization ---- */
__attribute__((constructor))
static void stub_init(void) {
    real_ioctl = (real_ioctl_t)dlsym(RTLD_NEXT, "ioctl");
    if (!real_ioctl) {
        fprintf(stderr, "STUB_KGSL: dlsym ioctl failed: %s\n", dlerror());
        _exit(1);
    }

    const char *mode = getenv("STUB_KGSL_WAIT_MODE");
    if (mode) {
        if (!strcmp(mode, "timeout"))  wait_mode = 1;
        else if (!strcmp(mode, "edeadlk")) wait_mode = 2;
        else if (!strcmp(mode, "badf")) wait_mode = 3;
    }
    const char *cnt = getenv("STUB_KGSL_WAIT_EDEADLK_COUNT");
    if (cnt) edeadlk_remaining = atoi(cnt);

    fprintf(stderr, "STUB_KGSL: loaded wait_mode=%d edeadlk_count=%d\n",
            wait_mode, edeadlk_remaining);
}

/* ---- KGSL ioctl name for logging ---- */
static const char *kgsl_name(unsigned long req) {
    switch (req) {
    case 0xC0094001: return "GETPROPERTY";
    case 0x40040902: return "SETPROPERTY";
    case 0xC0100903: return "WAITTIMESTAMP";
    case 0xC0180906: return "WAITTIMESTAMP_CTXTID";
    case 0xC038093D: return "SUBMIT_COMMANDS";
    case 0xC040093E: return "GPU_COMMAND";
    case 0xC0A0093F: return "GPUOBJ_ALLOC";
    case 0x400C0940: return "GPUOBJ_FREE";
    case 0xC0180941: return "GPUOBJ_INFO";
    case 0xC0180942: return "GPUOBJ_IMPORT";
    case 0xC0280943: return "GPUMEM_ALLOC_ID";
    case 0x40080944: return "GPUMEM_FREE_ID";
    case 0xC0100945: return "GPUMEM_BIND_RANGES";
    case 0xC0180946: return "GPUMEM_GET_INFO";
    case 0xC0200949: return "DRAWCTXT_CREATE";
    case 0x400C094A: return "DRAWCTXT_DESTROY";
    case 0xC090094B: return "TIMESTAMP_EVENT";
    default: return "UNKNOWN";
    }
}

/* ---- ioctl override ---- */
int ioctl(IOCTL_OVERRIDE) {
    va_list ap;
    va_start(ap, request);
    void *arg = va_arg(ap, void*);
    va_end(ap);

    unsigned long req = (unsigned long)request;

    /* Track first KGSL fd */
    if (kgsl_fd == -1 && req >= 0xC0000000) {
        if (((req >> 8) & 0xFF) == KGSL_IOC_TYPE)
            kgsl_fd = fd;
    }

    /* Pass through non-KGSL ioctls */
    if (fd != kgsl_fd || ((req >> 8) & 0xFF) != KGSL_IOC_TYPE) {
        return IOCTL_CALL(fd, request, arg);
    }

    int idx = req & 0xFF;
    kgsl_ioctl_counts[idx]++;

    /* ---- Handle WAITTIMESTAMP_CTXTID (the fence wait path) ---- */
    if (req == 0xC0180906) {
        if (wait_mode == 3) {
            errno = EBADF;
            return -1;
        }
        if (edeadlk_remaining > 0) {
            edeadlk_remaining--;
            errno = EDEADLK;
            return -1;
        }
        if (wait_mode == 2) {
            errno = EDEADLK;
            return -1;
        }
        if (wait_mode == 1) {
            errno = ETIMEDOUT;
            return -1;
        }
        /* success: write fake fence timestamp */
        unsigned int *ts = (unsigned int*)((char*)arg + sizeof(unsigned int));
        *ts = fake_ts++;
        return 0;
    }

    /* ---- Other KGSL ioctls: return success with stub data ---- */
    switch (req) {
    case 0xC0280943: /* GPUMEM_ALLOC_ID */
        *(unsigned int*)arg = 0x10000000;
        return 0;
    case 0xC0094001: /* GETPROPERTY */
        if (arg) *(unsigned int*)arg = 0;
        return 0;
    /* Non-data ioctls: just succeed */
    case 0xC040093E: /* GPU_COMMAND */
    case 0xC038093D: /* SUBMIT_COMMANDS */
    case 0xC0A0093F: /* GPUOBJ_ALLOC */
    case 0x400C0940: /* GPUOBJ_FREE */
    case 0xC0180941: /* GPUOBJ_INFO */
    case 0xC0180942: /* GPUOBJ_IMPORT */
    case 0x40080944: /* GPUMEM_FREE_ID */
    case 0xC0100945: /* GPUMEM_BIND_RANGES */
    case 0xC0180946: /* GPUMEM_GET_INFO */
    case 0xC0200949: /* DRAWCTXT_CREATE */
    case 0x400C094A: /* DRAWCTXT_DESTROY */
    case 0xC090094B: /* TIMESTAMP_EVENT */
    case 0x40040902: /* SETPROPERTY */
        return 0;
    }

    fprintf(stderr, "STUB_KGSL: unhandled ioctl fd=%d req=0x%lx (%s)\n",
            fd, req, kgsl_name(req));
    return 0;
}
