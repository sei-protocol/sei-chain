//go:build sqliteBackend
// +build sqliteBackend

package sqlite

import (
	"testing"

	sstest "github.com/sei-protocol/sei-db/ss/test"
	"github.com/sei-protocol/sei-db/ss/types"
	"github.com/stretchr/testify/suite"
)

func TestStorageTestSuite(t *testing.T) {
	s := &sstest.StorageTestSuite{
		NewDB: func(dir string) (types.StateStore, error) {
			return New(dir)
		},
		EmptyBatchSize: 0,
	}

	suite.Run(t, s)
}
