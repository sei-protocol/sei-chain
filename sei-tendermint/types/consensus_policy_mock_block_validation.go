//go:build mock_block_validation

package types

func SkipAppHashValidationForBuild() bool {
	return true
}
