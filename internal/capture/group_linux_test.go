//go:build linux

package capture

import (
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/lucky-verma/mwb-linux/internal/group"
	"github.com/lucky-verma/mwb-linux/internal/network"
	"github.com/lucky-verma/mwb-linux/internal/protocol"
)

// fakeRow is a Navigator over a fixed row where every machine is online.
type fakeRow struct {
	me    string
	order []string
	conns map[string]*network.Conn

	mu    sync.Mutex
	moved []string
}

func newFakeRow(me string, order ...string) *fakeRow {
	r := &fakeRow{me: me, order: order, conns: map[string]*network.Conn{}}
	for _, n := range order {
		if n != me {
			r.conns[n] = &network.Conn{RemoteName: n}
		}
	}
	return r
}

func (r *fakeRow) Beyond(from, side string) (group.Target, bool, bool) {
	if from == "" {
		from = r.me
	}
	name, ok := group.Beyond(r.order, func(string) bool { return true }, from, side)
	if !ok {
		return group.Target{}, false, false
	}
	if name == r.me {
		return group.Target{Name: name}, true, true
	}
	return group.Target{Name: name, Conn: r.conns[name], Width: 1000, Height: 500}, false, true
}

func (r *fakeRow) Moved(to, side string, y int32) {
	if to == "" {
		to = r.me
	}
	r.mu.Lock()
	r.moved = append(r.moved, to+" "+side)
	r.mu.Unlock()
}

func (r *fakeRow) Neighbors() (bool, bool) { return group.Neighbors(r.order, r.me) }

func (r *fakeRow) lastMove() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.moved) == 0 {
		return ""
	}
	return r.moved[len(r.moved)-1]
}

func newGroupCapturer(t *testing.T, row *fakeRow) (*Capturer, func() int) {
	t.Helper()
	c, grabbed := newFakeIsolated(t, 1)
	c.screen = ScreenInfo{Width: 1920, Height: 1080}
	c.active, c.canSwitch = true, true
	c.accelMult = 1
	c.pointer = &recordingPointer{}
	c.sendFn = func(*protocol.Packet) error { return nil }
	c.SetNavigator(row)
	return c, grabbed
}

func waitMove(t *testing.T, row *fakeRow, want string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for row.lastMove() != want && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if got := row.lastMove(); got != want {
		t.Fatalf("last announcement %q, want %q", got, want)
	}
}

// moveBy pushes the virtual remote cursor, as mouse motion would.
func moveBy(c *Capturer, dx int32) {
	c.mu.Lock()
	c.switchSent = time.Time{} // past the switch grace period
	c.moveRemoteLocked(dx, 0)
	c.mu.Unlock()
}

func TestGroupCursorWalksTheRowAndComesBack(t *testing.T) {
	row := newFakeRow("me", "me", "b", "c")
	c, grabbed := newGroupCapturer(t, row)
	if c.watchLeft || !c.watchRight {
		t.Fatal("the leftmost screen watches only its right edge")
	}
	if side := c.polledEdge(0); side != "" {
		t.Fatalf("the left edge has no neighbour but reported %q", side)
	}

	c.crossLocalEdge("right", 1919, 540)
	waitMove(t, row, "b left")
	if c.IsActive() || c.RemoteAt() != "b" || grabbed() != 1 {
		t.Fatalf("not on b: active=%v at=%q grabbed=%d", c.IsActive(), c.RemoteAt(), grabbed())
	}

	// Walk off b's right edge onto c.
	moveBy(c, 200) // away from the entry edge first
	moveBy(c, 2000)
	waitMove(t, row, "c left")
	if c.RemoteAt() != "c" || c.conn != row.conns["c"] {
		t.Fatalf("not on c: at=%q", c.RemoteAt())
	}
	// c is the end of the row: its right edge is a wall.
	moveBy(c, 200)
	moveBy(c, 2000)
	if c.RemoteAt() != "c" {
		t.Fatal("walked past the end of the row")
	}
	// All the way back left: c, b, then here.
	moveBy(c, -5000)
	waitMove(t, row, "b right")
	moveBy(c, -200)
	moveBy(c, -5000)
	waitMove(t, row, "me right")
	if !c.IsActive() || grabbed() != 0 {
		t.Fatalf("did not come back: active=%v grabbed=%d", c.IsActive(), grabbed())
	}
	if p := c.pointer.(*recordingPointer); !p.moved || p.x < 1800 {
		t.Fatalf("landed at x=%d, want near the right edge it came in from", p.x)
	}
}

func TestGroupFollowArriveAndLost(t *testing.T) {
	row := newFakeRow("me", "a", "me", "c")
	c, grabbed := newGroupCapturer(t, row)
	if !c.watchLeft || !c.watchRight {
		t.Fatal("a middle screen watches both edges")
	}

	// Another machine moved the cursor onto c: this keyboard follows.
	c.Follow(group.Target{Name: "c", Conn: row.conns["c"], Width: 1000, Height: 500}, "left", 32767)
	if c.IsActive() || c.RemoteAt() != "c" || grabbed() != 1 {
		t.Fatalf("not following: active=%v at=%q", c.IsActive(), c.RemoteAt())
	}
	// A different machine went away: nothing changes.
	c.Lost("a")
	if c.IsActive() {
		t.Fatal("came back when an unrelated machine left")
	}
	// The machine with the cursor went away: take it back.
	c.Lost("c")
	if !c.IsActive() || grabbed() != 0 {
		t.Fatal("did not take the cursor back from a machine that left")
	}

	c.Follow(group.Target{Name: "a", Conn: row.conns["a"], Width: 1000, Height: 500}, "right", 0)
	c.Arrive("left", 100)
	if !c.IsActive() || c.RemoteAt() != "" {
		t.Fatal("Arrive did not bring the cursor here")
	}
	if !slices.Equal(row.moved, nil) {
		t.Fatalf("following must not announce anything, announced %v", row.moved)
	}
}
