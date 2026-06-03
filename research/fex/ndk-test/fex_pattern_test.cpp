// Includes the REAL FEXCore PatternCache.h from the actual source tree.
// No stubs, no copies — this is the same header compiled into libFEXCore.dll.

#include <cstdio>
#include <cstdlib>
#include "Interface/Core/JIT/PatternCache.h"

int main() {
    fprintf(stderr, "=== FEXCore REAL PatternCache (actual source header) ===\n\n");

    FEXCore::CPU::PatternCache cache;

    fprintf(stderr, "IsEnabled: %s\n", cache.IsEnabled() ? "YES" : "NO");
    fprintf(stderr, "IsWarmup:  %s\n\n", cache.IsWarmup() ? "YES" : "NO");

    if (!cache.IsEnabled()) {
        fprintf(stderr, "Set FEXCORE_PATTERN_CACHE=1 to enable.\n");
        fprintf(stderr, "=== SKIPPED ===\n");
        return 0;
    }

    int total_ops = 5000;
    int steady_hits = 0;

    for (int i = 0; i < total_ops; i++) {
        uint64_t hash = ((uint64_t)(i % 15) << 32) | (i & 1);
        if (cache.Lookup(hash)) {
            cache.RecordHit();
            if (!cache.IsWarmup()) steady_hits++;
        } else {
            cache.RecordMiss();
            FEXCore::CPU::PatternTemplate tmpl{};
            tmpl.Hash = hash;
            tmpl.ByteSize = 16;
            cache.Insert(tmpl);
        }
        cache.TickWarmup();
    }

    cache.LogStats();

    int steady_total = total_ops - 500;
    fprintf(stderr, "Steady-state: %d hits / %d ops (%.1f%%)\n",
        steady_hits, steady_total, 100.0 * steady_hits / steady_total);

    bool pass = steady_hits >= steady_total * 80 / 100;
    fprintf(stderr, "\n=== %s ===\n", pass ? "PASSED" : "FAILED");
    return pass ? 0 : 1;
}
