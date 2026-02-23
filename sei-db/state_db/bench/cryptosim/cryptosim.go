package cryptosim

import (
	"context"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/bench/wrappers"
)

const (
	// Used to store the seed used for the current run.
	seedKey = string(evm.EVMKeyNonce) + "seed"
	// Used to store the next account ID to be used when creating a new account.
	nonceKey = string(evm.EVMKeyNonce) + "nonce"
)

// The test runner for the cryptosim benchmark.
type CryptoSim struct {
	ctx context.Context

	// The configuration for the benchmark.
	config *CryptoSimConfig

	// The database implementation to use for the benchmark.
	db wrappers.DBWrapper

	// The source of randomness for the benchmark.
	rand *RandomBuffer
}

// Creates a new cryptosim benchmark runner.
func NewCryptoSim(
	ctx context.Context,
	config *CryptoSimConfig,
) (*CryptoSim, error) {

	dataDir, err := resolveAndCreateDataDir(config.DataDir)
	if err != nil {
		return nil, err
	}

	db, err := wrappers.NewDBImpl(config.Backend, dataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create database: %w", err)
	}

	c := &CryptoSim{
		ctx:    ctx,
		config: config,
		db:     db,
	}

	c.setup()
	c.start()
	return c, nil
}

func (c *CryptoSim) setup() {
	// TODO
}

func (c *CryptoSim) start() {
	// TODO
}
