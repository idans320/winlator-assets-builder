/*
 * load_dlls_x64.c — x86-64 PE smoke test for the ARM64EC path.
 *
 * This executable is an ordinary IMAGE_FILE_MACHINE_AMD64 (0x8664) binary.
 * When Wine is an ARM64X build, running it on ARM64 happens through the FEX
 * ARM64EC emulator: Wine's ntdll loads C:\windows\system32\libarm64ecfex.dll
 * (dlls/ntdll/loader.c) for x64 guests, and every DLL this process loads must
 * therefore be ARM64EC (0xA641) or ARM64X (0xA64E) — a plain ARM64 (0xAA64)
 * DLL such as the pre-ARM64EC DXVK build cannot be mapped into this process.
 *
 * Checks:
 *   - d3d11.dll / dxgi.dll load and are ARM64EC or ARM64X (DXVK)
 *   - d3d11.dll exports D3D11CreateDevice, dxgi.dll exports CreateDXGIFactory
 *   - libarm64ecfex.dll loads and is exactly ARM64EC (the FEX x64 module)
 *
 * Build:
 *   x86_64-w64-mingw32-clang -Os -nostdlib -fno-stack-protector -fno-builtin \
 *       -Wl,--entry,mainCRTStartup -Wl,--subsystem,console \
 *       -Wl,--no-insert-timestamp -lkernel32 \
 *       -o load_dlls_x64.exe ci/tests/load_dlls_x64.c
 *
 * Exit code: 0 on success, otherwise the number of failed checks.
 */
#include "pe_test_common.h"

static const struct dll_expect tests[] = {
    /* name                    ok machines           export probe */
    {"d3d11.dll", MACHINE_ARM64EC, MACHINE_ARM64X, "D3D11CreateDevice"},
    {"dxgi.dll", MACHINE_ARM64EC, MACHINE_ARM64X, "CreateDXGIFactory"},
    {"libarm64ecfex.dll", MACHINE_ARM64EC, 0, 0},
};

void mainCRTStartup(void) {
    run_dll_tests("x64 (ARM64EC guest path)", tests,
                  (unsigned long)(sizeof(tests) / sizeof(tests[0])));
    for (;;) {
    }
}
