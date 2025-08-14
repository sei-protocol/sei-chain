package graph

// MST computes a minimum spanning tree for each connected component
// of an undirected weighted graph.
// The forest of spanning trees is returned as a slice of parent pointers:
// parent[v] is either the parent of v in a tree,
// or -1 if v is the root of a tree.
//
// The time complexity is O(|E|⋅log|V|), where |E| is the number of edges
// and |V| the number of vertices in the graph.
func MST(g Iterator) (parent []int) {
	n := g.Order()
	parent = make([]int, n)
	cost := make([]int64, n)
	for i := range parent {
		parent[i] = -1
		cost[i] = Max
	}

	// Prim's algorithm
	Q := newPrioQueue(cost)
	for Q.Len() > 0 {
		v := Q.Pop()
		g.Visit(v, func(w int, c int64) (skip bool) {
			if Q.Contains(w) && c < cost[w] {
				cost[w] = c
				Q.Fix(w)
				parent[w] = v
			}
			return
		})
	}
	return
}
