// prove_heresies.c — C driver with CNTVCT_EL0 + wall-clock timing harness
//
// Links against prove_heresies.S. Calls each heresy pair, measures
// both CPU ticks (CNTVCT_EL0) and wall-clock time (clock_gettime),
// then reports per-iteration tick count and nanoseconds.
//
// CNTVCT_EL0 frequency is read from CNTFRQ_EL0 at startup.

#include <stdio.h>
#include <stdint.h>
#include <time.h>

extern void heresy_c_vanilla(uint32_t count);
extern void heresy_c_heresy(uint32_t count);
extern void heresy_n_vanilla(uint32_t count);
extern void heresy_n_heresy(uint32_t count);
extern void heresy_a_vanilla(uint32_t count);
extern void heresy_a_heresy(uint32_t count);
extern void heresy_dj_vanilla(uint32_t count);
extern void heresy_dj_heresy(uint32_t count);
extern void heresy_k_vanilla(uint32_t count);
extern void heresy_k_heresy(uint32_t count);
extern void heresy_e_vanilla(uint32_t count);
extern void heresy_e_heresy(uint32_t count);

#define ITERATIONS 5000000
#define WARMUP      200000

static uint64_t g_freq_hz;

static inline uint64_t read_cntvct(void) {
    uint64_t val;
    __asm__ volatile("isb" ::: "memory");
    __asm__ volatile("mrs %0, cntvct_el0" : "=r"(val));
    __asm__ volatile("isb" ::: "memory");
    return val;
}

static inline uint64_t read_cntfrq(void) {
    uint64_t val;
    __asm__ volatile("mrs %0, cntfrq_el0" : "=r"(val));
    return val;
}

static inline uint64_t wall_ns(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (uint64_t)ts.tv_sec * 1000000000ULL + (uint64_t)ts.tv_nsec;
}

static void bench(const char *name,
                  void (*vanilla)(uint32_t),
                  void (*heresy)(uint32_t))
{
    vanilla(WARMUP);
    heresy(WARMUP);

    uint64_t c0 = read_cntvct();
    uint64_t w0 = wall_ns();
    vanilla(ITERATIONS);
    uint64_t c1 = read_cntvct();
    uint64_t w1 = wall_ns();
    heresy(ITERATIONS);
    uint64_t c2 = read_cntvct();
    uint64_t w2 = wall_ns();

    uint64_t v_cy = c1 - c0;
    uint64_t h_cy = c2 - c1;
    uint64_t v_ns = w1 - w0;
    uint64_t h_ns = w2 - w1;

    double dv = (double)v_cy / ITERATIONS;
    double dh = (double)h_cy / ITERATIONS;
    double nv = (double)v_ns / ITERATIONS;
    double nh = (double)h_ns / ITERATIONS;

    double delta_cy = (dv > 0.0001) ? ((dh - dv) / dv) * 100.0 : 0.0;
    double delta_ns = (nv > 0.01)    ? ((nh - nv) / nv) * 100.0 : 0.0;

    printf("%-32s %8.2f  %8.2f  %8.2f  %8.2f  %+6.1f%% (%+6.1f%%)\n",
           name, dv, dh, nv, nh, delta_cy, delta_ns);
}

int main(void) {
    g_freq_hz = read_cntfrq();

    printf("===== Oryon SIMD Heresies — AArch64 Assembly Probes =====\n");
    printf("CNTFRQ_EL0: %llu Hz  |  Iterations: %u  |  Warmup: %u\n\n",
           (unsigned long long)g_freq_hz, ITERATIONS, WARMUP);

    printf("%-32s %8s  %8s  %8s  %8s  %s\n",
           "Heresy", "v_ticks", "h_ticks", "v_ns", "h_ns", "delta (ticks, wall)");
    printf("----------------------------------------------------------------------------\n");

    bench("C  — int recip vs fdiv",          heresy_c_vanilla,  heresy_c_heresy);
    bench("N  — umull vs fmul",              heresy_n_vanilla,  heresy_n_heresy);
    bench("A  — rbit+clz vs 14x if-chain",   heresy_a_vanilla,  heresy_a_heresy);
    bench("DJ — 4-wide ILP vs sequential",   heresy_dj_vanilla, heresy_dj_heresy);
    bench("K  — DC ZVA vs stp zero loop",    heresy_k_vanilla,  heresy_k_heresy);
    bench("E  — 1x cbnz vs 3x tbnz chain",   heresy_e_vanilla,  heresy_e_heresy);

    printf("\n");
    printf("v_ticks/h_ticks = CNTVCT_EL0 ticks per iteration (vanilla / heresy)\n");
    printf("v_ns/h_ns       = wall-clock nanoseconds per iteration\n");
    printf("negative delta  = heresy is FASTER\n");
    printf("C,N negative on purpose — their value is FPU contention avoidance\n");
    printf("in mixed workloads, not raw instruction throughput.\n");
    return 0;
}
