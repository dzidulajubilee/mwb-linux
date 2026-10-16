// cmd/mwb/peer.go
package main

import (
	"log/slog"
	"net"

	"github.com/lucky-verma/mwb-linux/internal/capture"
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

// exchangePeerInfo sends this machine's PeerInfo and wires the handler to
// apply the peer's. Call before ReceiveLoop, which delivers the reply.
func exchangePeerInfo(conn *network.Conn, h *network.Handler, cp *capture.Capturer, edge string, sharing bool, st *status.Writer) {
	var rcv peerInfoReceiver
	if cp != nil {
		rcv = cp
	}
	h.OnPeerInfo = func(info protocol.PeerInfo) {
		slog.Info("peer settings received", "remote", conn.RemoteName,
			"screen_width", info.ScreenWidth, "screen_height", info.ScreenHeight,
			"peer_edge", info.Edge, "peer_sharing", info.Sharing)
		sideClash, nobodyShares := applyPeerInfo(info, edge, sharing, rcv)
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

	mine := protocol.PeerInfo{Edge: edge, Sharing: sharing}
	capture.DetectDisplay() // xrandr needs DISPLAY, which detection sets
	if s, ok := capture.DetectScreenSize(); ok {
		mine.ScreenWidth, mine.ScreenHeight = s.Width, s.Height
	}
	if err := conn.SendPacket(protocol.NewPeerInfoPacket(conn.MachineID, conn.RemoteID, mine)); err != nil {
		slog.Warn("could not send settings to the peer", "err", err)
	}
}
