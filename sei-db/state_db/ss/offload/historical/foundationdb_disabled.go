//go:build !foundationdb

package historical

func NewFoundationDBReader(FoundationDBConfig) (Reader, error) {
	return nil, ErrFoundationDBUnavailable
}
