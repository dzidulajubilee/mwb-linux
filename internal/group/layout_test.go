package group

import (
	"slices"
	"testing"
)

func TestLayoutNewer(t *testing.T) {
	old := Layout{Order: []string{"a", "b"}, Changed: 100, By: "a"}
	newer := Layout{Order: []string{"b", "a"}, Changed: 200, By: "b"}
	if !newer.Newer(old) || old.Newer(newer) {
		t.Fatal("the later choice must win")
	}
	if !old.Newer(Layout{Order: []string{"x"}}) {
		t.Fatal("any known choice beats an unknown one")
	}
	tieA := Layout{Changed: 5, By: "a"}
	tieB := Layout{Changed: 5, By: "b"}
	if tieA.Newer(tieB) == tieB.Newer(tieA) {
		t.Fatal("a tie must be settled one way")
	}
	if tieA.Newer(tieA) {
		t.Fatal("a choice is not newer than itself")
	}
}

func TestCompleteAddsMissingMachinesTheSameWayEverywhere(t *testing.T) {
	// Two machines that each know a different subset end up with one order.
	got1, _ := Complete([]string{"a", "b"}, []string{"b", "d", "c"}, "a", "")
	got2, _ := Complete([]string{"a", "b"}, []string{"a", "c", "d"}, "b", "")
	want := []string{"a", "b", "c", "d"}
	if !slices.Equal(got1, want) || !slices.Equal(got2, want) {
		t.Fatalf("got %v and %v, want %v", got1, got2, want)
	}
	if _, added := Complete(want, []string{"c"}, "a", ""); added {
		t.Fatal("a complete order reported a change")
	}
	if got, added := Complete([]string{"a", "", "a", "b"}, nil, "a", ""); !added || !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("duplicates not removed: %v %v", got, added)
	}
}

func TestCompleteKeepsTheTwoMachineSide(t *testing.T) {
	// A pair set up before layouts: "edge = left" on laptop means the peer is
	// on its left, and the peer had "edge = right". Both reach one order.
	onLaptop, _ := Complete(nil, []string{"mystogan"}, "laptop", "left")
	onPeer, _ := Complete(nil, []string{"laptop"}, "mystogan", "right")
	want := []string{"mystogan", "laptop"}
	if !slices.Equal(onLaptop, want) || !slices.Equal(onPeer, want) {
		t.Fatalf("got %v and %v, want %v", onLaptop, onPeer, want)
	}
}

func TestBeyondSkipsOfflineMachines(t *testing.T) {
	order := []string{"a", "b", "c", "d"}
	up := map[string]bool{"a": true, "b": false, "c": true, "d": true}
	online := func(n string) bool { return up[n] }
	for _, tc := range []struct {
		from, side, want string
		ok               bool
	}{
		{"a", "right", "c", true},
		{"c", "left", "a", true},
		{"d", "right", "", false},
		{"a", "left", "", false},
		{"x", "right", "", false},
		{"a", "up", "", false},
	} {
		got, ok := Beyond(order, online, tc.from, tc.side)
		if got != tc.want || ok != tc.ok {
			t.Errorf("Beyond(%s, %s) = %q %v, want %q %v", tc.from, tc.side, got, ok, tc.want, tc.ok)
		}
	}
	if l, r := Neighbors(order, "a"); l || !r {
		t.Fatal("the leftmost machine has only a right neighbour")
	}
	if l, r := Neighbors(order, "b"); !l || !r {
		t.Fatal("a middle machine has both")
	}
}
