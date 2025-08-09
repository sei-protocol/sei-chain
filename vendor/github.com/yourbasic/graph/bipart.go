package graph

// Bipartition returns a subset U of g's vertices with the property
// that every edge of g connects a vertex in U to one outside of U.
// If g isn't bipartite, it returns an empty slice and sets ok to false.
func Bipartition(g Iterator) (part []int, ok bool) {
	type color byte
	const (
		none color = iota
		white
		black
	)
	colors := make([]color, g.Order())
	whiteCount := 0
	for v := range colors {
		if colors[v] != none {
			continue
		}
		colors[v] = white
		whiteCount++
		for queue := []int{v}; len(queue) > 0; {
			v := queue[0]
			queue = queue[1:]
			if g.Visit(v, func(w int, _ int64) (skip bool) {
				switch {
				case colors[w] != none:
					if colors[v] == colors[w] {
						skip = true
					}
					return
				case colors[v] == white:
					colors[w] = black
				default:
					colors[w] = white
					whiteCount++
				}
				queue = append(queue, w)
				return
			}) {
				return []int{}, false
			}
		}
	}
	part = make([]int, 0, whiteCount)
	for v, color := range colors {
		if color == white {
			part = append(part, v)
		}
	}
	return part, true
}
