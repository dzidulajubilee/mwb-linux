// internal/protocol/peerinfo_test.go
package protocol

import "testing"

func TestPeerInfoRoundTripOverTheWire(t *testing.T) {
	for _, in := range []PeerInfo{
		{ScreenWidth: 1920, ScreenHeight: 1080, Edge: "right", Sharing: true},
		{ScreenWidth: 2560, ScreenHeight: 1440, Edge: "left", Sharing: false},
		{Edge: "left"}, // screen size unknown
	} {
		buf := NewPeerInfoPacket(7, 9, in).Marshal()
		if len(buf) != PacketSize {
			t.Fatalf("LinuxPeerInfo is %d bytes, want a plain %d-byte packet", len(buf), PacketSize)
		}
		got, err := UnmarshalPacket(buf)
		if err != nil {
			t.Fatal(err)
		}
		out, ok := got.PeerInfo()
		if !ok || out != in || got.Src != 7 || got.Des != 9 {
			t.Fatalf("round trip: sent %+v, got %+v ok=%v src=%d des=%d", in, out, ok, got.Src, got.Des)
		}
	}
}

func TestPeerInfoRejectsOtherPacketsAndVersions(t *testing.T) {
	if _, ok := (&Packet{Type: Mouse}).PeerInfo(); ok {
		t.Fatal("a Mouse packet decoded as PeerInfo")
	}
	p := NewPeerInfoPacket(1, 2, PeerInfo{Edge: "left"})
	p.Mouse.DwFlags = 99
	if _, ok := p.PeerInfo(); ok {
		t.Fatal("an unknown format version was accepted")
	}
}

func TestPeerInfoIgnoresImplausibleScreen(t *testing.T) {
	p := NewPeerInfoPacket(1, 2, PeerInfo{Edge: "left"})
	p.Mouse.X, p.Mouse.Y = -5, 70000
	info, ok := p.PeerInfo()
	if !ok || info.ScreenWidth != 0 || info.ScreenHeight != 0 {
		t.Fatalf("implausible size not treated as unknown: %+v", info)
	}
}

func TestLinuxPeerInfoIsNotAMatrixOrBigPacket(t *testing.T) {
	// Types with bit 128 set are Matrix packets, and big packets carry a
	// 32-byte name; either would change how a receiver frames it.
	if LinuxPeerInfo&Matrix == Matrix || IsBigPacket(LinuxPeerInfo) {
		t.Fatal("LinuxPeerInfo must be a plain 32-byte packet")
	}
}
