package network

import (
	"errors"
	"net"
	"testing"
	"time"

	"github.com/lucky-verma/mwb-linux/internal/protocol"
)

// A heartbeat to everyone gets one answer; an answer gets none, so two mwb
// peers cannot echo one heartbeat between them forever.
func TestHeartbeatAnswerIsNotAnswered(t *testing.T) {
	a, b := connectedPair(t)
	defer func() { _ = a.Close(); _ = b.Close() }()
	done := make(chan error, 1)
	go func() { done <- ReceiveLoop(a, &Handler{}) }()

	send := func(des uint32) {
		t.Helper()
		hb := &protocol.Packet{Type: protocol.HeartbeatEx, Src: b.MachineID, Des: des}
		if err := b.SendPacket(hb); err != nil {
			t.Fatal(err)
		}
	}
	// nextAnswer skips the rest of the handshake and a's own heartbeats,
	// which go to everyone.
	nextAnswer := func(wait time.Duration) (*protocol.Packet, error) {
		_ = b.raw.SetReadDeadline(time.Now().Add(wait))
		for {
			p, err := b.RecvPacket()
			if err != nil || (p.Type == protocol.HeartbeatEx && p.Des != protocol.IDAll) {
				return p, err
			}
		}
	}

	send(protocol.IDAll)
	resp, err := nextAnswer(2 * time.Second)
	if err != nil {
		t.Fatalf("no answer to a heartbeat: %v", err)
	}
	// The heartbeat b sent while connecting is answered too.
	for err == nil {
		_, err = nextAnswer(300 * time.Millisecond)
	}

	send(resp.Src) // echo the answer back, as an older mwb does
	var ne net.Error
	if _, err := nextAnswer(300 * time.Millisecond); err == nil {
		t.Fatal("an answer was answered")
	} else if !errors.As(err, &ne) || !ne.Timeout() {
		t.Fatalf("read: %v", err)
	}
}
