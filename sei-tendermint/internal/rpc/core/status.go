package core

import (
	"bytes"
	"context"
	"fmt"
	"time"

	sm "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state"
	tmbytes "github.com/sei-protocol/sei-chain/sei-tendermint/libs/bytes"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// Status returns Tendermint status including node info, pubkey, latest block
// hash, app hash, block height, current max peer block height, and time.
// More: https://docs.tendermint.com/master/rpc/#/Info/status
func (env *Environment) Status(ctx context.Context) (*coretypes.ResultStatus, error) {
	var (
		earliestBlockHeight   int64
		earliestBlockHash     tmbytes.HexBytes
		earliestAppHash       tmbytes.HexBytes
		earliestBlockTimeNano int64
	)

	if earliestBlockMeta := env.BlockStore.LoadBaseMeta(); earliestBlockMeta != nil {
		earliestBlockHeight = earliestBlockMeta.Header.Height
		earliestAppHash = earliestBlockMeta.Header.AppHash
		earliestBlockHash = earliestBlockMeta.BlockID.Hash
		earliestBlockTimeNano = earliestBlockMeta.Header.Time.UnixNano()
	}

	var (
		latestBlockHash     tmbytes.HexBytes
		latestAppHash       tmbytes.HexBytes
		latestBlockTimeNano int64

		latestHeight = env.BlockStore.Height()
	)

	if latestHeight != 0 {
		if latestBlockMeta := env.BlockStore.LoadBlockMeta(latestHeight); latestBlockMeta != nil {
			latestBlockHash = latestBlockMeta.BlockID.Hash
			latestAppHash = latestBlockMeta.Header.AppHash
			latestBlockTimeNano = latestBlockMeta.Header.Time.UnixNano()
		}
	}

	lastCommittedBlockHeight := latestHeight

	// Return the very last voting power, not the voting power of this validator
	// during the last block.
	validatorInfo := coretypes.ValidatorInfo{PubKey: env.PubKey}
	if val, ok := env.validatorAtHeight(env.latestUncommittedHeight()).Get(); ok {
		validatorInfo.VotingPower = val.VotingPower
	}
	abciInfo, err := env.ABCIInfo(ctx)
	if err != nil {
		return nil, err
	}
	applicationInfo := coretypes.ApplicationInfo{Version: fmt.Sprint(abciInfo.Response.AppVersion)}

	freezeHeight, frozen := env.freezeState()

	result := &coretypes.ResultStatus{
		NodeInfo:        env.NodeInfo,
		ApplicationInfo: applicationInfo,
		SyncInfo: coretypes.SyncInfo{
			LatestBlockHash:          latestBlockHash,
			LatestAppHash:            latestAppHash,
			LatestBlockHeight:        latestHeight,
			LatestBlockTime:          time.Unix(0, latestBlockTimeNano),
			LastCommittedBlockHeight: lastCommittedBlockHeight,
			EarliestBlockHash:        earliestBlockHash,
			EarliestAppHash:          earliestAppHash,
			EarliestBlockHeight:      earliestBlockHeight,
			EarliestBlockTime:        time.Unix(0, earliestBlockTimeNano),
			FreezeHeight:             freezeHeight,
			Frozen:                   frozen,
			// this should start as true, if consensus
			// hasn't started yet, and then flip to false
			// (or true,) depending on what's actually
			// happening.
			CatchingUp: true,
		},
		ValidatorInfo: validatorInfo,
	}

	if reactor, ok := env.ConsensusReactor.Get(); ok {
		result.SyncInfo.CatchingUp = reactor.WaitSync()
	}

	if reactor, ok := env.BlockSyncReactor.Get(); ok {
		result.SyncInfo.MaxPeerBlockHeight = reactor.GetMaxPeerBlockHeight()
		result.SyncInfo.TotalSyncedTime = reactor.GetTotalSyncedTime()
		result.SyncInfo.RemainingTime = reactor.GetRemainingSyncTime()
	}

	if reactor, ok := env.StateSyncReactor.Get(); ok {
		result.SyncInfo.TotalSnapshots = reactor.TotalSnapshots()
		result.SyncInfo.ChunkProcessAvgTime = reactor.ChunkProcessAvgTime()
		result.SyncInfo.SnapshotHeight = reactor.SnapshotHeight()
		result.SyncInfo.SnapshotChunksCount = reactor.SnapshotChunksCount()
		result.SyncInfo.SnapshotChunksTotal = reactor.SnapshotChunksTotal()
		result.SyncInfo.BackFilledBlocks = reactor.BackFilledBlocks()
		result.SyncInfo.BackFillBlocksTotal = reactor.BackFillBlocksTotal()
	}

	return result, nil
}

// freezeState returns the configured freeze height and whether this node has stopped
// at it. It returns (0, false) when freeze mode is off.
func (env *Environment) freezeState() (int64, bool) {
	if env.FreezeHeight == 0 {
		return 0, false
	}
	freezeHeight := int64(env.FreezeHeight) //nolint:gosec // freeze-height is validated to fit in int64 at startup.
	// Read the executed state, not the block store: consensus saves a block before executing it.
	state, err := env.StateStore.Load()
	if err != nil || state.IsEmpty() {
		return freezeHeight, false
	}
	return freezeHeight, nextHeightToExecute(state) >= freezeHeight
}

// nextHeightToExecute returns the height of the next block after state, which is the
// genesis initial height when no block has been executed.
func nextHeightToExecute(state sm.State) int64 {
	// Must match consensus nextHeightForState and blocksync startHeightForState, which gate freezing.
	height := state.LastBlockHeight + 1
	if height == 1 {
		height = state.InitialHeight
	}
	return height
}

func (env *Environment) validatorAtHeight(h int64) utils.Option[*types.Validator] {
	none := utils.None[*types.Validator]()
	k, ok := env.PubKey.Get()
	if !ok {
		return none
	}
	valsWithH, err := env.StateStore.LoadValidators(h)
	if err != nil {
		return none
	}
	privValAddress := k.Address()

	if consensusState, ok := env.ConsensusState.Get(); ok {
		lastBlockHeight, vals := consensusState.GetValidators()
		if lastBlockHeight == h {
			for _, val := range vals {
				if bytes.Equal(val.Address, privValAddress) {
					return utils.Some(val)
				}
			}
		}
	}

	_, val, ok := valsWithH.GetByAddress(privValAddress)
	if ok {
		return utils.Some(val)
	}
	return none
}
