package types_test

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

type storeTestSuite struct {
	suite.Suite
}

func TestStoreTestSuite(t *testing.T) {
	suite.Run(t, new(storeTestSuite))
}

func (s *storeTestSuite) SetupSuite() {
	s.T().Parallel()
}

func (s *storeTestSuite) TestPrefixEndBytes() {
	var testCases = []struct {
		prefix   []byte
		expected []byte
	}{
		{[]byte{byte(55), byte(255), byte(255), byte(0)}, []byte{byte(55), byte(255), byte(255), byte(1)}},
		{[]byte{byte(55), byte(255), byte(255), byte(15)}, []byte{byte(55), byte(255), byte(255), byte(16)}},
		{[]byte{byte(55), byte(200), byte(255)}, []byte{byte(55), byte(201)}},
		{[]byte{byte(55), byte(255), byte(255)}, []byte{byte(56)}},
		{[]byte{byte(255), byte(255), byte(255)}, nil},
		{[]byte{byte(255)}, nil},
		{nil, nil},
	}

	for _, test := range testCases {
		end := sdk.PrefixEndBytes(test.prefix)
		s.Require().Equal(test.expected, end)
	}
}

func (s *storeTestSuite) TestCommitID() {
	var empty sdk.CommitID
	s.Require().True(empty.IsZero())

	var nonempty = sdk.CommitID{
		Version: 1,
		Hash:    []byte("testhash"),
	}
	s.Require().False(nonempty.IsZero())
}

func (s *storeTestSuite) TestNewTransientStoreKeys() {
	s.Require().Equal(map[string]*sdk.TransientStoreKey{}, sdk.NewTransientStoreKeys())
	s.Require().Equal(1, len(sdk.NewTransientStoreKeys("one")))
}

func (s *storeTestSuite) TestNewInfiniteGasMeter() {
	gm := sdk.NewInfiniteGasMeter(1, 1)
	s.Require().NotNil(gm)
}

func (s *storeTestSuite) TestNewGasMeterWithMultiplier() {
	ctx := sdk.Context{}
	n, d := sdk.NewGasMeterWithMultiplier(ctx, 10).Multiplier()
	s.Require().Equal(uint64(1), n)
	s.Require().Equal(uint64(1), d)
	ctx = ctx.WithGasMeter(sdk.NewGasMeter(10, 3, 4))
	n, d = sdk.NewGasMeterWithMultiplier(ctx, 10).Multiplier()
	s.Require().Equal(uint64(3), n)
	s.Require().Equal(uint64(4), d)
}

func (s *storeTestSuite) TestNewInfiniteGasMeterWithMultiplier() {
	ctx := sdk.Context{}
	n, d := sdk.NewInfiniteGasMeterWithMultiplier(ctx).Multiplier()
	s.Require().Equal(uint64(1), n)
	s.Require().Equal(uint64(1), d)
	ctx = ctx.WithGasMeter(sdk.NewGasMeter(10, 3, 4))
	n, d = sdk.NewInfiniteGasMeterWithMultiplier(ctx).Multiplier()
	s.Require().Equal(uint64(3), n)
	s.Require().Equal(uint64(4), d)
}

func (s *storeTestSuite) TestStoreTypes() {
	s.Require().Equal(sdk.InclusiveEndBytes([]byte("endbytes")), types.InclusiveEndBytes([]byte("endbytes")))
}
