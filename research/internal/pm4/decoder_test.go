package pm4

import (
	"testing"
)

func buildPkt4(reg uint16, cnt uint16) uint32 {
	return Pkt4Header(reg, cnt)
}

func buildPkt7(opcode uint8, cnt uint16) uint32 {
	return Pkt7Header(uint32(opcode), cnt)
}

func TestDecodePKT4(t *testing.T) {
	reg := uint16(0x0800) // CP_RB_BASE
	val := uint32(0xDEADBEEF)
	hdr := buildPkt4(reg, 1)            // write 1 register
	dwords := []uint32{hdr, val}

	packets, _ := Decode(dwords)
	if len(packets) != 1 {
		t.Fatalf("expected 1 packet, got %d", len(packets))
	}
	p := packets[0]
	if p.Type != PacketPKT4 {
		t.Errorf("expected PacketPKT4, got %v", p.Type)
	}
	if p.RegOffset != 0x0800 {
		t.Errorf("expected reg 0x0800, got 0x%04x", p.RegOffset)
	}
	if p.RegCount != 1 {
		t.Errorf("expected count 1, got %d", p.RegCount)
	}
	if len(p.Payload) != 1 || p.Payload[0] != val {
		t.Errorf("expected payload [0x%08x], got %v", val, p.Payload)
	}
}

func TestDecodePKT4Multi(t *testing.T) {
	reg := uint16(0x2100) // VFD_CONTROL
	hdr := buildPkt4(reg, 3) // 3 register values
	dwords := []uint32{hdr, 0xAA, 0xBB, 0xCC}

	packets, _ := Decode(dwords)
	if len(packets) != 1 {
		t.Fatalf("expected 1 packet, got %d", len(packets))
	}
	if packets[0].RegCount != 3 {
		t.Errorf("expected count 3, got %d", packets[0].RegCount)
	}
	if len(packets[0].Payload) != 3 {
		t.Errorf("expected 3 payload dwords, got %d", len(packets[0].Payload))
	}
}

func TestDecodePKT7(t *testing.T) {
	hdr := buildPkt7(CP_WAIT_FOR_IDLE, 0)
	dwords := []uint32{hdr}

	packets, _ := Decode(dwords)
	if len(packets) != 1 {
		t.Fatalf("expected 1 packet, got %d", len(packets))
	}
	p := packets[0]
	if p.Type != PacketPKT7 {
		t.Errorf("expected PacketPKT7, got %v", p.Type)
	}
	if p.Opcode != CP_WAIT_FOR_IDLE {
		t.Errorf("expected CP_WAIT_FOR_IDLE (0x%x), got 0x%x", CP_WAIT_FOR_IDLE, p.Opcode)
	}
	if p.PayloadSize != 0 {
		t.Errorf("expected payload 0, got %d", p.PayloadSize)
	}
}

func TestDecodePKT7Draw(t *testing.T) {
	hdr := buildPkt7(CP_DRAW_INDX, 6)
	dwords := []uint32{hdr, 0, 1, 2, 3, 4, 5}

	packets, stats := Decode(dwords)
	if len(packets) != 1 {
		t.Fatalf("expected 1 packet, got %d", len(packets))
	}
	if stats.DrawPackets != 1 {
		t.Errorf("expected 1 draw packet, got %d", stats.DrawPackets)
	}
	if stats.PKT7Count != 1 {
		t.Errorf("expected 1 PKT7, got %d", stats.PKT7Count)
	}
}

func TestDecodeMixed(t *testing.T) {
	p4 := buildPkt4(0x0800, 1)
	p7 := buildPkt7(CP_NOP, 0)
	dwords := []uint32{p4, 0x11111111, p7}

	packets, stats := Decode(dwords)
	if len(packets) != 2 {
		t.Fatalf("expected 2 packets, got %d", len(packets))
	}
	if stats.TotalDwords != 3 {
		t.Errorf("expected 3 dwords, got %d", stats.TotalDwords)
	}
	if stats.PKT4Count != 1 {
		t.Errorf("expected 1 PKT4, got %d", stats.PKT4Count)
	}
	if stats.PKT7Count != 1 {
		t.Errorf("expected 1 PKT7, got %d", stats.PKT7Count)
	}
	if stats.NOPPackets != 1 {
		t.Errorf("expected 1 NOP, got %d", stats.NOPPackets)
	}
}

func TestDecodeBarrier(t *testing.T) {
	hdr := buildPkt7(CP_WAIT_FOR_ME, 0)
	dwords := []uint32{hdr}

	_, stats := Decode(dwords)
	if stats.BarrierPackets != 1 {
		t.Errorf("expected 1 barrier, got %d", stats.BarrierPackets)
	}
}

func TestDecodeEntry(t *testing.T) {
	p4 := buildPkt4(0x0800, 1)
	p7 := buildPkt7(CP_SET_MARKER, 2)
	dwords := []uint32{p4, 0xCAFE, p7, 0x1, 0x2}

	entry, stats := DecodeEntry(dwords, "test_entry")
	if entry.Size() != 20 {
		t.Errorf("expected size 20, got %d", entry.Size())
	}
	if stats.TotalPackets != 2 {
		t.Errorf("expected 2 total packets, got %d", stats.TotalPackets)
	}
	s := entry.Summary()
	if s == "" {
		t.Error("summary should not be empty")
	}
	t.Log(s)
}

func TestDecodeIndirectBuffer(t *testing.T) {
	hdr := buildPkt7(CP_INDIRECT_BUFFER, 3)
	dwords := []uint32{hdr, 0, 0, 0}

	packets, _ := Decode(dwords)
	if packets[0].Type != PacketIndirectBuffer {
		t.Errorf("expected PacketIndirectBuffer, got %v", packets[0].Type)
	}
}

func TestDecodeGarbage(t *testing.T) {
	dwords := []uint32{0x00000000, 0xFFFFFFFF, 0xDEADBEEF}
	packets, _ := Decode(dwords)
	if len(packets) != 0 {
		t.Errorf("expected 0 packets from garbage, got %d", len(packets))
	}
}
