package light_test

import (
	"errors"
	"github.com/sei-protocol/sei-chain/sei-tendermint/light/provider"
	"testing"
	"time"

	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/sei-tendermint/abci/example/kvstore"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	"github.com/sei-protocol/sei-chain/sei-tendermint/light"
	httpp "github.com/sei-protocol/sei-chain/sei-tendermint/light/provider/http"
	dbs "github.com/sei-protocol/sei-chain/sei-tendermint/light/store/db"
	rpctest "github.com/sei-protocol/sei-chain/sei-tendermint/rpc/test"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// Manually getting light blocks and verifying them.
func TestExampleClient(t *testing.T) {
	ctx := t.Context()
	conf, err := rpctest.CreateConfig(t, "ExampleClient_VerifyLightBlockAtHeight")
	if err != nil {
		t.Fatal(err)
	}

	logger, err := log.NewDefaultLogger(log.LogFormatPlain, log.LogLevelInfo)
	if err != nil {
		t.Fatal(err)
	}

	// Start a test application
	app := kvstore.NewApplication()

	_, closer, err := rpctest.StartTendermint(ctx, conf, app, rpctest.SuppressStdout)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = closer(ctx) }()

	dbDir := t.TempDir()
	chainID := conf.ChainID()

	primary, err := httpp.New(chainID, conf.RPC.ListenAddress)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("wait for the block")
	var block *types.LightBlock
	for {
		block, err = primary.LightBlock(ctx, 2)
		if err == nil {
			break
		}
		if !errors.Is(err, provider.ErrHeightTooHigh) {
			t.Fatal(err)
		}
		time.Sleep(time.Second)
	}

	db, err := dbm.NewGoLevelDB("light-client-db", dbDir)
	if err != nil {
		t.Fatal(err)
	}

	c, err := light.NewClient(ctx,
		chainID,
		light.TrustOptions{
			Period: 504 * time.Hour, // 21 days
			Height: 2,
			Hash:   block.Hash(),
		},
		primary,
		[]provider.Provider{primary},
		dbs.New(db),
		5*time.Minute,
		light.Logger(logger),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := c.Cleanup(); err != nil {
			t.Fatal(err)
		}
	}()

	// wait for a few more blocks to be produced
	time.Sleep(2 * time.Second)

	// veify the block at height 3
	_, err = c.VerifyLightBlockAtHeight(ctx, 3, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	// retrieve light block at height 3
	_, err = c.TrustedLightBlock(3)
	if err != nil {
		t.Fatal(err)
	}

	// update to the latest height
	lb, err := c.Update(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	logger.Info("verified light block", "light-block", lb)
}
