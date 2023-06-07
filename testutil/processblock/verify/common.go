package verify

type BlockRunnable func() (resultCodes []uint32)

// inefficient so only for test
func removeMatched[T any](l []T, matcher func(T) bool) []T {
	newL := []T{}
	for _, i := range l {
		if !matcher(i) {
			newL = append(newL, i)
		}
	}
	return newL
}
