package graph

// StrongComponents produces a partition of g's vertices into its
// strongly connected components.
//
// A component is strongly connected if all its vertices are reachable
// from every other vertex in the component.
// Each vertex of the graph appears in exactly one of the strongly
// connected components, and any vertex that is not on a directed cycle
// forms a strongly connected component all by itself.
func StrongComponents(g Iterator) [][]int {
	n := g.Order()
	s := &scc{
		graph:   g,
		visited: make([]bool, n),
		lowLink: make([]int, n),
	}
	components := [][]int{}
	for v := range s.visited {
		if !s.visited[v] {
			components = s.append(components, v)
		}
	}
	return components
}

// Tarjan's algorithm
type scc struct {
	graph   Iterator
	visited []bool
	stack   []int
	lowLink []int
	time    int
}

// Make a depth-first search starting at v and append all strongly
// connected components of the visited subgraph to comps.
func (s *scc) append(components [][]int, v int) [][]int {
	// A vertex remains on this stack after it has been visited iff
	// there is a path from it to some vertex earlier on the stack.
	s.stack = append(s.stack, v)

	// lowLink[v] is the smallest vertex known to be reachable from v.
	s.lowLink[v] = s.time
	s.time++

	newComponent := true
	s.visited[v] = true
	s.graph.Visit(v, func(w int, _ int64) (skip bool) {
		if !s.visited[w] {
			components = s.append(components, w)
		}
		if s.lowLink[v] > s.lowLink[w] {
			s.lowLink[v] = s.lowLink[w]
			newComponent = false
		}
		return
	})
	if !newComponent {
		return components
	}
	var comp []int
	for {
		n := len(s.stack) - 1
		w := s.stack[n]
		s.stack = s.stack[:n]
		s.lowLink[w] = int(^uint(0) >> 1) // maxint
		comp = append(comp, w)
		if v == w {
			return append(components, comp)
		}
	}
}
