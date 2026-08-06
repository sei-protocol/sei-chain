//go:build rocksdbBackend
// +build rocksdbBackend

package mvcc

import "fmt"

func (db *Database) CheckRollbackCoverage(targetVersion int64) error {
	return fmt.Errorf("rocksdb state store rollback to version %d is not supported", targetVersion)
}

func (db *Database) Rollback(targetVersion int64) error {
	return fmt.Errorf("rocksdb state store rollback to version %d is not supported", targetVersion)
}
