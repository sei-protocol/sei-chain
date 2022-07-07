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
