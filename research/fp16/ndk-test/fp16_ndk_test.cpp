// Standalone NDK test for DXVK FP16 utilities.
// Cross-compiled via build_ndk_test.sh
// Run on device: adb push fp16-ndk-test /data/local/tmp/ && adb shell /data/local/tmp/fp16-ndk-test

#include <cstdint>
#include <cstring>
#include <cstdio>
#include <cmath>
#include <ctime>

// ================================================================
// SieveCache (self-contained)
// ================================================================

template<uint32_t Omega>
class SieveCache {
public:
    static constexpr uint32_t kMaxDataSize = 256;
    SieveCache() : m_size(0), m_hand(0), m_hits(0), m_misses(0), m_evictions(0) {
        std::memset(m_keys, 0, sizeof(m_keys));
        std::memset(m_data, 0, sizeof(m_data));
    }
    const void* get(uint64_t key) {
        for (uint32_t i = 0; i < m_size; i++) {
            if (m_keys[i] == key) { m_visited[i] = true; m_hits++; return m_data[i]; }
        }
        m_misses++; return nullptr;
    }
    void put(uint64_t key, const void* data, uint32_t size) {
        if (size > kMaxDataSize) return;
        for (uint32_t i = 0; i < m_size; i++) if (m_keys[i] == key) return;
        if (m_size >= Omega) evict();
        for (uint32_t i = m_size; i > 0; i--) {
            m_keys[i] = m_keys[i - 1]; m_visited[i] = m_visited[i - 1];
            std::memcpy(m_data[i], m_data[i - 1], kMaxDataSize);
        }
        m_keys[0] = key; m_visited[0] = false;
        std::memcpy(m_data[0], data, size); m_size++;
        if (m_hand < m_size - 1) m_hand++;
    }
    uint64_t hits() const { return m_hits; }
    uint64_t misses() const { return m_misses; }
    uint64_t evictions() const { return m_evictions; }
    uint32_t size() const { return m_size; }
private:
    void evict() {
        if (m_size == 0) return;
        while (m_visited[m_hand]) { m_visited[m_hand] = false; m_hand = m_hand > 0 ? m_hand - 1 : m_size - 1; }
        for (uint32_t i = m_hand; i < m_size - 1; i++) {
            m_keys[i] = m_keys[i + 1]; m_visited[i] = m_visited[i + 1];
            std::memcpy(m_data[i], m_data[i + 1], kMaxDataSize);
        }
        m_size--; m_evictions++;
        if (m_size > 0 && m_hand >= m_size) m_hand = m_size - 1;
    }
    uint64_t m_keys[Omega]; bool m_visited[Omega]; uint8_t m_data[Omega][kMaxDataSize];
    uint32_t m_size, m_hand; uint64_t m_hits, m_misses, m_evictions;
};

// ================================================================
// FP16 pack/unpack
// ================================================================

uint64_t fp16HashData(const void* data, uint32_t size) {
    uint64_t hash = 14695981039346656037ull;
    const uint8_t* bytes = (const uint8_t*)data;
    for (uint32_t i = 0; i < size; i++) { hash ^= bytes[i]; hash *= 1099511628211ull; }
    return hash;
}

void fp16PackData(void* dst, const float* src, uint32_t count) {
    uint16_t* f16 = (uint16_t*)dst;
    for (uint32_t i = 0; i < count; i++) {
        uint32_t bits; std::memcpy(&bits, &src[i], sizeof(bits));
        uint32_t sign = (bits >> 16) & 0x8000;
        int32_t  exp  = (int32_t)((bits >> 23) & 0xFF) - 127 + 15;
        uint32_t mant = (bits >> 13) & 0x3FF;
        if (exp <= 0) f16[i] = (uint16_t)(sign);
        else if (exp >= 31) f16[i] = (uint16_t)(sign | 0x7C00);
        else f16[i] = (uint16_t)(sign | (exp << 10) | mant);
    }
}

float fp16UnpackSingle(uint16_t v) {
    uint32_t sign = (v >> 15) & 1, exp = (v >> 10) & 0x1F, mant = v & 0x3FF, result;
    if (exp == 0) {
        if (mant == 0) { result = sign << 31; }
        else { while ((mant & 0x400) == 0) { mant <<= 1; exp--; } mant &= 0x3FF; exp = 127 - 15 + exp; result = (sign << 31) | (exp << 23) | (mant << 13); }
    } else if (exp == 0x1F) { result = (sign << 31) | (0xFF << 23) | (mant << 13); }
    else { result = (sign << 31) | ((exp + 127 - 15) << 23) | (mant << 13); }
    float f; std::memcpy(&f, &result, sizeof(f)); return f;
}

double now_us() { struct timespec ts; clock_gettime(CLOCK_MONOTONIC, &ts); return ts.tv_sec * 1e6 + ts.tv_nsec / 1e3; }

// ================================================================
// Tests
// ================================================================

int test_pack_roundtrip() {
    fprintf(stderr, "  fp16 roundtrip...");
    const float inputs[] = { 1.0f, -1.0f, 0.5f, 0.0f, 3.14159f, -0.001f, 65504.0f, 0.000061f };
    const int n = sizeof(inputs)/sizeof(float);
    uint16_t packed[16]; float unpacked[16];
    fp16PackData(packed, inputs, n);
    for (int i = 0; i < n; i++) unpacked[i] = fp16UnpackSingle(packed[i]);
    int errors = 0;
    for (int i = 0; i < n; i++) {
        float diff = fabsf(unpacked[i] - inputs[i]);
        if (diff > fmaxf(fabsf(inputs[i]) * 0.001f, 0.0001f) && !isinf(inputs[i])) errors++;
    }
    fprintf(stderr, errors ? " %d ERRORS\n" : " OK (%d values)\n", errors ? errors : n);
    return errors;
}

int test_pack_throughput() {
    fprintf(stderr, "  fp16 pack throughput...");
    const int count = 1000000;
    float* src = new float[count]; uint16_t* dst = new uint16_t[count];
    for (int i = 0; i < count; i++) src[i] = (float)(i % 1000) / 100.0f;
    double start = now_us();
    fp16PackData(dst, src, count);
    double elapsed = now_us() - start;
    double mbps = (count * sizeof(float)) / (elapsed / 1e6) / (1024*1024);
    fprintf(stderr, " %.1f us (%.0f MB/s)\n", elapsed, mbps);
    delete[] src; delete[] dst;
    return 0;
}

int test_sieve_hitrate() {
    fprintf(stderr, "  SIEVE cache hit rate...");
    SieveCache<256> cache;
    uint64_t hits = 0, misses = 0; uint8_t buf[64] = {};
    for (int i = 0; i < 10000; i++) {
        uint64_t key = (i > 100 && (i % 7) < 5) ? (i - (i % 7)) * 1000 : i * 1000;
        if (cache.get(key)) hits++; else { misses++; cache.put(key, buf, sizeof(buf)); }
    }
    float rate = hits + misses > 0 ? 100.0f * hits / (hits + misses) : 0;
    fprintf(stderr, " %llu hits / %llu misses = %.1f%% (evictions=%llu)\n",
        (unsigned long long)hits, (unsigned long long)misses, rate, (unsigned long long)cache.evictions());
    return rate > 40 ? 0 : 1;
}

int test_sieve_eviction() {
    fprintf(stderr, "  SIEVE eviction correctness...");
    SieveCache<4> cache; uint8_t data[32];
    for (int i = 0; i < 4; i++) { std::memset(data, i, sizeof(data)); cache.put(i*100, data, sizeof(data)); }
    if (!cache.get(0) || !cache.get(100)) { fprintf(stderr, " FAIL\n"); return 1; }
    std::memset(data, 4, sizeof(data)); cache.put(400, data, sizeof(data));
    if (cache.evictions() < 1) { fprintf(stderr, " FAIL (no eviction)\n"); return 1; }
    if (!cache.get(0) || !cache.get(100)) { fprintf(stderr, " FAIL (visited keys evicted)\n"); return 1; }
    fprintf(stderr, " OK (evicted=%llu, survivors: 0+100)\n", (unsigned long long)cache.evictions());
    return 0;
}

int test_hash_stability() {
    fprintf(stderr, "  FNV1a hash stability...");
    const char* a = "uniform_buffer_data_12345";
    if (fp16HashData(a, 26) != fp16HashData(a, 26)) { fprintf(stderr, " FAIL\n"); return 1; }
    if (fp16HashData(a, 26) == fp16HashData("uniform_buffer_data_12346", 26)) { fprintf(stderr, " FAIL (collision)\n"); return 1; }
    fprintf(stderr, " OK\n");
    return 0;
}

int main() {
    fprintf(stderr, "=== DXVK FP16 NDK Test ===\nTarget: aarch64-android (Adreno Gen8)\n\n");
    int passed = 0, total = 5;
    if (test_pack_roundtrip() == 0) passed++;
    if (test_pack_throughput() == 0) passed++;
    if (test_sieve_hitrate() == 0) passed++;
    if (test_sieve_eviction() == 0) passed++;
    if (test_hash_stability() == 0) passed++;
    fprintf(stderr, "\n=== %d/%d tests passed ===\n", passed, total);
    return passed == total ? 0 : 1;
}
