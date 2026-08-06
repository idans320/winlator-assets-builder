package stub

import "time"

type ExperimentConfig struct {
	RedundantWriteFilter bool          `json:"redundant_write_filter"`
	BarrierBypass        bool          `json:"barrier_bypass"`
	LatencyInjectUs      time.Duration `json:"latency_inject_us"`
	BatchAnalysis        bool          `json:"batch_analysis"`
}

type ExperimentState struct {
	Config ExperimentConfig

	// Experiment A: Redundant Write Filter
	RedundantWritesDropped uint64 `json:"redundant_writes_dropped"`
	UniqueRegWrites        uint64 `json:"unique_reg_writes"`

	// Experiment B: Barrier Bypass
	BarriersSkipped uint64 `json:"barriers_skipped"`
	DrawsProcessed  uint64 `json:"draws_processed"`

	// Experiment C: Latency Injection
	TotalLatencyInjected time.Duration `json:"total_latency_injected"`

	// Experiment D: Batch Analysis
	SubmitsAnalyzed   uint64                   `json:"submits_analyzed"`
	PerSubmitStats    []SubmitBatchStats       `json:"per_submit_stats,omitempty"`
}

type SubmitBatchStats struct {
	SubmitID       uint64 `json:"submit_id"`
	Entries        int    `json:"entries"`
	TotalDwords    int    `json:"total_dwords"`
	PKT4Packets    int    `json:"pkt4_packets"`
	PKT7Packets    int    `json:"pkt7_packets"`
	RegWrites      int    `json:"reg_writes"`
	A8XXRegWrites  int    `json:"a8xx_reg_writes"`
	RedundantWrites int   `json:"redundant_writes"`
	Barriers       int    `json:"barriers"`
	Draws          int    `json:"draws"`
	LatencyUs      int    `json:"latency_us"`
}

func NewExperimentState(cfg ExperimentConfig) *ExperimentState {
	return &ExperimentState{
		Config: cfg,
	}
}

func (es *ExperimentState) ApplyLatency() {
	if es.Config.LatencyInjectUs > 0 {
		time.Sleep(es.Config.LatencyInjectUs * time.Microsecond)
		es.TotalLatencyInjected += es.Config.LatencyInjectUs
	}
}

func (es *ExperimentState) CheckRedundantWrite(reg uint32, newVal uint32, currentVal uint32) bool {
	if currentVal == newVal && es.Config.RedundantWriteFilter {
		es.RedundantWritesDropped++
		return true
	}
	es.UniqueRegWrites++
	return false
}

func (es *ExperimentState) CheckBarrierBypass(opcode uint32) bool {
	if es.Config.BarrierBypass {
		es.BarriersSkipped++
		return true
	}
	return false
}

func (es *ExperimentState) RecordDraw() {
	es.DrawsProcessed++
}

func (es *ExperimentState) RecordBatch(stats SubmitBatchStats) {
	if es.Config.BatchAnalysis {
		es.SubmitsAnalyzed++
		if len(es.PerSubmitStats) < 1000 {
			es.PerSubmitStats = append(es.PerSubmitStats, stats)
		}
	}
}
