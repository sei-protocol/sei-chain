// SPDX-License-Identifier: UNLICENSED
package parallel

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	"github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
)

// AccessType represents the type of access a message has on a resource.
type AccessType uint32

const (
	AccessType_Unknown AccessType = iota
	AccessType_Read
	AccessType_Write
)

// AccessOp defines the access of a message to a particular resource.
type AccessOp struct {
	Type       AccessType `json:"type"`
	ResourceID string     `json:"resource_id"`
}

// ------------------- Access Control Keeper -------------------
// AccessMappingKeeper maintains the message-to-access mapping definitions.
type AccessMappingKeeper struct {
	storeKey storetypes.StoreKey
}

// NewAccessMappingKeeper creates a new AccessMappingKeeper instance.
func NewAccessMappingKeeper(sk storetypes.StoreKey) AccessMappingKeeper {
	return AccessMappingKeeper{storeKey: sk}
}

// GetOps fetches the access operations associated with the provided message key.
func (k AccessMappingKeeper) GetOps(ctx types.Context, msgKey string) ([]AccessOp, bool) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), []byte("access/"))
	bz := store.Get([]byte(msgKey))
	if bz == nil {
		return nil, false
	}
	var ops []AccessOp
	_ = json.Unmarshal(bz, &ops)
	return ops, true
}

// SetOps sets the access operations for the provided message key.
func (k AccessMappingKeeper) SetOps(ctx types.Context, msgKey string, ops []AccessOp) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), []byte("access/"))
	bz, _ := json.Marshal(ops)
	store.Set([]byte(msgKey), bz)
}

// DeleteOps removes the access operations associated with the provided message key.
func (k AccessMappingKeeper) DeleteOps(ctx types.Context, msgKey string) {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), []byte("access/"))
	store.Delete([]byte(msgKey))
}

// ------------------- Governance Msg + Handler -------------------
// MsgSetAccessMapping is the governance message to configure access mappings.
type MsgSetAccessMapping struct {
	Authority string     `json:"authority"`
	MsgKey    string     `json:"msg_key"`
	Ops       []AccessOp `json:"ops"`
}

func (m MsgSetAccessMapping) Route() string { return "accesscontrol" }
func (m MsgSetAccessMapping) Type() string  { return "set_access_mapping" }
func (m MsgSetAccessMapping) GetSigners() []types.AccAddress {
	addr, _ := types.AccAddressFromBech32(m.Authority)
	return []types.AccAddress{addr}
}

// NewHandler returns the handler capable of processing governance messages for the
// access mapping module.
func NewHandler(k AccessMappingKeeper, authority string) types.Handler {
	return func(ctx types.Context, msg types.Msg) (*types.Result, error) {
		switch m := msg.(type) {
		case *MsgSetAccessMapping:
			if m.Authority != authority {
				return nil, fmt.Errorf("unauthorized")
			}
			k.SetOps(ctx, m.MsgKey, m.Ops)
			return &types.Result{}, nil
		case MsgSetAccessMapping:
			if m.Authority != authority {
				return nil, fmt.Errorf("unauthorized")
			}
			k.SetOps(ctx, m.MsgKey, m.Ops)
			return &types.Result{}, nil
		default:
			return nil, fmt.Errorf("unrecognized accesscontrol message")
		}
	}
}

// ------------------- DAG Execution Runtime -------------------
// AccessOpInstance represents an instantiated access operation for a specific message in a transaction.
type AccessOpInstance struct {
	MsgIndex   int
	OpIndex    int
	Type       AccessType
	ResourceID string
	DependsOn  []chan struct{}
	Signal     chan struct{}
}

// TxContext wraps all the information needed to process a transaction in the DAG executor.
type TxContext struct {
	MsgIndex int
	Accesses []*AccessOpInstance
	Ctx      types.Context
	Msg      types.Msg
}

// DAGExecutor executes the provided transactions respecting the dependency graph between their access operations.
func DAGExecutor(txs []TxContext, handler func(TxContext) error) error {
	var wg sync.WaitGroup
	errs := make([]error, len(txs))
	for i, tx := range txs {
		tx := tx
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for _, access := range tx.Accesses {
				for _, dep := range access.DependsOn {
					<-dep
				}
			}
			err := handler(tx)
			errs[idx] = err
			for _, access := range tx.Accesses {
				close(access.Signal)
			}
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// ParallelAnteAndMsgs executes the ante handlers and message handlers for the provided messages in parallel while
// respecting the access mapping dependencies.
func ParallelAnteAndMsgs(txList []types.Msg, ctx types.Context, mm module.Manager, am AccessMappingKeeper) error {
	var txContexts []TxContext
	resourceSignals := make(map[string][]*AccessOpInstance)

	for i, msg := range txList {
		msgKey := msg.Route() + "/" + msg.Type()
		opDefs, found := am.GetOps(ctx, msgKey)
		if !found {
			return fmt.Errorf("missing access mapping for msg %s", msgKey)
		}
		var accessList []*AccessOpInstance
		for j, def := range opDefs {
			access := &AccessOpInstance{
				MsgIndex:   i,
				OpIndex:    j,
				Type:       def.Type,
				ResourceID: fmt.Sprintf(def.ResourceID, i),
				Signal:     make(chan struct{}),
			}
			for _, prev := range resourceSignals[access.ResourceID] {
				if prev.Type == AccessType_Write || access.Type == AccessType_Write {
					access.DependsOn = append(access.DependsOn, prev.Signal)
				}
			}
			resourceSignals[access.ResourceID] = append(resourceSignals[access.ResourceID], access)
			accessList = append(accessList, access)
		}
		txContexts = append(txContexts, TxContext{
			MsgIndex: i,
			Accesses: accessList,
			Ctx:      ctx,
			Msg:      msg,
		})
	}

	handler := func(tx TxContext) error {
		return mm.Route(tx.Msg.Route()).Handler()(tx.Ctx, tx.Msg)
	}

	return DAGExecutor(txContexts, handler)
}
