// cmd/mwb/peer_test.go
package main

import (
	"net"
	"reflect"
	"testing"

	"github.com/lucky-verma/mwb-linux/internal/protocol"
)

func TestLocalAddressStrings(t *testing.T) {
	cidr := func(s string) net.Addr {
		ip, n, err := net.ParseCIDR(s)
		if err != nil {
			t.Fatal(err)
		}
		n.IP = ip
		return n
	}
	got := localAddressStrings([]net.Addr{
		cidr("127.0.0.1/8"), cidr("10.249.1.20/24"), cidr("192.0.2.2/24"),
		cidr("fe80::1/64"), cidr("fd00::5/64"), cidr("8.8.8.8/32"),
	})
	want := []string{"10.249.1.20", "fd00::5"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("localAddressStrings = %v, want %v", got, want)
	}
}

func TestPeerHostPrefersConfigured(t *testing.T) {
	if got := peerHost("10.0.0.2", nil); got != "10.0.0.2" {
		t.Fatalf("peerHost = %q", got)
	}
}

type fakeScreen struct{ w, h int32 }

func (f *fakeScreen) SetRemoteScreen(w, h int32) { f.w, f.h = w, h }

func TestApplyPeerInfo(t *testing.T) {
	scr := &fakeScreen{w: 1928, h: 1076} // a mistyped remote_width/height
	clash, nobody := applyPeerInfo(protocol.PeerInfo{ScreenWidth: 1920, ScreenHeight: 1080, Edge: "right", Sharing: true}, "left", false, scr)
	if scr.w != 1920 || scr.h != 1080 {
		t.Fatalf("remote screen = %dx%d; the peer's own measurement should win", scr.w, scr.h)
	}
	if clash || nobody {
		t.Fatalf("opposite sides with one sharing: clash=%v nobody=%v", clash, nobody)
	}

	// Unknown size keeps the configured one.
	scr = &fakeScreen{w: 1280, h: 800}
	applyPeerInfo(protocol.PeerInfo{Edge: "right"}, "left", true, scr)
	if scr.w != 1280 || scr.h != 800 {
		t.Fatal("an unknown peer size replaced the configured one")
	}

	if clash, _ := applyPeerInfo(protocol.PeerInfo{Edge: "left"}, "left", true, nil); !clash {
		t.Fatal("both on the left is a clash")
	}
	if _, nobody := applyPeerInfo(protocol.PeerInfo{Edge: "right"}, "left", false, nil); !nobody {
		t.Fatal("neither sharing should be reported")
	}
}

func TestMirrorPeerSide(t *testing.T) {
	const unknown = protocol.EdgeAgeUnknown
	for _, tc := range []struct {
		name               string
		localEdge          string
		localAge, peerAge  uint32
		localName, peerSrc string
		peerEdge           string
		adopt              bool
		want               string
	}{
		{"no clash", "left", 5, 1, "AA-000-526", "mystogan", "right", false, "left"},
		{"peer chose more recently", "right", 30, 2, "AA-000-526", "mystogan", "right", true, "left"},
		{"this machine chose more recently", "right", 2, 30, "AA-000-526", "mystogan", "right", false, "right"},
		{"never chosen anywhere: name sorting last adopts", "left", unknown, unknown, "mystogan", "AA-000-526", "left", true, "right"},
		{"never chosen anywhere: name sorting first keeps", "left", unknown, unknown, "AA-000-526", "mystogan", "left", false, "left"},
		{"older peer build has no age: a recorded choice wins", "right", 10, unknown, "zz", "aa", "right", false, "right"},
		{"same name never adopts", "left", 3, 3, "box", "box", "left", false, "left"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			peer := protocol.PeerInfo{Edge: tc.peerEdge, EdgeAge: tc.peerAge}
			adopt, edge := mirrorPeerSide(tc.localEdge, tc.localAge, tc.localName, peer, tc.peerSrc)
			if adopt != tc.adopt || edge != tc.want {
				t.Fatalf("adopt=%v edge=%q, want adopt=%v edge=%q", adopt, edge, tc.adopt, tc.want)
			}
		})
	}
}

// Both machines evaluate the same pair of ages; exactly one may adopt, or
// the sides would flip together and clash again.
func TestMirrorPeerSideExactlyOneAdopts(t *testing.T) {
	ages := []uint32{0, 1, 7, protocol.EdgeAgeUnknown}
	for _, a := range ages {
		for _, b := range ages {
			adoptA, _ := mirrorPeerSide("left", a, "alpha", protocol.PeerInfo{Edge: "left", EdgeAge: b}, "beta")
			adoptB, _ := mirrorPeerSide("left", b, "beta", protocol.PeerInfo{Edge: "left", EdgeAge: a}, "alpha")
			if adoptA == adoptB {
				t.Fatalf("ages %d/%d: alpha adopts=%v, beta adopts=%v", a, b, adoptA, adoptB)
			}
		}
	}
}
