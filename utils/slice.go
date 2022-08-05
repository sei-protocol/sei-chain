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
