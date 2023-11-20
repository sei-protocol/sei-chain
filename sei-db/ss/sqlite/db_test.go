package sqlite

import (
	"testing"

	"github.com/stretchr/testify/suite"

	sstest "github.com/sei-protocol/sei-db/ss/test"
	"github.com/sei-protocol/sei-db/ss/types"
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
