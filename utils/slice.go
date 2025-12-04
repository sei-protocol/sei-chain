package utils

func FilterUInt64Slice(slice []uint64, item uint64) []uint64 {
	res := []uint64{}
	for _, i := range slice {
		if i != item {
			res = append(res, i)
		}
	}
	return res
}

func Map[I any, O any](input []I, lambda func(i I) O) []O {
	res := []O{}
	for _, i := range input {
		res = append(res, lambda(i))
	}
	return res
}

func SliceCopy[T any](slice []T) []T {
	return append([]T{}, slice...)
}

func Reduce[I, O any](input []I, reducer func(I, O) O, initial O) O {
	for _, i := range input {
		initial = reducer(i, initial)
	}
	return initial
}

func Filter[T any](slice []T, lambda func(t T) bool) []T {
	res := []T{}
	for _, t := range slice {
		if lambda(t) {
			res = append(res, t)
		}
	}
	return res
}

func Copy[T any](slice []T) []T {
	cpy := make([]T, len(slice))
	copy(cpy, slice)
	return cpy
}
