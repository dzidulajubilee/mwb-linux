//go:build linux

package capture

import (
	"log/slog"
	"time"

	"github.com/lucky-verma/mwb-linux/internal/group"
)

// Group mode: this machine is one screen in a row of up to six. Whichever
// machine's keyboard and mouse are used drives the one shared cursor; the
// others follow the group's announcements of where it is (Arrive, Follow)
// and forward their own input there.

// switchToGroup hands the cursor to the machine past side of this screen.
func (c *Capturer) switchToGroup(side string, y int32) {
	t, self, ok := c.nav.Beyond("", side)
	if !ok || self || t.Conn == nil {
		return // no machine online that way: the edge is a wall
	}
	now := time.Now()
	if now.Sub(c.lastSwitch) < 100*time.Millisecond {
		return
	}
	c.lastSwitch = now
	slog.Info("screen edge hit, moving the cursor", "edge", side, "to", t.Name, "y", y)

	entryY := int32(float64(y) / float64(c.screen.Height) * 65535)
	c.mu.Lock()
	c.active = false
	c.switchSent = now
	c.edgeY = y
	c.at, c.conn = t.Name, t.Conn
	c.remoteW, c.remoteH = t.Width, t.Height
	c.edgeSide = side // the direction of travel: land on the far side of it
	pixelX, entryX := c.remoteEntryLocked()
	c.remoteX = pixelX
	c.remoteY = int32(float64(y) / float64(c.screen.Height) * float64(c.remoteH))
	c.canReturn = false
	conn, send := c.conn, c.sendFn
	c.mu.Unlock()

	c.applyIsolation()
	go func() {
		// The announcement goes first on every connection, so the target
		// knows the cursor is coming before the burst arrives.
		c.nav.Moved(t.Name, group.Opposite(side), entryY)
		sendEntryBurst(conn, send, entryX, entryY)
	}()
}

// crossRemoteLocked moves the cursor on when the virtual cursor reaches a
// side of the remote screen it is on: back here, or to the next machine.
// It reports whether the cursor left that screen. c.mu must be held; it may
// be released and re-acquired.
func (c *Capturer) crossRemoteLocked() bool {
	side := ""
	switch {
	case c.remoteX <= 0:
		side = "left"
	case c.remoteX >= c.remoteW-1:
		side = "right"
	default:
		return false
	}
	t, self, ok := c.nav.Beyond(c.at, side)
	if !ok {
		return false // the end of the row, or nobody online past it
	}
	yWire := int32(int64(c.remoteY) * 65535 / int64(max(1, c.remoteH)))

	if self {
		slog.Info("cursor came back to this machine", "from", c.at, "side", group.Opposite(side))
		c.at = ""
		c.edgeSide = group.Opposite(side) // where it lands here
		if c.screen.Height > 0 {
			// SafeEntryPosition maps remoteY by remoteH; keep both in step.
			c.remoteY = int32(int64(yWire) * int64(c.remoteH) / 65535)
		}
		nav := c.nav
		c.returnLocalLocked(func() { nav.Moved("", group.Opposite(side), yWire) })
		return true
	}
	if t.Conn == nil {
		return false
	}
	slog.Info("cursor moving on", "from", c.at, "to", t.Name, "side", side)
	c.at, c.conn = t.Name, t.Conn
	c.remoteW, c.remoteH = t.Width, t.Height
	c.edgeSide = side
	pixelX, entryX := c.remoteEntryLocked()
	c.remoteX = pixelX
	c.remoteY = int32(int64(yWire) * int64(c.remoteH) / 65535)
	c.canReturn = false
	c.switchSent = time.Now()
	conn, send, nav := c.conn, c.sendFn, c.nav
	c.mu.Unlock()
	nav.Moved(t.Name, group.Opposite(side), yWire)
	go sendEntryBurst(conn, send, entryX, yWire)
	c.mu.Lock()
	return true
}

// Arrive: another machine announced the cursor is now here, entering from
// side at height y (0-65535).
func (c *Capturer) Arrive(side string, y int32) {
	c.mu.Lock()
	if c.stopped || c.active {
		c.mu.Unlock()
		return
	}
	c.at = ""
	if side != "" {
		c.edgeSide = side
	}
	c.remoteY = int32(int64(y) * int64(c.remoteH) / 65535)
	c.mu.Unlock()
	c.SetActive(true)
}

// Follow: another machine announced the cursor is now on t. This machine's
// keyboard and mouse go there until the cursor moves again.
func (c *Capturer) Follow(t group.Target, side string, y int32) {
	if t.Conn == nil {
		return
	}
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		return
	}
	c.active = false
	c.switchSent = time.Now()
	c.at, c.conn = t.Name, t.Conn
	c.remoteW, c.remoteH = t.Width, t.Height
	c.edgeSide = group.Opposite(side) // the direction it travelled
	if side == "" {
		c.remoteX = c.remoteW / 2 // taken back without crossing an edge
	} else {
		c.remoteX, _ = c.remoteEntryLocked()
	}
	c.remoteY = int32(int64(y) * int64(c.remoteH) / 65535)
	c.canReturn = false
	c.mu.Unlock()
	c.applyIsolation()
}

// Lost: the connection to the named machine ended. If this machine's input
// was going there, it stays here again.
func (c *Capturer) Lost(name string) {
	c.mu.Lock()
	here := c.active || c.at != name
	c.mu.Unlock()
	if here {
		return
	}
	slog.Info("the machine with the cursor went away; taking it back", "machine", name)
	c.mu.Lock()
	c.at = ""
	c.mu.Unlock()
	c.SetActive(true)
}

// RemoteAt reports which machine this machine's input goes to, "" when the
// cursor is here.
func (c *Capturer) RemoteAt() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active {
		return ""
	}
	return c.at
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}
