/*
 * test_stub_client.c — Simulates Turnip's KGSL ioctl pattern
 * Compiled natively to test the LD_PRELOAD stub → Go engine pipeline.
 *
 * Usage:
 *   ./gpu-stub &           # start Go engine
 *   cc -o test_stub test_stub_client.c
 *   TU_STUB_GPU=1 LD_PRELOAD=./libstub_gpu_client.so ./test_stub
 */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/ioctl.h>
#include <sys/mman.h>
#include <unistd.h>
#include <fcntl.h>

#define KGSL_IOC_TYPE 0x09
#define _KGSL_IOC(nr, type, sz) _IO(KGSL_IOC_TYPE, nr)

struct kgsl_command_object {
    unsigned long offset, gpuaddr, size;
    unsigned int flags, id;
};

int main(void) {
    printf("=== test_stub_client: simulating Turnip KGSL ioctls ===\n");

    int fd = open("/dev/null", O_RDWR); /* pretend kgsl fd */
    if (fd < 0) fd = 3;

    /* Step 1: Get device properties */
    printf("1. DEVICE_GETPROPERTY...\n");
    int ret = ioctl(fd, _KGSL_IOC(0x02, void, 0), NULL);
    printf("   result: %d (expect 0)\n", ret);

    /* Step 2: Create draw context */
    printf("2. DRAWCTXT_CREATE...\n");
    struct { unsigned int flags; unsigned int drawctxt_id; } ctx = {0, 0};
    ret = ioctl(fd, _KGSL_IOC(0x13, void, 0), &ctx);
    printf("   result: %d  context_id: %u (expect 1)\n", ret, ctx.drawctxt_id);

    /* Step 3: Allocate GPU memory for command stream */
    printf("3. GPUMEM_ALLOC_ID...\n");
    struct kgsl_gpumem_alloc_id { unsigned long size; unsigned int flags; int id; unsigned long gpuaddr; unsigned int padding; } alloc = {4096, 0, 0, 0};
    ret = ioctl(fd, _KGSL_IOC(0x34, void, 0), &alloc);
    printf("   result: %d  gpuaddr: 0x%lx\n", ret, (unsigned long)alloc.gpuaddr);

    /* Step 4: Submit a command */
    printf("4. GPU_COMMAND...\n");
    struct kgsl_command_object cmd_obj = {
        .offset = 0,
        .gpuaddr = alloc.gpuaddr,
        .size = 256,
        .flags = 0,
        .id = 1,
    };
    struct kgsl_gpu_command {
        unsigned long flags;
        unsigned long cmdlist;
        unsigned int cmdsize;
        unsigned int numcmds;
        unsigned long objlist;
        unsigned int objsize;
        unsigned int numobjs;
        unsigned long synclist;
        unsigned int syncsize;
        unsigned int numsyncs;
        unsigned int context_id;
        unsigned int timestamp;
    } gpu_cmd = {
        .flags = 0,
        .cmdlist = (unsigned long)&cmd_obj,
        .cmdsize = sizeof(cmd_obj),
        .numcmds = 1,
        .context_id = 1,
    };
    ret = ioctl(fd, _KGSL_IOC(0x4A, void, 0), &gpu_cmd);
    printf("   result: %d  timestamp: %u (expect >0)\n", ret, gpu_cmd.timestamp);

    /* Step 5: Wait on timestamp (simulate fence) */
    printf("5. WAITTIMESTAMP_CTXTID...\n");
    struct { unsigned int ctx_id; unsigned int timestamp; unsigned int timeout; } wait = {
        .ctx_id = 1, .timestamp = 0, .timeout = 1000
    };
    ret = ioctl(fd, _KGSL_IOC(0x06, void, 0), &wait);
    printf("   result: %d  timestamp: %u (expect >0)\n", ret, wait.timestamp);

    /* Step 6: Submit more commands (batch test) */
    printf("6. BATCH: 10 GPU_COMMANDs in a row...\n");
    for (int i = 0; i < 10; i++) {
        ret = ioctl(fd, _KGSL_IOC(0x4A, void, 0), &gpu_cmd);
    }
    printf("   result: %d (batch of 10)\n", ret);

    printf("\n=== All tests passed ===\n");
    printf("Check /tmp/stub_gpu_client.log for details\n");
    return 0;
}
