/*
 * load_dlls_i386.c — i386 PE smoke test for the 32-bit (WoW64/FEX) path.
 *
 * The executable is IMAGE_FILE_MACHINE_I386 (0x14C). On ARM64 Wine it runs
 * through the new WoW64 mode (FEX's libwow64fex.dll), and the DLLs it loads
 * are resolved from the prefix's syswow64 (also 0x14C). Loading the DXVK i686
 * DLLs here proves the 32-bit side of the package is importable and that
 * DllMain runs under WoW64 on ARM64.
 *
 * Checks:
 *   - d3d11.dll / dxgi.dll load and are i386
 *   - d3d11.dll exports D3D11CreateDevice, dxgi.dll exports CreateDXGIFactory
 *
 * Build:
 *   i686-w64-mingw32-clang -Os -nostdlib -fno-stack-protector -fno-builtin \
 *       -Wl,--entry,mainCRTStartup -Wl,--subsystem,console \
 *       -Wl,--no-insert-timestamp -lkernel32 \
 *       -o load_dlls_i386.exe ci/tests/load_dlls_i386.c
 *
 * Exit code: 0 on success, otherwise the number of failed checks.
 */
#include "pe_test_common.h"

static const struct dll_expect tests[] = {
    /* name          ok machines     export probe */
    {"d3d11.dll", MACHINE_I386, 0, "D3D11CreateDevice"},
    {"dxgi.dll", MACHINE_I386, 0, "CreateDXGIFactory"},
};

void mainCRTStartup(void) {
    run_dll_tests("i386 (WoW64 path)", tests,
                  (unsigned long)(sizeof(tests) / sizeof(tests[0])));
    for (;;) {
    }
}
