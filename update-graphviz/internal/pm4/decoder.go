package pm4

import "fmt"

type PacketType int

const (
	PacketUnknown PacketType = iota
	PacketPKT4
	PacketPKT7
	PacketIndirectBuffer
)

type PM4Packet struct {
	Type        PacketType `json:"type"`
	Header      uint32     `json:"header"`
	Payload     []uint32   `json:"payload"`

	// For PKT4:
	RegOffset  uint32 `json:"reg_offset,omitempty"`
	RegCount   int    `json:"reg_count,omitempty"`
	RegName    string `json:"reg_name,omitempty"`

	// For PKT7:
	Opcode      uint32 `json:"opcode,omitempty"`
	OpcodeName  string `json:"opcode_name,omitempty"`
	PayloadSize int    `json:"payload_size,omitempty"`
}

type DecodeStats struct {
	TotalDwords    int
	TotalPackets   int
	PKT4Count      int
	PKT7Count      int
	RegWrites      int
	BarrierPackets int
	DrawPackets    int
	A8XXRegWrites  int
	NOPPackets      int
	UnknownOpcodes map[uint32]int
}

func (s *DecodeStats) Merge(other *DecodeStats) {
	s.TotalDwords += other.TotalDwords
	s.TotalPackets += other.TotalPackets
	s.PKT4Count += other.PKT4Count
	s.PKT7Count += other.PKT7Count
	s.RegWrites += other.RegWrites
	s.BarrierPackets += other.BarrierPackets
	s.DrawPackets += other.DrawPackets
	s.A8XXRegWrites += other.A8XXRegWrites
	s.NOPPackets += other.NOPPackets
	for k, v := range other.UnknownOpcodes {
		s.UnknownOpcodes[k] += v
	}
}

func NewDecodeStats() *DecodeStats {
	return &DecodeStats{UnknownOpcodes: make(map[uint32]int)}
}

func Decode(dwords []uint32) ([]PM4Packet, *DecodeStats) {
	var packets []PM4Packet
	stats := NewDecodeStats()
	stats.TotalDwords = len(dwords)

	i := 0
	for i < len(dwords) {
		hdr := dwords[i]

		switch {
		case isType4(hdr):
			pkt := decodeType4(dwords, i)
			packets = append(packets, pkt)
			stats.PKT4Count++
			stats.RegWrites += pkt.RegCount
			if IsA8XXRegister[pkt.RegOffset] {
				stats.A8XXRegWrites += pkt.RegCount
			}
			i += 1 + pkt.RegCount

		case isType7(hdr):
			pkt := decodeType7(dwords, i)
			if isIndirectBuffer(pkt) {
				pkt.Type = PacketIndirectBuffer
			}
			packets = append(packets, pkt)
			stats.PKT7Count++
			if BarrierOpcodes[pkt.Opcode] || pkt.Opcode == CP_WAIT_FOR_IDLE {
				stats.BarrierPackets++
			}
			if DrawOpcodes[pkt.Opcode] {
				stats.DrawPackets++
			}
			if pkt.Opcode == CP_NOP {
				stats.NOPPackets++
			}
			if _, known := OpcodeNames[pkt.Opcode]; !known {
				stats.UnknownOpcodes[pkt.Opcode]++
			}
			i += 1 + pkt.PayloadSize

		default:
			i++
		}
	}

	stats.TotalPackets = len(packets)
	return packets, stats
}

func DecodeEntry(dwords []uint32, name string) (*SubmissionEntry, *DecodeStats) {
	packets, stats := Decode(dwords)
	return &SubmissionEntry{
		Name:    name,
		Dwords:  dwords,
		Packets: packets,
	}, stats
}

type SubmissionEntry struct {
	Name    string       `json:"name"`
	Dwords  []uint32     `json:"dwords"`
	Packets []PM4Packet  `json:"packets"`
}

func (e *SubmissionEntry) Size() int { return len(e.Dwords) * 4 }

func (e *SubmissionEntry) Summary() string {
	barriers := 0
	draws := 0
	regWrites := 0
	a8xxWrites := 0
	for _, p := range e.Packets {
		switch p.Type {
		case PacketPKT4:
			regWrites += p.RegCount
			if IsA8XXRegister[p.RegOffset] {
				a8xxWrites += p.RegCount
			}
		case PacketPKT7:
			if BarrierOpcodes[p.Opcode] {
				barriers++
			}
			if DrawOpcodes[p.Opcode] {
				draws++
			}
		}
	}
	return fmt.Sprintf("%s: %d dwords, %d packets, %d reg writes (%d A8XX), %d barriers, %d draws",
		e.Name, len(e.Dwords), len(e.Packets), regWrites, a8xxWrites, barriers, draws)
}

func isType4(hdr uint32) bool {
	if (hdr & 0xF0000000) != Type4Mask {
		return false
	}
	reg := type4RegOffset(hdr)
	cnt := type4RegCount(hdr)
	return parityCheck(reg, (hdr>>27)&1) && parityCheck(uint32(cnt), (hdr>>7)&1)
}

func isType7(hdr uint32) bool {
	if (hdr & 0xF0000000) != Type7Mask {
		return false
	}
	if (hdr & 0x0F000000) != 0 {
		return false
	}
	op := type7Opcode(hdr)
	cnt := type7PayloadSize(hdr)
	return parityCheck(op, (hdr>>23)&1) && parityCheck(uint32(cnt), (hdr>>15)&1)
}

func decodeType4(dwords []uint32, i int) PM4Packet {
	hdr := dwords[i]
	reg := type4RegOffset(hdr)
	count := type4RegCount(hdr)
	payload := safeSlice(dwords, i+1, count)
	return PM4Packet{
		Type:      PacketPKT4,
		Header:    hdr,
		Payload:   payload,
		RegOffset: reg,
		RegCount:  count,
		RegName:   RegNameByOffset[reg],
	}
}

func decodeType7(dwords []uint32, i int) PM4Packet {
	hdr := dwords[i]
	op := type7Opcode(hdr)
	count := type7PayloadSize(hdr)
	payload := safeSlice(dwords, i+1, count)
	return PM4Packet{
		Type:        PacketPKT7,
		Header:      hdr,
		Payload:     payload,
		Opcode:      op,
		OpcodeName:  OpcodeName(op),
		PayloadSize: count,
	}
}

func isIndirectBuffer(p PM4Packet) bool {
	return p.Opcode == CP_INDIRECT_BUFFER ||
		p.Opcode == CP_INDIRECT_BUFFER_CHAIN ||
		p.Opcode == CP_INDIRECT_BUFFER_PFD
}

func type4RegOffset(hdr uint32) uint32 { return (hdr >> 8) & 0x7FFFF }
func type4RegCount(hdr uint32) int     { return int(hdr & 0x7F) }
func type7Opcode(hdr uint32) uint32    { return (hdr >> 16) & 0x7F }
func type7PayloadSize(hdr uint32) int  { return int(hdr & 0x3FFF) }

func safeSlice(dwords []uint32, start, count int) []uint32 {
	if start >= len(dwords) {
		return nil
	}
	end := start + count
	if end > len(dwords) {
		end = len(dwords)
	}
	return dwords[start:end]
}

func parityCheck(val uint32, expectedBit uint32) bool {
	val ^= val >> 16
	val ^= val >> 8
	val ^= val >> 4
	val &= 0xf
	actual := (uint32(0x9669) >> val) & 1
	return actual == expectedBit
}

func (p PM4Packet) String() string {
	switch p.Type {
	case PacketPKT4:
		name := p.RegName
		if name == "" {
			name = fmt.Sprintf("reg_0x%04x", p.RegOffset)
		}
		if p.RegCount == 1 {
			return fmt.Sprintf("PKT4  %-40s = 0x%08x", name, p.Payload[0])
		}
		return fmt.Sprintf("PKT4  %-40s [%d vals]", name, p.RegCount)
	case PacketPKT7:
		name := p.OpcodeName
		if name == "" {
			name = fmt.Sprintf("op_0x%02x", p.Opcode)
		}
		return fmt.Sprintf("PKT7  %-40s (%d dwords)", name, p.PayloadSize)
	default:
		return fmt.Sprintf("????  header=0x%08x", p.Header)
	}
}
