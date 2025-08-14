package graph

import (
	"strconv"
)

const initialMapSize = 4

// Mutable represents a directed graph with a fixed number
// of vertices and weighted edges that can be added or removed.
// The implementation uses hash maps to associate each vertex in the graph with
// its adjacent vertices. This gives constant time performance for
// all basic operations.
//
type Mutable struct {
	// The map edges[v] contains the mapping {w:c} if there is an edge
	// from v to w, and c is the cost assigned to this edge.
	// The maps may be nil and are allocated as needed.
	edges []map[int]int64
}

// New constructs a new graph with n vertices, numbered from 0 to n-1, and no edges.
func New(n int) *Mutable {
	return &Mutable{edges: make([]map[int]int64, n)}
}

// Copy returns a copy of g.
// If g is a multigraph, any duplicate edges in g will be lost.
func Copy(g Iterator) *Mutable {
	switch g := g.(type) {
	case *Mutable:
		return copyMutable(g)
	case *Immutable:
		return copyImmutable(g)
	}
	n := g.Order()
	h := New(n)
	for v := 0; v < n; v++ {
		g.Visit(v, func(w int, c int64) (skip bool) {
			h.AddCost(v, w, c)
			return
		})
	}
	return h
}

func copyMutable(g *Mutable) *Mutable {
	h := New(g.Order())
	for v, neighbors := range g.edges {
		if deg := len(neighbors); deg > 0 {
			h.edges[v] = make(map[int]int64, deg)
			for w, c := range neighbors {
				h.edges[v][w] = c
			}
		}
	}
	return h
}

func copyImmutable(g *Immutable) *Mutable {
	h := New(g.Order())
	for v, neighbors := range g.edges {
		if deg := len(neighbors); deg > 0 {
			h.edges[v] = make(map[int]int64, deg)
			for _, edge := range neighbors {
				h.edges[v][edge.vertex] = edge.cost
			}
		}
	}
	return h
}

// String returns a string representation of the graph.
func (g *Mutable) String() string {
	return String(g)
}

// Order returns the number of vertices in the graph.
func (g *Mutable) Order() int {
	return len(g.edges)
}

// Visit calls the do function for each neighbor w of v,
// with c equal to the cost of the edge from v to w.
// If do returns true, Visit returns immediately,
// skipping any remaining neighbors, and returns true.
//
// The iteration order is not specified and is not guaranteed
// to be the same every time.
// It is safe to delete, but not to add, edges adjacent to v
// during a call to this method.
func (g *Mutable) Visit(v int, do func(w int, c int64) bool) bool {
	for w, c := range g.edges[v] {
		if do(w, c) {
			return true
		}
	}
	return false
}

// Degree returns the number of outward directed edges from v.
func (g *Mutable) Degree(v int) int {
	return len(g.edges[v])
}

// Edge tells if there is an edge from v to w.
func (g *Mutable) Edge(v, w int) bool {
	if v < 0 || v >= g.Order() {
		return false
	}
	_, ok := g.edges[v][w]
	return ok
}

// Cost returns the cost of an edge from v to w, or 0 if no such edge exists.
func (g *Mutable) Cost(v, w int) int64 {
	if v < 0 || v >= g.Order() {
		return 0
	}
	return g.edges[v][w]
}

// Add inserts a directed edge from v to w with zero cost.
// It removes the previous cost if this edge already exists.
func (g *Mutable) Add(v, w int) {
	g.AddCost(v, w, 0)
}

// AddCost inserts a directed edge from v to w with cost c.
// It overwrites the previous cost if this edge already exists.
func (g *Mutable) AddCost(v, w int, c int64) {
	// Make sure not to break internal state.
	if w < 0 || w >= len(g.edges) {
		panic("vertex out of range: " + strconv.Itoa(w))
	}
	if g.edges[v] == nil {
		g.edges[v] = make(map[int]int64, initialMapSize)
	}
	g.edges[v][w] = c
}

// AddBoth inserts edges with zero cost between v and w.
// It removes the previous costs if these edges already exist.
func (g *Mutable) AddBoth(v, w int) {
	g.AddCost(v, w, 0)
	if v != w {
		g.AddCost(w, v, 0)
	}
}

// AddBothCost inserts edges with cost c between v and w.
// It overwrites the previous costs if these edges already exist.
func (g *Mutable) AddBothCost(v, w int, c int64) {
	g.AddCost(v, w, c)
	if v != w {
		g.AddCost(w, v, c)
	}
}

// Delete removes an edge from v to w.
func (g *Mutable) Delete(v, w int) {
	delete(g.edges[v], w)
}

// DeleteBoth removes all edges between v and w.
func (g *Mutable) DeleteBoth(v, w int) {
	g.Delete(v, w)
	if v != w {
		g.Delete(w, v)
	}
}
