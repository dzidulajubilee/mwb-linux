package group

import "sort"

// This file is the groundwork for arranging screens in two dimensions (a
// machine above or below another, not only left and right). It is a pure model
// with no callers yet: the running group still uses the one-row Layout in
// layout.go. A single row is simply a grid one row tall, so the row keeps
// working unchanged while the rest of the code is taught to use the grid.

// Pos is a screen's place in the arrangement: Col increases to the right, Row
// increases downward. The current one-row layout is every screen on Row 0.
// The short JSON tags keep it compact on the wire.
type Pos struct {
	Col int `json:"c"`
	Row int `json:"r"`
}

// step is how one cell offset maps to each side.
var step = map[string]Pos{
	"left":  {Col: -1, Row: 0},
	"right": {Col: 1, Row: 0},
	"up":    {Col: 0, Row: -1},
	"down":  {Col: 0, Row: 1},
}

// Sides lists the four directions in a stable order, for callers that iterate
// them (tests, edge watching) and need the same result every time.
var Sides = []string{"left", "right", "up", "down"}

// RowGrid places names left to right on a single row, so the layout the group
// uses today becomes a 1×N grid with no change in meaning. Empty names are
// skipped; a name repeated keeps its first cell.
func RowGrid(order []string) map[string]Pos {
	g := make(map[string]Pos, len(order))
	col := 0
	for _, n := range order {
		if n == "" {
			continue
		}
		if _, seen := g[n]; seen {
			continue
		}
		g[n] = Pos{Col: col, Row: 0}
		col++
	}
	return g
}

// Neighbor returns the machine in the cell immediately to side of name, and
// whether one is there. side is "left", "right", "up" or "down".
func Neighbor(grid map[string]Pos, name, side string) (string, bool) {
	d, ok := step[side]
	if !ok {
		return "", false
	}
	from, ok := grid[name]
	if !ok {
		return "", false
	}
	want := Pos{Col: from.Col + d.Col, Row: from.Row + d.Row}
	best, found := "", false
	for n, p := range grid {
		if p == want && (!found || n < best) {
			best, found = n, true
		}
	}
	return best, found
}

// Edges reports which sides of name's screen border another machine, online or
// not: where the screen has a shared edge the pointer could cross at all. This
// is the 2-D form of Neighbors.
func Edges(grid map[string]Pos, name string) map[string]bool {
	out := map[string]bool{}
	for _, side := range Sides {
		if _, ok := Neighbor(grid, name, side); ok {
			out[side] = true
		}
	}
	return out
}

// BeyondGrid returns the first machine past name towards side for which online
// reports true, skipping machines that are offline, as the one-row Beyond does
// along a row. It stops at the first empty cell: the pointer cannot cross a gap
// where no screen stands. online is called for machine names only.
func BeyondGrid(grid map[string]Pos, online func(string) bool, name, side string) (string, bool) {
	d, ok := step[side]
	if !ok {
		return "", false
	}
	from, ok := grid[name]
	if !ok {
		return "", false
	}
	// A reverse index from cell to the machine there, so each step is a lookup
	// rather than a scan. Ties (two names claiming a cell) take the smaller
	// name, matching Neighbor.
	at := map[Pos]string{}
	for n, p := range grid {
		if cur, seen := at[p]; !seen || n < cur {
			at[p] = n
		}
	}
	cell := Pos{Col: from.Col + d.Col, Row: from.Row + d.Row}
	for {
		n, there := at[cell]
		if !there {
			return "", false // a gap, or the edge of the arrangement
		}
		if online(n) {
			return n, true
		}
		cell = Pos{Col: cell.Col + d.Col, Row: cell.Row + d.Row}
	}
}

// GridOrderRows returns the machines grouped into rows, each row left to right,
// rows top to bottom. It lets a one-row consumer or a test read a grid back in
// a stable order.
func GridOrderRows(grid map[string]Pos) [][]string {
	rowsByY := map[int][]string{}
	ys := []int{}
	for n := range grid {
		y := grid[n].Row
		if _, seen := rowsByY[y]; !seen {
			ys = append(ys, y)
		}
		rowsByY[y] = append(rowsByY[y], n)
	}
	sort.Ints(ys)
	out := make([][]string, 0, len(ys))
	for _, y := range ys {
		row := rowsByY[y]
		sort.Slice(row, func(i, j int) bool { return grid[row[i]].Col < grid[row[j]].Col })
		out = append(out, row)
	}
	return out
}
