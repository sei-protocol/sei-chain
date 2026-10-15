package bootstrap

import (
	"context"
	"fmt"

	"github.com/sei-protocol/sei-chain/giga/apphash/builder"
	"github.com/sei-protocol/sei-chain/giga/apphash/vault"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/hashlog"
)

// openAppHashBuilder opens the hash vault and the app hash builder over it, connects the state DB's hashes to the
// builder, and completes its setup at the height recovery left the state on.
//
// It must run after every other store has recovered, and before any block is executed.
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
	if err := m.reportStateHashesToAppHashBuilder(); err != nil {
		return err
	}
	height, err := m.SC().GetLatestVersion()
	if err != nil {
		return fmt.Errorf("read state height: %w", err)
	}
	//nolint:gosec // state heights are non-negative
	if err := m.appHashBuilder.SetupComplete(ctx, uint64(height)); err != nil {
		return fmt.Errorf("complete app hash builder setup at block %d: %w", height, err)
	}
	return nil
}

// reportStateHashesToAppHashBuilder registers the app hash builder for the inputs of every block committed from here
// on.
func (m *GigaStorageManager) reportStateHashesToAppHashBuilder() error {
	// The blocks replayed during recovery are hashed asynchronously. Their hashes have to reach the listeners
	// already registered before the builder registers, since the builder expects nothing at or below the
	// recovered height.
	if err := m.SC().FlushHashes(); err != nil {
		return fmt.Errorf("flush state hashes: %w", err)
	}
	if _, err := m.stateDB.RegisterHashListener(m.reportStateHash); err != nil {
		return fmt.Errorf("register app hash builder for state hashes: %w", err)
	}
	if _, _, _, err := m.StateWAL().RegisterHashListener(m.appHashBuilder.ReportBUDHash); err != nil {
		return fmt.Errorf("register app hash builder for BUD hashes: %w", err)
	}
	return nil
}

// reportStateHash reports a committed block's state hash to the app hash builder, along with a zero block hash and a
// zero receipt hash. Its signature is gigatypes.HashListener.
func (m *GigaStorageManager) reportStateHash(ctx context.Context, blockNumber int64, hash *lthash.BlockHash) error {
	//nolint:gosec // committed block numbers are non-negative
	height := uint64(blockNumber)
	// TODO: report the real block hash and receipt hash once execution reports them.
	if err := m.appHashBuilder.ReportBlockHash(ctx, height, [32]byte{}); err != nil {
		return fmt.Errorf("report the block hash of block %d: %w", height, err)
	}
	if err := m.appHashBuilder.ReportReceiptHash(ctx, height, [32]byte{}); err != nil {
		return fmt.Errorf("report the receipt hash of block %d: %w", height, err)
	}
	stateHash := hash.Global.Checksum()
	componentHashes := hashlog.FlatKVComponentHashes(hash)
	if err := m.appHashBuilder.ReportStateHash(ctx, height, stateHash, componentHashes); err != nil {
		return fmt.Errorf("report the state hash of block %d: %w", height, err)
	}
	return nil
}
