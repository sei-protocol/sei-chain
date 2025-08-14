package graph

// Connected tells if g has exactly one (weakly) connected component.
func Connected(g Iterator) bool {
	_, count := components(g)
	return count == 1
}

// Components produces a partition of g's vertices into its
// (weakly) connected components.
func Components(g Iterator) [][]int {
	sets, count := components(g)

	m := make([][]int, g.Order())
	for v := range m {
		x := sets.find(v)
		m[x] = append(m[x], v)
	}

	components := make([][]int, 0, count)
	for _, comp := range m {
		if comp != nil {
			components = append(components, comp)
		}
	}
	return components
}

func components(g Iterator) (sets disjointSets, count int) {
	n := g.Order()
	sets, count = makeSingletons(n), n
	for v := 0; v < n && count > 1; v++ {
		g.Visit(v, func(w int, _ int64) (skip bool) {
			x, y := sets.find(v), sets.find(w)
			if x != y {
				sets.union(x, y)
				count--
				if count == 1 {
					skip = true
				}
			}
			return
		})
	}
	return
}

// Union-find with path compression performs any sequence of m ≥ n find
// and n – 1 union operations in O(m log n) time. Union by rank doesn't
// seem to improve performance here.
type disjointSets []int

func makeSingletons(n int) disjointSets {
	p := make(disjointSets, n)
	for v := range p {
		p[v] = v
	}
	return p
}

func (p disjointSets) find(x int) int {
	root := x
	for root != p[root] {
		root = p[root]
	}
	for p[x] != root {
		p[x], x = root, p[x]
	}
	return root
}

func (p disjointSets) union(x, y int) {
	x, y = p.find(x), p.find(y)
	if x != y {
		p[y] = x
	}
}
