// cmd/mwb/peer.go
package main

import (
	"log/slog"
	"net"
	"time"

	"github.com/lucky-verma/mwb-linux/internal/capture"
	"github.com/lucky-verma/mwb-linux/internal/config"
	"github.com/lucky-verma/mwb-linux/internal/network"
	"github.com/lucky-verma/mwb-linux/internal/protocol"
	"github.com/lucky-verma/mwb-linux/internal/status"
)

// peerHost is where to dial the peer's file channel: the configured host, or,
// for a Linux peer waiting without one, the address the other machine
// connected from.
func peerHost(configured string, conn *network.Conn) string {
	if configured != "" {
		return configured
	}
	return conn.RemoteHost()
}

// localAddresses lists this machine's local-network addresses, the ones the
// other computer can enter as host to reach a waiting machine.
func localAddresses() []string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	return localAddressStrings(addrs)
}

func localAddressStrings(addrs []net.Addr) []string {
	var out []string
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		// Private ranges only: those are what a waiting peer accepts from, and
		// a link-local IPv6 address is not something to type into a form.
		if ipn.IP.IsPrivate() {
			out = append(out, ipn.IP.String())
		}
	}
	return out
}

// peerInfoReceiver is what a Linux peer's settings are applied to; the
// capturer satisfies it. A nil receiver (receive-only) skips the screen size.
type peerInfoReceiver interface {
	SetRemoteScreen(w, h int32)
}

// applyPeerInfo uses a Linux peer's reported settings and returns the
// problems worth telling the user about.
func applyPeerInfo(info protocol.PeerInfo, localEdge string, localSharing bool, rcv peerInfoReceiver) (sideClash, nobodyShares bool) {
	if rcv != nil && info.ScreenWidth > 0 && info.ScreenHeight > 0 {
		// The peer measured its own screen; that beats remote_width/height.
		rcv.SetRemoteScreen(info.ScreenWidth, info.ScreenHeight)
	}
	return info.Edge == localEdge, !info.Sharing && !localSharing
}

// sideSync keeps the two machines' sides mirrored. The side is one fact
// ("the peer is on my left" = "I am on the peer's right"), so when both put
// the other on the same side, the more recent choice wins and the other
// machine saves the mirror of it.
type sideSync struct {
	edge    *string // this machine's side; replaced when the peer's choice is adopted
	changed *int64  // when *edge was chosen, Unix seconds; 0 = unknown
	name    string  // this machine's name, for the tie-break
	// save persists an adopted side; nil means never adopt (the side came
	// from the -edge flag, which the user asked for explicitly).
	save func(edge string, changed int64) error
}

// mirrorPeerSide decides whether this machine should give up its side for
// the mirror of the peer's. Both machines run it on the same two ages, so
// exactly one of them adopts: the one whose choice is older or, on a tie,
// whose name sorts last. Equal names never adopt; the clash is reported.
func mirrorPeerSide(localEdge string, localAge uint32, localName string, peer protocol.PeerInfo, peerName string) (adopt bool, edge string) {
	if peer.Edge != localEdge {
		return false, localEdge
	}
	if peer.EdgeAge < localAge || (peer.EdgeAge == localAge && localName > peerName) {
		return true, oppositeEdge(peer.Edge)
	}
	return false, localEdge
}

func oppositeEdge(edge string) string {
	if edge == "left" {
		return "right"
	}
	return "left"
}

// adoptPeerSide saves the mirror of the peer's side and drops the connection,
// so the reconnect restarts capture on the new edge. It reports false, leaving
// everything as it was, when the side cannot be saved.
func adoptPeerSide(conn *network.Conn, sync sideSync, edge string, peerAge uint32, st *status.Writer) bool {
	// Keep the peer's choice time, so the side's age stays that of the choice.
	var changed int64
	if peerAge != protocol.EdgeAgeUnknown {
		changed = time.Now().Unix() - int64(peerAge)*60
	}
	if err := sync.save(edge, changed); err != nil {
		slog.Warn("could not save the side chosen on the other computer", "edge", edge, "remote", conn.RemoteName, "err", err)
		return false
	}
	*sync.edge, *sync.changed = edge, changed
	slog.Info("both computers put the other on the same side; switched to match the other computer's more recent choice",
		"remote", conn.RemoteName, "edge", edge)
	if err := st.AdoptedEdge(edge, conn.RemoteName); err != nil {
		slog.Debug("status file not updated", "err", err)
	}
	_ = conn.Close()
	return true
}

// exchangePeerInfo sends this machine's PeerInfo and wires the handler to
// apply the peer's. Call before ReceiveLoop, which delivers the reply.
func exchangePeerInfo(conn *network.Conn, h *network.Handler, cp *capture.Capturer, sync sideSync, sharing bool, st *status.Writer) {
	var rcv peerInfoReceiver
	if cp != nil {
		rcv = cp
	}
	edge := *sync.edge
	// Compare against exactly the age that was sent, so both machines decide
	// on the same pair of numbers.
	myAge := config.EdgeAge(*sync.changed, time.Now(), protocol.EdgeAgeUnknown)
	h.OnPeerInfo = func(info protocol.PeerInfo) {
		slog.Info("peer settings received", "remote", conn.RemoteName,
			"screen_width", info.ScreenWidth, "screen_height", info.ScreenHeight,
			"peer_edge", info.Edge, "peer_sharing", info.Sharing)
		sideClash, nobodyShares := applyPeerInfo(info, edge, sharing, rcv)
		if sideClash && sync.save != nil {
			if adopt, mirrored := mirrorPeerSide(edge, myAge, sync.name, info, conn.RemoteName); adopt {
				if adoptPeerSide(conn, sync, mirrored, info.EdgeAge, st) {
					return // reconnecting with the new side
				}
			} else {
				slog.Info("both computers put the other on the same side; this computer's choice is newer, so the other computer switches to match",
					"edge", edge, "remote", conn.RemoteName)
				sideClash = false
			}
		}
		if sideClash {
			slog.Warn("both computers put the other on the same side; on one of them, choose the other side",
				"edge", edge, "remote", conn.RemoteName)
		}
		if nobodyShares {
			slog.Warn("connected, but neither computer shares its keyboard and mouse; turn on bidirectional on at least one",
				"remote", conn.RemoteName)
		}
		if err := st.Peer(info.ScreenWidth, info.ScreenHeight, info.Edge, info.Sharing); err != nil {
			slog.Debug("status file not updated", "err", err)
		}
	}

	mine := protocol.PeerInfo{Edge: edge, Sharing: sharing, EdgeAge: myAge}
	capture.DetectDisplay() // xrandr needs DISPLAY, which detection sets
	if s, ok := capture.DetectScreenSize(); ok {
		mine.ScreenWidth, mine.ScreenHeight = s.Width, s.Height
	}
	if err := conn.SendPacket(protocol.NewPeerInfoPacket(conn.MachineID, conn.RemoteID, mine)); err != nil {
		slog.Warn("could not send settings to the peer", "err", err)
	}
}
