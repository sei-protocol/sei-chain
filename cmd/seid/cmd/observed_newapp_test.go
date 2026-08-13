package cmd

import (
	"os"
	"testing"

	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/flags"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/testutil/configtest"
)

// censusOpts is what newApp is given while its reads are recorded.
//
// Some values are supplied deliberately rather than left absent, because a read behind a condition is
// only observable when the condition holds. Pruning is the case that matters: the reader looks the
// three custom-pruning keys up only when the strategy is "custom", so any other strategy hides them
// and an absent one fails the construction outright. Those three then have to be a combination the
// reader accepts, since it validates them before returning, so the census cannot observe them without
// also being a configuration that works.
type censusOpts struct {
	app.TestAppOpts
	home string
}

func (o censusOpts) Get(key string) any {
	switch key {
	case flags.FlagHome:
		return o.home
	case server.FlagPruning:
		return "custom"
	case server.FlagPruningKeepRecent:
		return 100
	case server.FlagPruningKeepEvery:
		return 0
	case server.FlagPruningInterval:
		return 10
	}
	return o.TestAppOpts.Get(key)
}

// recordAppCreatorReads records every key newApp resolves, including those it resolves before app.New.
//
// A separate recording from the one the app package keeps. That one wraps app.New, and newApp resolves
// keys of its own before calling it: the pruning strategy, the halt heights, the inter-block cache, the
// OCC switch. Those reach the same configuration source, so the registry can answer them, and a census
// scoped to app.New reports them as read by nobody.
func recordAppCreatorReads(t *testing.T) *configtest.RecordingAppOpts {
	t.Helper()

	homeDir, err := os.MkdirTemp("", "sei-newapp-observed")
	if err != nil {
		t.Fatalf("temp home: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(homeDir) })

	recorder := configtest.Record(censusOpts{home: homeDir})
	created := newApp(dbm.NewMemDB(), nil, tmcfg.DefaultConfig(), recorder)
	if created == nil {
		t.Fatal("the application was not created, so nothing was read and this test measures nothing")
	}
	t.Cleanup(func() { _ = created.Close() })
	return recorder
}

// TestTheKeysTheAppCreatorReadsAreObserved records the set, so adding or removing a reader is a diff.
func TestTheKeysTheAppCreatorReadsAreObserved(t *testing.T) {
	configtest.CheckObservedKeys(t, "newapp", recordAppCreatorReads(t).Keys())
}
