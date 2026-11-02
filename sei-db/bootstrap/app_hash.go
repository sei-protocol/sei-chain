package bootstrap

import (
	"context"
	"fmt"

	"github.com/sei-protocol/sei-chain/giga/apphash/bud"
	"github.com/sei-protocol/sei-chain/giga/apphash/builder"
	"github.com/sei-protocol/sei-chain/giga/apphash/receipthash"
	"github.com/sei-protocol/sei-chain/giga/apphash/vault"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/hashlog"
)

// openAppHashBuilder opens the hash vault, the app hash builder over it, and the receipt and BUD hashers reporting
// into it. It must run before the state DB opens, since the state DB submits each committed block to the BUD hasher.
func (m *GigaStorageManager) openAppHashBuilder(ctx context.Context) error {
	appHashConfig := m.cfg.AppHashBuilderConfig
	hashVault, err := vault.NewStandardHashVault(
		appHashConfig.HashVaultConfig, appHashConfig.ChainID, appHashConfig.GigaActivationHeight)
	if err != nil {
		return fmt.Errorf("open hash vault: %w", err)
	}
	m.appHashVault = hashVault
	appHashBuilder, err := builder.NewStandardAppHashBuilder(ctx, *m.cfg.AppHashBuilderConfig, hashVault)
	if err != nil {
		return fmt.Errorf("open app hash builder: %w", err)
	}
	m.appHashBuilder = appHashBuilder
	receiptHasher, err := receipthash.NewReceiptHasher(
		ctx, *m.cfg.ReceiptHasherConfig, appHashBuilder.ReportReceiptHash)
	if err != nil {
		return fmt.Errorf("open receipt hasher: %w", err)
	}
	m.receiptHasher = receiptHasher
	budHasher, err := bud.NewStandardBUDHasher(ctx, *m.cfg.BUDHasherConfig, appHashBuilder.ReportBUDHash)
	if err != nil {
		return fmt.Errorf("open BUD hasher: %w", err)
	}
	m.budHasher = budHasher
	return nil
}

// completeAppHashBuilderSetup connects the state DB's hashes to the app hash builder, and completes its setup at the
// height recovery left the state on.
//
// It must run after every other store has recovered, and before any block is executed.
func (m *GigaStorageManager) completeAppHashBuilderSetup(ctx context.Context) error {
	stateHeight, err := m.SC().GetLatestVersion()
	if err != nil {
		return fmt.Errorf("read state height: %w", err)
	}
	//nolint:gosec // state heights are non-negative
	height := uint64(stateHeight)
	if err := m.reportStateHashesToAppHashBuilder(height); err != nil {
		return err
	}
	if err := m.appHashBuilder.SetupComplete(ctx, height); err != nil {
		return fmt.Errorf("complete app hash builder setup at block %d: %w", height, err)
	}
	return nil
}

// reportStateHashesToAppHashBuilder registers the app hash builder for the state hash of every block committed from
// here on. Errors unless the first state hash delivered follows block height.
func (m *GigaStorageManager) reportStateHashesToAppHashBuilder(
	// The block recovery left the state on.
	height uint64,
) error {
	// The blocks replayed during recovery are hashed asynchronously. Their hashes have to reach the listeners
	// already registered before the builder registers, since the builder expects nothing at or below the
	// recovered height.
	if err := m.SC().FlushHashes(); err != nil {
		return fmt.Errorf("flush state hashes: %w", err)
	}
	stateHash, err := m.stateDB.RegisterHashListener(m.reportStateHash)
	if err != nil {
		return fmt.Errorf("register app hash builder for state hashes: %w", err)
	}
	//nolint:gosec // state heights are non-negative
	if stateHashHeight := uint64(stateHash.BlockNumber); stateHashHeight != height {
		return fmt.Errorf("the state commit store delivers state hashes after block %d, but the state is at "+
			"block %d", stateHashHeight, height)
	}
	return nil
}

// reportStateHash reports a committed block's state hash to the app hash builder, along with a zero block hash. Its
// signature is gigatypes.HashListener.
func (m *GigaStorageManager) reportStateHash(ctx context.Context, blockNumber int64, hash *lthash.BlockHash) error {
	//nolint:gosec // committed block numbers are non-negative
	height := uint64(blockNumber)
	// TODO: report the real block hash once execution reports it.
	if err := m.appHashBuilder.ReportBlockHash(ctx, height, [32]byte{}); err != nil {
		return fmt.Errorf("report the block hash of block %d: %w", height, err)
	}
	stateHash := hash.Global.Checksum()
	componentHashes := hashlog.FlatKVComponentHashes(hash)
	if err := m.appHashBuilder.ReportStateHash(ctx, height, stateHash, componentHashes); err != nil {
		return fmt.Errorf("report the state hash of block %d: %w", height, err)
	}
	return nil
}
