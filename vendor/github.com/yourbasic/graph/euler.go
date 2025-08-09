package graph

// EulerDirected returns an Euler walk in a directed graph.
// If no such walk exists, it returns an empty walk and sets ok to false.
func EulerDirected(g Iterator) (walk []int, ok bool) {
	n := g.Order()
	degree := make([]int, n) // outdegree - indegree for each vertex
	edgeCount := 0
	for v := range degree {
		g.Visit(v, func(w int, _ int64) (skip bool) {
			edgeCount++
			degree[v]++
			degree[w]--
			return
		})
	}
	if edgeCount == 0 {
		return []int{}, true
	}

	start, end := -1, -1
	for v := range degree {
		switch {
		case degree[v] == 0:
		case degree[v] == 1 && start == -1:
			start = v
		case degree[v] == -1 && end == -1:
			end = v
		default:
			return []int{}, false
		}
	}

	// Make a copy of g
	h := make([][]int, n)
	for v := range h {
		g.Visit(v, func(w int, _ int64) (skip bool) {
			h[v] = append(h[v], w)
			return
		})
	}

	// Find a starting point with neighbors.
	if start == -1 {
		for v, neighbors := range h {
			if len(neighbors) > 0 {
				start = v
				break
			}
		}
	}

	for stack := []int{start}; len(stack) > 0; {
		n := len(stack)
		v := stack[n-1]
		stack = stack[:n-1]
		for len(h[v]) > 0 {
			stack = append(stack, v)
			v, h[v] = h[v][0], h[v][1:]
			edgeCount--
		}
		walk = append(walk, v)
	}
	if edgeCount > 0 {
		return []int{}, false
	}
	for i, j := 0, len(walk)-1; i < j; i, j = i+1, j-1 {
		walk[i], walk[j] = walk[j], walk[i]
	}
	return walk, true
}

// EulerUndirected returns an Euler walk following undirected edges
// in only one direction. If no such walk exists, it returns an empty walk
// and sets ok to false.
func EulerUndirected(g Iterator) (walk []int, ok bool) {
	n := g.Order()
	out := make([]int, n) // outdegree for each vertex
	edgeCount := 0
	for v := range out {
		g.Visit(v, func(w int, _ int64) (skip bool) {
			edgeCount++
			if v != w {
				out[v]++
			}
			return
		})
	}
	if edgeCount == 0 {
		return []int{}, true
	}

	start, oddDeg := -1, 0
	for v := range out {
		if out[v]&1 == 1 {
			start = v
			oddDeg++
		}
	}
	if !(oddDeg == 0 || oddDeg == 2) {
		return []int{}, false
	}

	// Find a starting point with neighbors.
	if start == -1 {
		for v := 0; v < n; v++ {
			if g.Visit(v, func(w int, _ int64) (skip bool) {
				start = w
				return true
			}) {
				break
			}
		}
	}

	h := Copy(g)
	for stack := []int{start}; len(stack) > 0; {
		n := len(stack)
		v := stack[n-1]
		stack = stack[:n-1]
		for h.Degree(v) > 0 {
			stack = append(stack, v)
			var w int
			h.Visit(v, func(u int, _ int64) (skip bool) {
				w = u
				return true
			})
			h.DeleteBoth(v, w)
			edgeCount--
			if v != w {
				edgeCount--
			}
			v = w
		}
		walk = append(walk, v)
	}
	if edgeCount > 0 {
		return []int{}, false
	}
	return walk, true
}
