package graph

// TopSort returns a topological ordering of the vertices in
// a directed acyclic graph; if the graph is not acyclic,
// no such ordering exists and ok is set to false.
//
// In a topological order v comes before w for every directed edge from v to w.
func TopSort(g Iterator) (order []int, ok bool) {
	order, ok = topsort(g, true)
	return
}

// Acyclic tells if g has no cycles.
func Acyclic(g Iterator) bool {
	_, acyclic := topsort(g, false)
	return acyclic
}

// Kahn's algorithm
func topsort(g Iterator, output bool) (order []int, acyclic bool) {
	indegree := make([]int, g.Order())
	for v := range indegree {
		g.Visit(v, func(w int, _ int64) (skip bool) {
			indegree[w]++
			return
		})
	}

	// Invariant: this queue holds all vertices with indegree 0.
	var queue []int
	for v, degree := range indegree {
		if degree == 0 {
			queue = append(queue, v)
		}
	}

	order = []int{}
	vertexCount := 0
	for len(queue) > 0 {
		v := queue[0]
		queue = queue[1:]
		if output {
			order = append(order, v)
		}
		vertexCount++
		g.Visit(v, func(w int, _ int64) (skip bool) {
			indegree[w]--
			if indegree[w] == 0 {
				queue = append(queue, w)
			}
			return
		})
	}
	if vertexCount != g.Order() {
		return
	}
	acyclic = true
	return
}
