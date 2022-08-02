package utils

func PtrCopier[T any](item *T) *T {
	copy := *item
	return &copy
}
