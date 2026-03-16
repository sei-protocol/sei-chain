package utils

func Map[I any, O any](input []I, lambda func(i I) O) []O {
	if input == nil {
		return nil
	}
	res := []O{}
	for _, i := range input {
		res = append(res, lambda(i))
	}
	return res
}

func StringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}
