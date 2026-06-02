package stub

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/idans/winlator-cmod-builder/research/internal/pm4"
)

type GPUEngine struct {
	mu           sync.Mutex
	shadowRegs   map[uint32]uint32
	memTracker   *MemoryTracker
	experiments  *ExperimentState
	timestampSeq atomic.Uint64
	submitSeq    atomic.Uint64
	totalSubmits atomic.Uint64
	totalDraws   atomic.Uint64
	totalDwords  atomic.Uint64
}

func NewGPUEngine(memTracker *MemoryTracker, cfg ExperimentConfig) *GPUEngine {
	return &GPUEngine{
		shadowRegs:  make(map[uint32]uint32, 4096),
		memTracker:  memTracker,
		experiments: NewExperimentState(cfg),
	}
}

func (e *GPUEngine) RegisterName(offset uint32) string {
	if name, ok := pm4.RegNameByOffset[offset]; ok {
		return name
	}
	return fmt.Sprintf("reg_0x%04x", offset)
}

func (e *GPUEngine) GetRegister(reg uint32) uint32 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.shadowRegs[reg]
}

func (e *GPUEngine) SetRegister(reg uint32, val uint32) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	current := e.shadowRegs[reg]
	if e.experiments.CheckRedundantWrite(reg, val, current) {
		return false
	}
	e.shadowRegs[reg] = val
	return true
}

func (e *GPUEngine) ProcessPacket(pkt pm4.PM4Packet) {
	switch pkt.Type {
	case pm4.PacketPKT4:
		for i, val := range pkt.Payload {
			reg := pkt.RegOffset
			if pkt.RegCount > 1 {
				reg += uint32(i)
			}
			e.SetRegister(reg, val)
		}

	case pm4.PacketPKT7:
		switch pkt.Opcode {
		case pm4.CP_EVENT_WRITE:
			e.processEventWrite(pkt)
		case pm4.CP_WAIT_FOR_IDLE, pm4.CP_WAIT_FOR_ME, pm4.CP_WAIT_MEM_WRITES, pm4.CP_BARRIER:
			if e.experiments.Config.BarrierBypass {
				e.experiments.CheckBarrierBypass(pkt.Opcode)
				return
			}

		case pm4.CP_REG_TO_MEM, pm4.CP_MEM_WRITE, pm4.CP_MEM_WRITE_AT:
			e.processRegToMem(pkt)

		case pm4.CP_COND_EXEC:
			e.evaluateCondExec(pkt)
		}

		if pm4.DrawOpcodes[pkt.Opcode] {
			e.experiments.RecordDraw()
			e.totalDraws.Add(1)
		}
	}
}

func (e *GPUEngine) processEventWrite(pkt pm4.PM4Packet) {
	if len(pkt.Payload) < 1 {
		return
	}
	event := pkt.Payload[0] & 0x3F

	switch event {
	case 0x1f: // CACHE_FLUSH_TS
		if len(pkt.Payload) >= 4 {
			addr := uint64(pkt.Payload[2]) | uint64(pkt.Payload[3])<<32
			e.memTracker.WriteDword(addr, e.nextTimestamp())
		}
	case 0x1e: // ZPASS_DONE
	case 0x20: // WT_DONE_GFX_CORE
	case 0x22: // RB_DONE_TS
	}
}

func (e *GPUEngine) processRegToMem(pkt pm4.PM4Packet) {
	if len(pkt.Payload) < 3 {
		return
	}
	reg := pkt.Payload[1]
	addr := uint64(pkt.Payload[2])
	val := e.GetRegister(reg)
	e.memTracker.WriteDword(addr, val)
}

func (e *GPUEngine) evaluateCondExec(pkt pm4.PM4Packet) {
	if len(pkt.Payload) < 2 {
		return
	}
	reg := pkt.Payload[0]
	val := e.GetRegister(reg)
	dref := pkt.Payload[1] & 0xFFFFFFFF
	if val == dref {
		e.shadowRegs[reg] = 0
	}
}

func (e *GPUEngine) ProcessEntry(entry *pm4.SubmissionEntry) {
	for _, pkt := range entry.Packets {
		e.ProcessPacket(pkt)
		e.totalDwords.Add(1 + uint64(len(pkt.Payload)))
	}
}

func (e *GPUEngine) ProcessSubmit(entries []*pm4.SubmissionEntry) *SubmitResult {
	id := e.submitSeq.Add(1)
	e.experiments.ApplyLatency()

	var stats SubmitBatchStats
	stats.SubmitID = id

	for _, entry := range entries {
		e.ProcessEntry(entry)
		stats.Entries++
		stats.TotalDwords += len(entry.Dwords)

		for _, pkt := range entry.Packets {
			switch pkt.Type {
			case pm4.PacketPKT4:
				stats.PKT4Packets++
				stats.RegWrites += pkt.RegCount
				if pm4.IsA8XXRegister[pkt.RegOffset] {
					stats.A8XXRegWrites += pkt.RegCount
				}
			case pm4.PacketPKT7:
				stats.PKT7Packets++
				if pm4.BarrierOpcodes[pkt.Opcode] || pkt.Opcode == pm4.CP_WAIT_FOR_IDLE {
					stats.Barriers++
				}
				if pm4.DrawOpcodes[pkt.Opcode] {
					stats.Draws++
				}
			}
		}
	}

	stats.RedundantWrites = int(e.experiments.RedundantWritesDropped)
	e.experiments.RecordBatch(stats)
	e.totalSubmits.Add(1)

	result := &SubmitResult{
		SubmitID:      id,
		Timestamp:     e.nextTimestamp(),
		BatchStats:    stats,
		WritebackBOs:  e.memTracker.GetWritebackEntries(),
		TotalSubmits:  e.totalSubmits.Load(),
		TotalDraws:    e.totalDraws.Load(),
		TotalDwords:   e.totalDwords.Load(),
	}
	e.memTracker.ResetWriteCounts()
	return result
}

type SubmitResult struct {
	SubmitID      uint64              `json:"submit_id"`
	Timestamp     uint32              `json:"timestamp"`
	Result        string              `json:"result"`
	BatchStats    SubmitBatchStats    `json:"batch_stats"`
	WritebackBOs  map[uint64][]byte   `json:"writeback_bos,omitempty"`
	TotalSubmits  uint64              `json:"total_submits"`
	TotalDraws    uint64              `json:"total_draws"`
	TotalDwords   uint64              `json:"total_dwords"`
}

func (e *GPUEngine) nextTimestamp() uint32 {
	return uint32(e.timestampSeq.Add(1))
}

func (e *GPUEngine) Stats() *ExperimentState {
	return e.experiments
}

func (e *GPUEngine) SubmitCount() uint64 {
	return e.totalSubmits.Load()
}

func (e *GPUEngine) DrawCount() uint64 {
	return e.totalDraws.Load()
}

func (e *GPUEngine) DwordCount() uint64 {
	return e.totalDwords.Load()
}
