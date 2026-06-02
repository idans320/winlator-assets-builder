package stub

import (
	"encoding/binary"
	"sync"
)

type TrackedBO struct {
	GPUVA  uint64
	Size   uint64
	Name   string
	dataMu sync.RWMutex
	Data   []byte
	Writes uint32
}

type MemoryTracker struct {
	mu  sync.RWMutex
	bos map[uint64]*TrackedBO
}

func NewMemoryTracker() *MemoryTracker {
	return &MemoryTracker{bos: make(map[uint64]*TrackedBO)}
}

func (m *MemoryTracker) AddBO(gpuva uint64, size uint64, name string) *TrackedBO {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.bos[gpuva]; ok {
		return existing
	}
	bo := &TrackedBO{
		GPUVA: gpuva,
		Size:  size,
		Name:  name,
		Data:  make([]byte, size),
	}
	m.bos[gpuva] = bo
	return bo
}

func (m *MemoryTracker) GetBO(gpuva uint64) *TrackedBO {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.bos[gpuva]
}

func (m *MemoryTracker) WriteDword(addr uint64, value uint32) {
	m.mu.RLock()
	bo, ok := m.bos[addr&^0xFFF]
	m.mu.RUnlock()
	if !ok || addr < bo.GPUVA || addr+4 > bo.GPUVA+bo.Size {
		return
	}
	offset := addr - bo.GPUVA
	bo.dataMu.Lock()
	binary.LittleEndian.PutUint32(bo.Data[offset:], value)
	bo.Writes++
	bo.dataMu.Unlock()
}

func (m *MemoryTracker) WriteQword(addr uint64, value uint64) {
	m.mu.RLock()
	bo, ok := m.bos[addr&^0xFFF]
	m.mu.RUnlock()
	if !ok || addr < bo.GPUVA || addr+8 > bo.GPUVA+bo.Size {
		return
	}
	offset := addr - bo.GPUVA
	bo.dataMu.Lock()
	binary.LittleEndian.PutUint64(bo.Data[offset:], value)
	bo.Writes += 2
	bo.dataMu.Unlock()
}

func (m *MemoryTracker) GetWritebackEntries() map[uint64][]byte {
	result := make(map[uint64][]byte)
	m.mu.RLock()
	defer m.mu.RUnlock()
	for gpuva, bo := range m.bos {
		if bo.Writes > 0 {
			bo.dataMu.RLock()
			result[gpuva] = append([]byte(nil), bo.Data...)
			bo.dataMu.RUnlock()
		}
	}
	return result
}

func (m *MemoryTracker) ResetWriteCounts() {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, bo := range m.bos {
		bo.Writes = 0
	}
}
