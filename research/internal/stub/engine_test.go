package stub

import (
	"testing"
	"time"

	"github.com/idans/winlator-cmod-builder/research/internal/pm4"
)

func buildPkt4(reg uint16, cnt uint16) uint32 {
	hdr := pm4.Type4Mask | uint32(cnt) | (parityCheck2(uint32(cnt)) << 7) |
		((uint32(reg) & 0x3ffff) << 8) |
		(parityCheck2(uint32(reg)) << 27)
	return hdr
}

func buildPkt7(opcode uint8, cnt uint16) uint32 {
	hdr := pm4.Type7Mask | uint32(cnt) |
		(parityCheck2(uint32(cnt)) << 15) |
		((uint32(opcode) & 0x7f) << 16) |
		(parityCheck2(uint32(opcode)) << 23)
	return hdr
}

func parityCheck2(val uint32) uint32 {
	val ^= val >> 16
	val ^= val >> 8
	val ^= val >> 4
	val &= 0xf
	return (uint32(0x9669) >> val) & 1
}

func TestEngineBasicSubmit(t *testing.T) {
	cfg := ExperimentConfig{}
	memTracker := NewMemoryTracker()
	engine := NewGPUEngine(memTracker, cfg)

	p4 := buildPkt4(0x0800, 1)
	p7 := buildPkt7(pm4.CP_WAIT_FOR_IDLE, 0)

	entries := []*pm4.SubmissionEntry{
		{
			Name:   "test_cs",
			Dwords: []uint32{p4, 0xDEADBEEF, p7},
		},
	}

	for _, e := range entries {
		packets, _ := pm4.Decode(e.Dwords)
		e.Packets = packets
	}

	result := engine.ProcessSubmit(entries)

	if result.Result != "" {
		t.Errorf("expected empty result string, got %q", result.Result)
	}
	if engine.SubmitCount() != 1 {
		t.Errorf("expected 1 submit, got %d", engine.SubmitCount())
	}
	if engine.DwordCount() < 3 {
		t.Errorf("expected at least 3 dwords, got %d", engine.DwordCount())
	}
	if engine.GetRegister(0x0800) != 0xDEADBEEF {
		t.Errorf("expected reg 0x0800 = 0xDEADBEEF, got 0x%08x", engine.GetRegister(0x0800))
	}
}

func TestEngineRedundantWriteFilter(t *testing.T) {
	cfg := ExperimentConfig{
		RedundantWriteFilter: true,
	}
	memTracker := NewMemoryTracker()
	engine := NewGPUEngine(memTracker, cfg)

	p4 := buildPkt4(0x0800, 1)

	entries := []*pm4.SubmissionEntry{
		{Name: "cs1", Dwords: []uint32{p4, 0xAAAA}},
		{Name: "cs2", Dwords: []uint32{p4, 0xAAAA}}, // redundant
		{Name: "cs3", Dwords: []uint32{p4, 0xBBBB}},
	}

	for _, e := range entries {
		packets, _ := pm4.Decode(e.Dwords)
		e.Packets = packets
		engine.ProcessSubmit([]*pm4.SubmissionEntry{e})
	}

	stats := engine.Stats()
	if stats.RedundantWritesDropped != 1 {
		t.Errorf("expected 1 redundant write dropped, got %d", stats.RedundantWritesDropped)
	}
	if engine.GetRegister(0x0800) != 0xBBBB {
		t.Errorf("expected reg = 0xBBBB, got 0x%08x", engine.GetRegister(0x0800))
	}
}

func TestEngineBarrierBypass(t *testing.T) {
	cfg := ExperimentConfig{
		BarrierBypass: true,
	}
	memTracker := NewMemoryTracker()
	engine := NewGPUEngine(memTracker, cfg)

	p7_wfi := buildPkt7(pm4.CP_WAIT_FOR_IDLE, 0)
	p7_barrier := buildPkt7(pm4.CP_BARRIER, 0)

	entries := []*pm4.SubmissionEntry{
		{Name: "cs", Dwords: []uint32{p7_wfi, p7_barrier}},
	}

	for _, e := range entries {
		packets, _ := pm4.Decode(e.Dwords)
		e.Packets = packets
	}

	engine.ProcessSubmit(entries)
	stats := engine.Stats()

	if stats.BarriersSkipped != 2 {
		t.Errorf("expected 2 barriers skipped, got %d", stats.BarriersSkipped)
	}
}

func TestEngineLatencyInjection(t *testing.T) {
	cfg := ExperimentConfig{
		LatencyInjectUs: 10,
	}
	memTracker := NewMemoryTracker()
	engine := NewGPUEngine(memTracker, cfg)

	p4 := buildPkt4(0x0800, 1)
	start := time.Now()

	for i := 0; i < 5; i++ {
		entries := []*pm4.SubmissionEntry{
			{Name: "cs", Dwords: []uint32{p4, uint32(i)}},
		}
		for _, e := range entries {
			packets, _ := pm4.Decode(e.Dwords)
			e.Packets = packets
		}
		engine.ProcessSubmit(entries)
	}

	elapsed := time.Since(start)
	if elapsed < 40*time.Microsecond {
		t.Errorf("expected at least 40us latency, got %v", elapsed)
	}
}

func TestEngineMemoryTracker(t *testing.T) {
	cfg := ExperimentConfig{}
	memTracker := NewMemoryTracker()
	engine := NewGPUEngine(memTracker, cfg)

	memTracker.AddBO(0x1000, 4096, "query_pool")

	// CP_EVENT_WRITE7 with CACHE_FLUSH_TS event (0x1f) writing to addr 0x1000
	p7 := buildPkt7(pm4.CP_EVENT_WRITE, 4)
	dwords := []uint32{p7, 0x1f, 0, 0x1000, 0x0000}

	entries := []*pm4.SubmissionEntry{
		{Name: "cs", Dwords: dwords},
	}
	for _, e := range entries {
		packets, _ := pm4.Decode(e.Dwords)
		e.Packets = packets
	}

	result := engine.ProcessSubmit(entries)

	bo := memTracker.GetBO(0x1000)
	if bo == nil {
		t.Fatal("expected tracked BO at 0x1000")
	}

	if len(result.WritebackBOs) > 0 {
		t.Logf("got %d writeback entries", len(result.WritebackBOs))
	}
}

func TestEngineBatchAnalysis(t *testing.T) {
	cfg := ExperimentConfig{
		BatchAnalysis: true,
	}
	memTracker := NewMemoryTracker()
	engine := NewGPUEngine(memTracker, cfg)

	p4 := buildPkt4(0x0800, 1)
	dwords := []uint32{p4, 0xCAFE}

	for i := 0; i < 5; i++ {
		entries := []*pm4.SubmissionEntry{
			{Name: "cs", Dwords: dwords},
		}
		for _, e := range entries {
			packets, _ := pm4.Decode(e.Dwords)
			e.Packets = packets
		}
		engine.ProcessSubmit(entries)
	}

	stats := engine.Stats()
	if stats.SubmitsAnalyzed != 5 {
		t.Errorf("expected 5 batches analyzed, got %d", stats.SubmitsAnalyzed)
	}
	if len(stats.PerSubmitStats) != 5 {
		t.Errorf("expected 5 per-submit stats, got %d", len(stats.PerSubmitStats))
	}
}
