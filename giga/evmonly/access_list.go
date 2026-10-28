package evmonly

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth/tracers/logger"
	"github.com/ethereum/go-ethereum/params"
)

// CreateAccessList returns the access list msg touches when executed against the current
// committed state, leaving out its sender, its recipient, the precompiles and its authorities, and
// the result of executing msg with that list. Like Call it persists no state change.
func (e *Executor) CreateAccessList(ctx context.Context, blockCtx BlockContext, msg *core.Message) (ethtypes.AccessList, *core.ExecutionResult, error) {
	chainConfig := e.chainConfig(blockCtx)
	if err := validateBlockContext(chainConfig, blockCtx); err != nil {
		return nil, nil, err
	}
	if e.stateStore == nil {
		return nil, nil, errMissingStateStore
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if uint64(len(msg.SetCodeAuthorizations)) > msg.GasLimit/params.CallNewAccountGas {
		return nil, nil, errors.New("insufficient gas to process all authorizations")
	}

	snapshot := e.stateStore.OpenView()
	if snapshot == nil {
		return nil, nil, errors.New("giga store returned a nil snapshot")
	}
	defer snapshot.Close()
	reader := gigaSnapshotStateReader{snapshot: snapshot, missingState: e.missingState}

	listCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	excluded := e.accessListExclusions(chainConfig, blockCtx, reader, msg)
	// Each run may touch what the previous list warmed differently, so rerun until the list is stable.
	prev := logger.NewAccessListTracer(msg.AccessList, excluded)
	for {
		accessList := prev.AccessList()
		run := *msg
		run.AccessList = accessList
		tracer := logger.NewAccessListTracer(accessList, excluded)
		result, err := e.traceAccessList(listCtx, blockCtx, chainConfig, reader, &run, tracer)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, nil, ctxErr
		}
		if err != nil {
			return nil, nil, err
		}
		if tracer.Equal(prev) {
			return accessList, result, nil
		}
		prev = tracer
	}
}

// traceAccessList executes msg on a fresh view of reader with tracer recording what it touches.
func (e *Executor) traceAccessList(
	ctx context.Context,
	blockCtx BlockContext,
	chainConfig *params.ChainConfig,
	reader StateReader,
	msg *core.Message,
	tracer *logger.AccessListTracer,
) (*core.ExecutionResult, error) {
	stateDB := e.acquireStateDB(reader)
	defer e.releaseStateDB(stateDB)
	result, cancelled, err := e.applyMessage(ctx, blockCtx, chainConfig, stateDB, msg, vm.Config{Tracer: tracer.Hooks(), NoBaseFee: true})
	if cancelled {
		return nil, fmt.Errorf("EVM-only access list exceeded %s execution timeout", callTimeout)
	}
	if stateErr := stateDB.Error(); stateErr != nil {
		return nil, stateErr
	}
	if err != nil {
		return nil, fmt.Errorf("failed to apply transaction: %w", err)
	}
	return result, nil
}

// accessListExclusions returns the addresses an access list for msg leaves out: its sender, its
// recipient or the contract it creates, the active precompiles, and the authorities of its
// authorizations that can apply on this chain.
func (e *Executor) accessListExclusions(chainConfig *params.ChainConfig, blockCtx BlockContext, reader StateReader, msg *core.Message) map[common.Address]struct{} {
	to := msg.To
	if to == nil {
		stateDB := e.acquireStateDB(reader)
		created := crypto.CreateAddress(msg.From, stateDB.GetNonce(msg.From))
		e.releaseStateDB(stateDB)
		to = &created
	}
	excluded := map[common.Address]struct{}{msg.From: {}, *to: {}}
	evmBlock := buildBlockContext(blockCtx)
	for _, precompile := range vm.ActivePrecompiles(chainConfig.Rules(evmBlock.BlockNumber, evmBlock.Random != nil, evmBlock.Time)) {
		excluded[precompile] = struct{}{}
	}
	for precompile := range e.customContracts {
		excluded[precompile] = struct{}{}
	}
	for _, auth := range msg.SetCodeAuthorizations {
		if (!auth.ChainID.IsZero() && auth.ChainID.CmpBig(chainConfig.ChainID) != 0) || auth.Nonce+1 < auth.Nonce {
			continue
		}
		if authority, err := auth.Authority(); err == nil {
			excluded[authority] = struct{}{}
		}
	}
	return excluded
}
