package stub

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"sync"

	"github.com/idans/winlator-cmod-builder/update-graphviz/internal/pm4"
)

type SubmitRequest struct {
	SubmitID uint64            `json:"submit_id"`
	Entries  []CSEntryRequest  `json:"entries"`
	TrackBOs []BOInfo          `json:"tracked_bos,omitempty"`
	Fences   []FenceRequest    `json:"fences,omitempty"`
}

type CSEntryRequest struct {
	Name   string   `json:"name,omitempty"`
	Dwords []uint32 `json:"dwords"`
	Size   uint32   `json:"size"`
}

type BOInfo struct {
	GPUVA uint64 `json:"gpuva"`
	Size  uint64 `json:"size"`
	Name  string `json:"name,omitempty"`
}

type FenceRequest struct {
	ID       uint32 `json:"id"`
	SeqnoPtr uint64 `json:"seqno_ptr,omitempty"`
	Seqno    uint32 `json:"seqno"`
}

type SubmitResponse struct {
	SubmitID     uint64              `json:"submit_id"`
	Result       string              `json:"result"`
	Timestamp    uint32              `json:"timestamp"`
	BatchStats   SubmitBatchStats    `json:"batch_stats"`
	WritebackBOs map[string][]byte   `json:"writeback_bos,omitempty"`
	FenceValues  []FenceValue        `json:"fence_values,omitempty"`
	TotalSubmits uint64              `json:"total_submits"`
	TotalDraws   uint64              `json:"total_draws"`
	TotalDwords  uint64              `json:"total_dwords"`
	Experiment   *ExperimentSnap     `json:"experiment,omitempty"`
}

type FenceValue struct {
	ID    uint32 `json:"id"`
	Value uint32 `json:"value"`
}

type ExperimentSnap struct {
	RedundantDropped uint64 `json:"redundant_writes_dropped"`
	BarriersSkipped  uint64 `json:"barriers_skipped"`
	LatencyInjectUs  int64  `json:"latency_inject_us"`
}

type RPCServer struct {
	engine  *GPUEngine
	socket  string
	listener net.Listener
	mu      sync.Mutex
	running bool
}

func NewRPCServer(engine *GPUEngine, socketPath string) *RPCServer {
	return &RPCServer{
		engine: engine,
		socket: socketPath,
	}
}

func (s *RPCServer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	os.Remove(s.socket)

	l, err := net.Listen("unix", s.socket)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	s.listener = l
	s.running = true

	go s.acceptLoop()
	return nil
}

func (s *RPCServer) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	if s.listener != nil {
		s.listener.Close()
		os.Remove(s.socket)
	}
}

func (s *RPCServer) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			s.mu.Lock()
			if !s.running {
				s.mu.Unlock()
				return
			}
			s.mu.Unlock()
			continue
		}
		go s.handleConn(conn)
	}
}

func (s *RPCServer) handleConn(conn net.Conn) {
	defer conn.Close()

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	for {
		var req SubmitRequest
		if err := decoder.Decode(&req); err != nil {
			if err != io.EOF {
				fmt.Fprintf(os.Stderr, "RPC decode error: %v\n", err)
			}
			return
		}

		resp := s.handleSubmit(&req)
		if err := encoder.Encode(resp); err != nil {
			fmt.Fprintf(os.Stderr, "RPC encode error: %v\n", err)
			return
		}
	}
}

func (s *RPCServer) handleSubmit(req *SubmitRequest) *SubmitResponse {
	for _, bo := range req.TrackBOs {
		s.engine.memTracker.AddBO(bo.GPUVA, bo.Size, bo.Name)
	}

	entries := make([]*pm4.SubmissionEntry, len(req.Entries))
	for i, e := range req.Entries {
		name := e.Name
		if name == "" {
			name = fmt.Sprintf("entry_%d", i)
		}
		entries[i] = &pm4.SubmissionEntry{
			Name:    name,
			Dwords:  e.Dwords,
			Packets: nil,
		}
	}

	for _, entry := range entries {
		packets, _ := pm4.Decode(entry.Dwords)
		entry.Packets = packets
	}

	result := s.engine.ProcessSubmit(entries)

	resp := &SubmitResponse{
		SubmitID:   req.SubmitID,
		Result:     "VK_SUCCESS",
		Timestamp:  result.Timestamp,
		BatchStats: result.BatchStats,
		TotalSubmits: s.engine.SubmitCount(),
		TotalDraws:   s.engine.DrawCount(),
		TotalDwords:  s.engine.DwordCount(),
		Experiment: &ExperimentSnap{
			RedundantDropped: s.engine.Stats().RedundantWritesDropped,
			BarriersSkipped:  s.engine.Stats().BarriersSkipped,
			LatencyInjectUs:  int64(s.engine.Stats().TotalLatencyInjected),
		},
	}

	if len(result.WritebackBOs) > 0 {
		resp.WritebackBOs = make(map[string][]byte)
		for gpuva, data := range result.WritebackBOs {
			resp.WritebackBOs[fmt.Sprintf("0x%x", gpuva)] = data
		}
	}

	for i := range req.Fences {
		resp.FenceValues = append(resp.FenceValues, FenceValue{
			ID:    req.Fences[i].ID,
			Value: result.Timestamp,
		})
	}

	return resp
}
