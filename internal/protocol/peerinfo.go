// internal/protocol/peerinfo.go
package protocol

// PeerInfo is what two mwb-linux peers tell each other right after they
// connect, so neither user has to type the other's settings: PowerToys
// exchanges the equivalent through its own machine matrix.
//
// It travels in a LinuxPeerInfo packet, reusing the 16-byte MOUSEDATA
// payload, as NextMachine already does for its target ID:
//
//	X          screen width in pixels, 0 if unknown
//	Y          screen height in pixels, 0 if unknown
//	WheelDelta flags: bit 0 = sharing active, bit 1 = peer is on the right
//	DwFlags    format version (peerInfoVersion)
type PeerInfo struct {
	ScreenWidth  int32
	ScreenHeight int32
	// Edge is the side of the sender's screen the receiver sits on:
	// "left" or "right". In a correct layout the two edges are opposite.
	Edge string
	// Sharing reports that the sender actually forwards its own input when
	// the cursor crosses: bidirectional is on and capture started.
	Sharing bool
}

const (
	peerInfoVersion     = 1
	peerInfoSharing     = 1 << 0
	peerInfoEdgeRight   = 1 << 1
	peerInfoMaxScreenPx = 65535
)

// NewPeerInfoPacket builds a LinuxPeerInfo packet.
func NewPeerInfoPacket(src, des uint32, info PeerInfo) *Packet {
	p := &Packet{Type: LinuxPeerInfo, Src: src, Des: des}
	if validScreen(info.ScreenWidth, info.ScreenHeight) {
		p.Mouse.X, p.Mouse.Y = info.ScreenWidth, info.ScreenHeight
	}
	var flags int32
	if info.Sharing {
		flags |= peerInfoSharing
	}
	if info.Edge == "right" {
		flags |= peerInfoEdgeRight
	}
	p.Mouse.WheelDelta = flags
	p.Mouse.DwFlags = peerInfoVersion
	return p
}

// PeerInfo decodes a LinuxPeerInfo packet. ok is false for any other packet
// type or an unknown format version. An implausible screen size is reported
// as unknown (0×0) rather than trusted.
func (p *Packet) PeerInfo() (info PeerInfo, ok bool) {
	if p.Type != LinuxPeerInfo || p.Mouse.DwFlags != peerInfoVersion {
		return PeerInfo{}, false
	}
	if validScreen(p.Mouse.X, p.Mouse.Y) {
		info.ScreenWidth, info.ScreenHeight = p.Mouse.X, p.Mouse.Y
	}
	info.Sharing = p.Mouse.WheelDelta&peerInfoSharing != 0
	info.Edge = "left"
	if p.Mouse.WheelDelta&peerInfoEdgeRight != 0 {
		info.Edge = "right"
	}
	return info, true
}

func validScreen(w, h int32) bool {
	return w > 0 && h > 0 && w <= peerInfoMaxScreenPx && h <= peerInfoMaxScreenPx
}
