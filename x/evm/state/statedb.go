package state

import (
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

// Initialized for each transaction individually
type DBImpl struct {
	ctx             sdk.Context
	snapshottedCtxs []sdk.Context
	// If err is not nil at the end of the execution, the transaction will be rolled
	// back.
	err error

	k EVMKeeper
}

func NewDBImpl(ctx sdk.Context, k EVMKeeper) *DBImpl {
	s := &DBImpl{
		ctx:             ctx,
		k:               k,
		snapshottedCtxs: []sdk.Context{},
	}
	s.Snapshot()                                                                            // take an initial snapshot for GetCommitted
	s.AddBigIntTransientModuleState(k.GetModuleBalance(s.ctx), TotalUnassociatedBalanceKey) // set total unassociated balance to be current module balance
	return s
}

// AddPreimage records a SHA3 preimage seen by the VM.
// AddPreimage performs a no-op since the EnablePreimageRecording flag is disabled
// on the vm.Config during state transitions. No store trie preimages are written
// to the database.
func (s *DBImpl) AddPreimage(_ common.Hash, _ []byte) {}

func (s *DBImpl) Finalize() error {
	if s.err != nil {
		return s.err
	}
	if err := s.CheckBalance(); err != nil {
		return err
	}

	logs, err := s.GetLogs()
	if err != nil {
		return err
	}
	for _, l := range logs {
		s.snapshottedCtxs[0].EventManager().EmitEvent(sdk.NewEvent(
			types.EventTypeEVMLog,
			sdk.NewAttribute(types.AttributeTypeContractAddress, l.Address.Hex()),
			sdk.NewAttribute(types.AttributeTypeTopics, strings.Join(
				utils.Map(l.Topics, func(h common.Hash) string { return h.Hex() }), ",",
			)),
			sdk.NewAttribute(types.AttributeTypeData, string(l.Data)),
			sdk.NewAttribute(types.AttributeTypeBlockHash, l.BlockHash.Hex()),
			sdk.NewAttribute(types.AttributeTypeBlockNumber, fmt.Sprintf("%d", l.BlockNumber)),
			sdk.NewAttribute(types.AttributeTypeTxHash, l.TxHash.Hex()),
			sdk.NewAttribute(types.AttributeTypeTxIndex, fmt.Sprintf("%d", l.TxIndex)),
			sdk.NewAttribute(types.AttributeTypeIndex, fmt.Sprintf("%d", l.Index)),
			sdk.NewAttribute(types.AttributeTypeRemoved, fmt.Sprintf("%t", l.Removed)),
		))
	}

	// remove transient states
	// write cache to underlying
	s.flushCtx(s.ctx)
	// write all snapshotted caches in reverse order, except the very first one (base) which will be written by baseapp::runTx
	for i := len(s.snapshottedCtxs) - 1; i > 0; i-- {
		s.flushCtx(s.snapshottedCtxs[i])
	}
	return nil
}

func (s *DBImpl) flushCtx(ctx sdk.Context) {
	s.k.PurgePrefix(ctx, types.TransientStateKeyPrefix)
	s.k.PurgePrefix(ctx, types.AccountTransientStateKeyPrefix)
	s.k.PurgePrefix(ctx, types.TransientModuleStateKeyPrefix)
	ctx.MultiStore().(sdk.CacheMultiStore).Write()
}

// ** TEST ONLY FUNCTIONS **//
func (s *DBImpl) Err() error {
	return s.err
}

func (s *DBImpl) WithErr(err error) {
	s.err = err
}

func (s *DBImpl) Ctx() sdk.Context {
	return s.ctx
}

func (s *DBImpl) WithCtx(ctx sdk.Context) {
	s.ctx = ctx
}
