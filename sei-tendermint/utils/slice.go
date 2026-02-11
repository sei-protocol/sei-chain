package utils

func Map[I any, O any](input []I, lambda func(i I) O) []O {
	res := make([]O, 0, len(input))
	for _, i := range input {
		res = append(res, lambda(i))
	}
	return res
}
