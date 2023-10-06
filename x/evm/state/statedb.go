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
type StateDBImpl struct {
	ctx             sdk.Context
	snapshottedCtxs []sdk.Context
	// If err is not nil at the end of the execution, the transaction will be rolled
	// back.
	err error

	k EVMKeeper
}

func NewStateDBImpl(ctx sdk.Context, k EVMKeeper) *StateDBImpl {
	s := &StateDBImpl{
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
func (s *StateDBImpl) AddPreimage(_ common.Hash, _ []byte) {}

func (s *StateDBImpl) Finalize() error {
	if s.err != nil {
		return s.err
	}
	if err := s.CheckBalance(); err != nil {
		return err
	}

	if logs, err := s.GetLogs(); err != nil {
		return err
	} else {
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
	}

	// remove transient states
	s.k.PurgePrefix(s.ctx, types.TransientStateKeyPrefix)
	s.k.PurgePrefix(s.ctx, types.AccountTransientStateKeyPrefix)
	s.k.PurgePrefix(s.ctx, types.TransientModuleStateKeyPrefix)

	// write cache to underlying
	s.ctx.MultiStore().(sdk.CacheMultiStore).Write()
	// write all snapshotted caches in reverse order, except the very first one (base) which will be written by baseapp::runTx
	for i := len(s.snapshottedCtxs) - 1; i > 0; i-- {
		s.snapshottedCtxs[i].MultiStore().(sdk.CacheMultiStore).Write()
	}
	return nil
}

// ** TEST ONLY FUNCTIONS **//
func (s *StateDBImpl) Err() error {
	return s.err
}

func (s *StateDBImpl) WithErr(err error) {
	s.err = err
}

func (s *StateDBImpl) Ctx() sdk.Context {
	return s.ctx
}

func (s *StateDBImpl) WithCtx(ctx sdk.Context) {
	s.ctx = ctx
}
