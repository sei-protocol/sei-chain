package types_test

import (
	fmt "fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/suite"
)

type contextCacheTestSuite struct {
	suite.Suite
	contextCache sdk.ContextMemCache
}

func TestContextCacheTestSuite(t *testing.T) {
	suite.Run(t, new(contextCacheTestSuite))
}

func (s *contextCacheTestSuite) SetupSuite() {
	s.T().Parallel()
	s.contextCache = *sdk.NewContextMemCache()
}

func (s *contextCacheTestSuite) TestDeferredSendUpserts() {
	s.contextCache.UpsertDeferredSends("module1", sdk.NewCoins(
		sdk.NewInt64Coin("denom1", 100),
		sdk.NewInt64Coin("denom2", 20),
	))

	s.contextCache.UpsertDeferredSends("module1", sdk.NewCoins(
		sdk.NewInt64Coin("denom3", 50),
		sdk.NewInt64Coin("denom2", 10),
	))

	s.contextCache.UpsertDeferredSends("module2", sdk.NewCoins(
		sdk.NewInt64Coin("denom3", 50),
		sdk.NewInt64Coin("denom4", 40),
	))

	expectedDeferredBalances := map[string]sdk.Coins{
		"module1": sdk.NewCoins(
			sdk.NewInt64Coin("denom1", 100),
			sdk.NewInt64Coin("denom2", 30),
			sdk.NewInt64Coin("denom3", 50),
		),
		"module2": sdk.NewCoins(
			sdk.NewInt64Coin("denom3", 50),
			sdk.NewInt64Coin("denom4", 40),
		),
	}
	entries := 0
	s.contextCache.RangeOnDeferredSendsAndDelete(func(recipient string, amount sdk.Coins) {
		s.Require().Equal(expectedDeferredBalances[recipient], amount, fmt.Sprint("unexpected deferred balances", recipient, amount))
		entries++
	})
	s.Require().Equal(len(expectedDeferredBalances), entries)
	entries = 0
	// assert empty after range and delete
	s.contextCache.RangeOnDeferredSendsAndDelete(func(recipient string, amount sdk.Coins) {
		entries++
	})
	s.Require().Zero(entries)
}

func (s *contextCacheTestSuite) TestDeferredSendSafeSub() {
	// set up some balances
	s.contextCache.UpsertDeferredSends("module1", sdk.NewCoins(
		sdk.NewInt64Coin("denom1", 100),
		sdk.NewInt64Coin("denom2", 20),
	))
	s.contextCache.UpsertDeferredSends("module1", sdk.NewCoins(
		sdk.NewInt64Coin("denom3", 50),
		sdk.NewInt64Coin("denom2", 10),
	))
	s.contextCache.UpsertDeferredSends("module2", sdk.NewCoins(
		sdk.NewInt64Coin("denom3", 50),
		sdk.NewInt64Coin("denom4", 40),
	))
	s.contextCache.UpsertDeferredSends("module3", sdk.NewCoins(
		sdk.NewInt64Coin("denom1", 10),
	))

	// valid safesub - should succeed
	subtracted := s.contextCache.SafeSubDeferredSends("module1", sdk.NewCoins(
		sdk.NewInt64Coin("denom3", 25),
	))
	s.Require().True(subtracted)

	// safesub with nonexisting denom and valid one - should fail
	subtracted = s.contextCache.SafeSubDeferredSends("module1", sdk.NewCoins(
		sdk.NewInt64Coin("denom1", 20),
		sdk.NewInt64Coin("denom4", 20),
	))
	s.Require().False(subtracted)

	// safesub with other module multiple denoms - should succeed
	subtracted = s.contextCache.SafeSubDeferredSends("module2", sdk.NewCoins(
		sdk.NewInt64Coin("denom3", 20),
		sdk.NewInt64Coin("denom4", 20),
	))
	s.Require().True(subtracted)

	// safesub with nonexisting denom and valid one - should fail
	subtracted = s.contextCache.SafeSubDeferredSends("module4", sdk.NewCoins(
		sdk.NewInt64Coin("denom1", 20),
		sdk.NewInt64Coin("denom4", 20),
	))
	s.Require().False(subtracted)

	// safesub full balance for a module - should succeed
	subtracted = s.contextCache.SafeSubDeferredSends("module3", sdk.NewCoins(
		sdk.NewInt64Coin("denom1", 10),
	))
	s.Require().True(subtracted)

	expectedDeferredBalances := map[string]sdk.Coins{
		"module1": sdk.NewCoins(
			sdk.NewInt64Coin("denom1", 100),
			sdk.NewInt64Coin("denom2", 30),
			sdk.NewInt64Coin("denom3", 25),
		),
		"module2": sdk.NewCoins(
			sdk.NewInt64Coin("denom3", 30),
			sdk.NewInt64Coin("denom4", 20),
		),
		// is empty because was fully subbed
		"module3": sdk.Coins(nil),
	}

	entries := 0
	s.contextCache.RangeOnDeferredSendsAndDelete(func(recipient string, amount sdk.Coins) {
		s.Require().Equal(expectedDeferredBalances[recipient], amount, fmt.Sprint("unexpected deferred balances", recipient, amount))
		entries++
	})
	s.Require().Equal(len(expectedDeferredBalances), entries)
}

func (s *contextCacheTestSuite) TestDeferredSendSaturatingSub() {
	// set up some balances
	s.contextCache.UpsertDeferredSends("module1", sdk.NewCoins(
		sdk.NewInt64Coin("denom1", 100),
		sdk.NewInt64Coin("denom2", 20),
	))
	s.contextCache.UpsertDeferredSends("module1", sdk.NewCoins(
		sdk.NewInt64Coin("denom3", 50),
		sdk.NewInt64Coin("denom2", 10),
	))
	s.contextCache.UpsertDeferredSends("module2", sdk.NewCoins(
		sdk.NewInt64Coin("denom3", 50),
		sdk.NewInt64Coin("denom4", 40),
	))
	s.contextCache.UpsertDeferredSends("module3", sdk.NewCoins(
		sdk.NewInt64Coin("denom1", 10),
	))

	// valid saturating sub - should succeed
	err := s.contextCache.AtomicSpilloverSubDeferredSends(
		"module1",
		sdk.NewCoins(
			sdk.NewInt64Coin("denom3", 25),
		),
		func(amount sdk.Coins) error {
			// we shouldnt even touch this
			panic("Shouldn't be called")
		},
	)
	s.Require().NoError(err)

	// saturating sub with nonexisting denom and valid one - should fail
	called := false
	err = s.contextCache.AtomicSpilloverSubDeferredSends(
		"module1",
		sdk.NewCoins(
			sdk.NewInt64Coin("denom1", 20),
			sdk.NewInt64Coin("denom4", 20),
		),
		func(amount sdk.Coins) error {
			// should be called with 20denom4
			s.Require().Equal(sdk.NewCoins(sdk.NewInt64Coin("denom4", 20)), amount)
			called = true
			// simulate error
			return fmt.Errorf("insufficient balance")
		},
	)
	s.Require().True(called)
	s.Require().Error(err)

	// different saturaing sub but this time the balance *is* present in underlying store
	called = false
	err = s.contextCache.AtomicSpilloverSubDeferredSends(
		"module1",
		sdk.NewCoins(
			sdk.NewInt64Coin("denom1", 5),
			sdk.NewInt64Coin("denom5", 20),
		),
		func(amount sdk.Coins) error {
			s.Require().Equal(sdk.NewCoins(sdk.NewInt64Coin("denom5", 20)), amount)
			// simulate success querying underlying store
			called = true
			return nil
		},
	)
	s.Require().True(called)
	s.Require().NoError(err)

	// test failed subtraction from underlying store
	called = false
	err = s.contextCache.AtomicSpilloverSubDeferredSends(
		"module2",
		sdk.NewCoins(
			sdk.NewInt64Coin("denom3", 50),
			sdk.NewInt64Coin("denom4", 55),
		),
		func(amount sdk.Coins) error {
			s.Require().Equal(sdk.NewCoins(sdk.NewInt64Coin("denom4", 15)), amount)
			// simulate success querying underlying store
			called = true
			return fmt.Errorf("failed underlying subtract")
		},
	)
	s.Require().True(called)
	s.Require().Error(err)

	// saturating sub that uses all the balance of a denom and spills over into underlying store
	called = false
	err = s.contextCache.AtomicSpilloverSubDeferredSends(
		"module3",
		sdk.NewCoins(
			sdk.NewInt64Coin("denom1", 75),
		),
		func(amount sdk.Coins) error {
			s.Require().Equal(sdk.NewCoins(sdk.NewInt64Coin("denom1", 65)), amount)
			// simulate success querying underlying store
			called = true
			return nil
		},
	)
	s.Require().True(called)
	s.Require().NoError(err)

	// saturating sub for a module that doesnt exist in the map
	called = false
	err = s.contextCache.AtomicSpilloverSubDeferredSends(
		"module4",
		sdk.NewCoins(
			sdk.NewInt64Coin("denom2", 30),
		),
		func(amount sdk.Coins) error {
			s.Require().Equal(sdk.NewCoins(sdk.NewInt64Coin("denom2", 30)), amount)
			// simulate success querying underlying store
			called = true
			return nil
		},
	)
	s.Require().True(called)
	s.Require().NoError(err)

	expectedDeferredBalances := map[string]sdk.Coins{
		"module1": sdk.NewCoins(
			sdk.NewInt64Coin("denom1", 95),
			sdk.NewInt64Coin("denom2", 30),
			sdk.NewInt64Coin("denom3", 25),
		),
		"module2": sdk.NewCoins(
			sdk.NewInt64Coin("denom3", 50),
			sdk.NewInt64Coin("denom4", 40),
		),
		// is empty because was fully subbed
		"module3": sdk.Coins(nil),
	}

	entries := 0
	s.contextCache.RangeOnDeferredSendsAndDelete(func(recipient string, amount sdk.Coins) {
		s.Require().Equal(expectedDeferredBalances[recipient], amount, fmt.Sprint("unexpected deferred balances", recipient, amount))
		entries++
	})
	s.Require().Equal(len(expectedDeferredBalances), entries)
}
