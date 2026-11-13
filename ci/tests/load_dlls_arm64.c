/*
 * load_dlls_arm64.c — native ARM64 PE smoke test.
 *
 * Loads the ARM64X Wine DLLs that a native ARM64 Windows process sees
 * (ntdll/kernel32 are 0xA64E in an ARM64X Wine build) plus the plain ARM64 FEX
 * modules that live in the prefix system32 (libFEXCore.dll, libwow64fex.dll —
 * 0xAA64). This is the "native ARM64 still works" half of the matrix.
 *
 * NOTE: an ARM64 process cannot load ARM64EC (0xA641) images, so this test
 * deliberately does not touch libarm64ecfex.dll or an ARM64EC-only DXVK build.
 * If DXVK ships its system32 DLLs as ARM64X (0xA64E) they would be loadable
 * here too; the check is left out to keep the pass/fail signal about Wine and
 * FEX only.
 *
 * Build:
 *   aarch64-w64-mingw32-clang -Os -nostdlib -fno-stack-protector -fno-builtin \
 *       -Wl,--entry,mainCRTStartup -Wl,--subsystem,console \
 *       -Wl,--no-insert-timestamp -lkernel32 \
 *       -o load_dlls_arm64.exe ci/tests/load_dlls_arm64.c
 *
 * Exit code: 0 on success, otherwise the number of failed checks.
 */
#include "pe_test_common.h"

static const struct dll_expect tests[] = {
    /* name                 ok machines          export probe */
    {"ntdll.dll", MACHINE_ARM64X, MACHINE_ARM64, 0},
    {"kernel32.dll", MACHINE_ARM64X, MACHINE_ARM64, 0},
    {"libFEXCore.dll", MACHINE_ARM64, 0, 0},
    {"libwow64fex.dll", MACHINE_ARM64, 0, 0},
};

void mainCRTStartup(void) {
    run_dll_tests("arm64 (native path)", tests,
                  (unsigned long)(sizeof(tests) / sizeof(tests[0])));
    for (;;) {
    }
}
