/*
 * pe_test_common.h — minimal, stdlib-free Win32 helpers for the DLL load
 * smoke tests used by ci/run-wine-tests.sh.
 *
 * Design constraints:
 *   - No CRT: linked with -nostdlib, entry point is mainCRTStartup.
 *   - Only kernel32 imports (LoadLibraryA / GetProcAddress / GetLastError /
 *     GetStdHandle / WriteFile / ExitProcess), so the test program itself
 *     cannot be the thing that fails to load.
 *   - Prints a machine-readable-ish line per check, and exits nonzero if any
 *     check failed.
 *
 * Build (see ci/build-test-programs.sh):
 *
 *   x86_64-w64-mingw32-clang -Os -nostdlib -fno-stack-protector -fno-builtin \
 *       -Wl,--entry,mainCRTStartup -Wl,--subsystem,console \
 *       -Wl,--no-insert-timestamp -lkernel32 \
 *       -o load_dlls_x64.exe ci/tests/load_dlls_x64.c
 *
 * The same flags work with i686-w64-mingw32-clang and aarch64-w64-mingw32-clang
 * (and with the bylaws ARM64EC toolchain drivers).
 */
#ifndef PE_TEST_COMMON_H
#define PE_TEST_COMMON_H

typedef void *HMODULE;
typedef void *HANDLE;
typedef unsigned long DWORD;
typedef int BOOL;
typedef const char *LPCSTR;

__declspec(dllimport) HMODULE __stdcall LoadLibraryA(LPCSTR name);
__declspec(dllimport) void *__stdcall GetProcAddress(HMODULE module, LPCSTR name);
__declspec(dllimport) DWORD __stdcall GetLastError(void);
__declspec(dllimport) HANDLE __stdcall GetStdHandle(DWORD which);
__declspec(dllimport) BOOL __stdcall WriteFile(HANDLE file, const void *buf,
                                               DWORD len, DWORD *written,
                                               void *overlapped);
__declspec(dllimport) void __stdcall ExitProcess(unsigned int code);

#define TEST_STD_OUTPUT_HANDLE ((DWORD)-11)

/* Accepted PE machine types. Kept as raw numbers so the test does not depend
 * on any SDK header (and so the values are greppable in CI logs). */
#define MACHINE_I386 0x014Cu
#define MACHINE_AMD64 0x8664u
#define MACHINE_ARM64 0xAA64u
#define MACHINE_ARM64EC 0xA641u
#define MACHINE_ARM64X 0xA64Eu

struct dll_expect {
    const char *name;          /* DLL name passed to LoadLibraryA */
    unsigned short machine_a;  /* accepted machine type (0 = any) */
    unsigned short machine_b;  /* second accepted machine type (0 = none) */
    const char *export_probe;  /* optional export that must resolve, or 0 */
};

static HANDLE g_out;
static unsigned long g_checks;
static unsigned long g_failures;

static unsigned long str_len(const char *s) {
    unsigned long n = 0;
    while (s[n]) n++;
    return n;
}

static void out_str(const char *s) {
    DWORD written = 0;
    WriteFile(g_out, s, (DWORD)str_len(s), &written, 0);
}

static void out_hex4(unsigned long v) {
    static const char digits[] = "0123456789ABCDEF";
    char buf[7];
    int i;
    buf[0] = '0';
    buf[1] = 'x';
    for (i = 0; i < 4; i++) buf[2 + i] = digits[(v >> ((3 - i) * 4)) & 0xF];
    buf[6] = 0;
    out_str(buf);
}

static void out_dec(unsigned long v) {
    char buf[12];
    int i = 11;
    buf[i] = 0;
    if (v == 0) {
        out_str("0");
        return;
    }
    while (v && i > 0) {
        buf[--i] = (char)('0' + (v % 10));
        v /= 10;
    }
    out_str(&buf[i]);
}

/* Read e_machine straight out of the mapped image. Wine maps the PE headers,
 * so this works for both native and builtin modules. Returns 0 if the header
 * cannot be parsed (fake handle, exotic module, ...). */
static unsigned short image_machine(HMODULE module) {
    const unsigned char *base = (const unsigned char *)module;
    const unsigned char *pe;
    unsigned long e_lfanew;

    if (!base) return 0;
    e_lfanew = *(const unsigned long *)(base + 0x3C);
    if (e_lfanew < 0x40 || e_lfanew > 0x100000) return 0;
    pe = base + e_lfanew;
    if (pe[0] != 'P' || pe[1] != 'E' || pe[2] != 0 || pe[3] != 0) return 0;
    return *(const unsigned short *)(pe + 4);
}

static void machine_name(unsigned long m, char *dst) {
    const char *name = "unknown";
    switch (m) {
        case MACHINE_I386: name = "i386"; break;
        case MACHINE_AMD64: name = "x86_64"; break;
        case MACHINE_ARM64: name = "arm64"; break;
        case MACHINE_ARM64EC: name = "arm64ec"; break;
        case MACHINE_ARM64X: name = "arm64x"; break;
        default: break;
    }
    while (*name) *dst++ = *name++;
    *dst = 0;
}

static int machine_accepted(unsigned long actual, const struct dll_expect *e) {
    if (e->machine_a == 0) return 1;
    if (actual == e->machine_a) return 1;
    if (e->machine_b != 0 && actual == e->machine_b) return 1;
    return 0;
}

/*
 * Runs every entry, prints one line per check, then exits the process:
 * exit code 0 = all checks passed, 1..n = number of failed checks.
 */
static void run_dll_tests(const char *flavour, const struct dll_expect *tests,
                          unsigned long count) {
    unsigned long i;
    char namebuf[16];

    g_out = GetStdHandle(TEST_STD_OUTPUT_HANDLE);

    out_str("== load_dlls ");
    out_str(flavour);
    out_str(" ==\n");

    for (i = 0; i < count; i++) {
        const struct dll_expect *e = &tests[i];
        HMODULE module;
        unsigned long machine;

        g_checks++;
        module = LoadLibraryA(e->name);
        if (!module) {
            g_failures++;
            out_str("[FAIL] LoadLibraryA(");
            out_str(e->name);
            out_str(") error=");
            out_dec((unsigned long)GetLastError());
            out_str("\n");
            continue;
        }

        machine = image_machine(module);
        if (!machine) {
            g_failures++;
            out_str("[FAIL] ");
            out_str(e->name);
            out_str(" loaded but PE header unreadable\n");
            continue;
        }

        if (!machine_accepted(machine, e)) {
            g_failures++;
            out_str("[FAIL] ");
            out_str(e->name);
            out_str(" machine=");
            out_hex4(machine);
            out_str(" name=");
            machine_name(machine, namebuf);
            out_str(namebuf);
            out_str(" expected=");
            out_hex4(e->machine_a);
            if (e->machine_b) {
                out_str("/");
                out_hex4(e->machine_b);
            }
            out_str("\n");
            continue;
        }

        out_str("[ ok ] ");
        out_str(e->name);
        out_str(" machine=");
        out_hex4(machine);
        out_str(" (");
        machine_name(machine, namebuf);
        out_str(namebuf);
        out_str(")\n");

        if (e->export_probe) {
            g_checks++;
            if (!GetProcAddress(module, e->export_probe)) {
                g_failures++;
                out_str("[FAIL] ");
                out_str(e->name);
                out_str(" missing export ");
                out_str(e->export_probe);
                out_str("\n");
            } else {
                out_str("[ ok ] ");
                out_str(e->name);
                out_str(" exports ");
                out_str(e->export_probe);
                out_str("\n");
            }
        }
    }

    out_str("== load_dlls ");
    out_str(flavour);
    out_str(": ");
    out_dec(g_checks - g_failures);
    out_str("/");
    out_dec(g_checks);
    out_str(" checks passed, ");
    out_dec(g_failures);
    out_str(" failed ==\n");

    ExitProcess((unsigned int)(g_failures > 255 ? 255 : g_failures));
}

#endif /* PE_TEST_COMMON_H */
