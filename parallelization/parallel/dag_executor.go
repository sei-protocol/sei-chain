// SPDX-License-Identifier: UNLICENSED
package parallel

import (
	"fmt"
	"sync"

	"github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
)

type AccessType uint32

const (
	AccessType_Unknown AccessType = iota
	AccessType_Read
	AccessType_Write
)

type AccessOp struct {
	MsgIndex   int
	OpIndex    int
	Type       AccessType
	ResourceID string
	DependsOn  []chan struct{} // Wait for these
	Signal     chan struct{}   // Signal completion to others
}

type TxContext struct {
	MsgIndex int
	Accesses []*AccessOp
	Ctx      types.Context
	Msg      types.Msg
}

// DAGExecutor runs message handlers in parallel with enforced dependencies
func DAGExecutor(txs []TxContext, handler func(TxContext) error) error {
	var wg sync.WaitGroup
	errs := make([]error, len(txs))
	for i, tx := range txs {
		tx := tx // capture range var
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

// Integrates with the Cosmos SDK app layer via module manager
func ParallelAnteAndMsgs(txList []types.Msg, accessMapping map[string][]AccessOp, ctx types.Context, mm module.Manager) error {
	var txContexts []TxContext
	resourceSignals := make(map[string][]*AccessOp)

	for i, msg := range txList {
		msgKey := msg.Route() + "/" + msg.Type()
		ops := accessMapping[msgKey]
		var accessList []*AccessOp
		for j, op := range ops {
			access := &AccessOp{
				MsgIndex:   i,
				OpIndex:    j,
				Type:       op.Type,
				ResourceID: fmt.Sprintf(op.ResourceID, i),
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
