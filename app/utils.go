package app

import (
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	accesscontroltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	abci "github.com/tendermint/tendermint/abci/types"
)

type OptimisticProcessingInfo struct {
	Height     int64
	Hash       []byte
	Aborted    bool
	Completion chan struct{}
	// result fields
	Events       []abci.Event
	TxRes        []*abci.ExecTxResult
	EndBlockResp abci.ResponseEndBlock
}

type DagNode struct {
	NodeId          int
	TxIndex         int
	AccessOperation accesscontroltypes.AccessOperation
}

type DagEdge struct {
	FromNodeId      int
	ToNodeId        int
	AccessOperation *accesscontroltypes.AccessOperation
}

type Dag struct {
	NodeMap      map[int]DagNode
	EdgesMap     map[int][]DagEdge                          // maps node Id (from node) and contains edge info
	AccessOpsMap map[accesscontroltypes.AccessOperation]int // tracks latest node to use a specific access op
	TxIndexMap   map[int]int                                // tracks latest node ID for a tx index
	NextId       int
}

func (edge *DagEdge) GetCompletionSignal() *CompletionSignal {
	// only if there is an access operation
	if edge.AccessOperation != nil {
		return &CompletionSignal{
			FromNodeId:      edge.FromNodeId,
			ToNodeId:        edge.ToNodeId,
			AccessOperation: *edge.AccessOperation,
		}
	} else {
		return nil
	}
}

// Order returns the number of vertices in a graph.
func (dag *Dag) Order() int {
	return len(dag.NodeMap)
}

// Visit calls the do function for each neighbor w of vertex v, used by the graph acyclic validator
func (dag *Dag) Visit(v int, do func(w int, c int64) (skip bool)) (aborted bool) {
	for w, _ := range dag.EdgesMap[v] {
		// just have cost as zero because we only need for acyclic validation purposes
		if do(w, 0) {
			return true
		}
	}
	return false
}

func NewDag() Dag {
	return Dag{
		NodeMap:      make(map[int]DagNode),
		EdgesMap:     make(map[int][]DagEdge),
		AccessOpsMap: make(map[accesscontroltypes.AccessOperation]int),
		TxIndexMap:   make(map[int]int),
		NextId:       0,
	}
}

func (dag *Dag) AddNode(txIndex int, accessOp types.AccessOperation) DagNode {
	dagNode := DagNode{
		NodeId:          dag.NextId,
		TxIndex:         txIndex,
		AccessOperation: accessOp,
	}
	dag.NodeMap[dag.NextId] = dagNode
	dag.NextId += 1
	return dagNode
}

func (dag *Dag) AddEdge(fromIndex int, toIndex int, accessOp *accesscontroltypes.AccessOperation) *DagEdge {
	// no-ops if the from or to node doesn't exist
	if _, ok := dag.NodeMap[fromIndex]; !ok {
		return nil
	}
	if _, ok := dag.NodeMap[toIndex]; !ok {
		return nil
	}
	newEdge := DagEdge{fromIndex, toIndex, accessOp}
	edges, ok := dag.EdgesMap[fromIndex]
	if !ok {
		edges = []DagEdge{newEdge}
	}
	dag.EdgesMap[fromIndex] = append(edges, newEdge)
	return &newEdge
}

func (dag *Dag) AddNodeBuildDependency(ctx sdk.Context, txIndex int, accessOp types.AccessOperation) {
	dagNode := dag.AddNode(txIndex, accessOp)
	// if in TxIndexMap, make an edge from the previous node index
	if lastTxNodeId, ok := dag.TxIndexMap[txIndex]; ok {
		// add an edge with no access op
		dag.AddEdge(lastTxNodeId, dagNode.NodeId, nil)
		// update tx index map
		dag.TxIndexMap[txIndex] = dagNode.NodeId
	}
	// if the blocking access ops are in access ops map, make an edge
	switch accessOp.AccessType {
	case accesscontroltypes.AccessType_READ:
		// if we need to do a read, we need latest write as a dependency
		writeAccessOp := accesscontroltypes.AccessOperation{
			AccessType:         accesscontroltypes.AccessType_WRITE,
			ResourceType:       accessOp.GetResourceType(),
			IdentifierTemplate: accessOp.GetIdentifierTemplate(),
		}
		if writeNodeId, ok := dag.AccessOpsMap[writeAccessOp]; ok {
			// if accessOp exists already (and from a previous transaction), we need to define a dependency on the previous message (and make a edge between the two)
			writeNode := dag.NodeMap[writeNodeId]
			// if from a previous transaction, we need to create an edge
			if writeNode.TxIndex < dagNode.TxIndex {
				dag.AddEdge(writeNode.NodeId, dagNode.NodeId, &writeAccessOp)
			}
		}
	case accesscontroltypes.AccessType_WRITE:
		// if we need to do a write, we need read and write as dependencies
		writeAccessOp := accesscontroltypes.AccessOperation{
			AccessType:         accesscontroltypes.AccessType_WRITE,
			ResourceType:       accessOp.GetResourceType(),
			IdentifierTemplate: accessOp.GetIdentifierTemplate(),
		}
		if writeNodeId, ok := dag.AccessOpsMap[writeAccessOp]; ok {
			// if accessOp exists already (and from a previous transaction), we need to define a dependency on the previous message (and make a edge between the two)
			writeNode := dag.NodeMap[writeNodeId]
			// if from a previous transaction, we need to create an edge
			if writeNode.TxIndex < dagNode.TxIndex {
				dag.AddEdge(writeNode.NodeId, dagNode.NodeId, &writeAccessOp)
			}
		}
		readAccessOp := accesscontroltypes.AccessOperation{
			AccessType:         accesscontroltypes.AccessType_READ,
			ResourceType:       accessOp.GetResourceType(),
			IdentifierTemplate: accessOp.GetIdentifierTemplate(),
		}
		if readNodeId, ok := dag.AccessOpsMap[readAccessOp]; ok {
			// if accessOp exists already (and from a previous transaction), we need to define a dependency on the previous message (and make a edge between the two)
			readNode := dag.NodeMap[readNodeId]
			// if from a previous transaction, we need to create an edge
			if readNode.TxIndex < dagNode.TxIndex {
				dag.AddEdge(readNode.NodeId, dagNode.NodeId, &readAccessOp)
			}
		}
	default:
		// unknown or something else, raise error
		ctx.Logger().Error("Invalid AccessControlType Received")
	}
	// update access ops map with the latest node id using a specific access op
	dag.AccessOpsMap[accessOp] = dagNode.NodeId
}

// returns completion signaling map and blocking signals map
func (dag *Dag) BuildCompletionSignalMaps() (completionSignalingMap map[int]map[accesscontroltypes.AccessOperation][]CompletionSignal, blockingSignalsMap map[int]map[accesscontroltypes.AccessOperation][]CompletionSignal) {
	// go through every node
	for _, node := range dag.NodeMap {
		// for each node, assign its completion signaling, and also assign blocking signals for the destination nodes
		if outgoingEdges, ok := dag.EdgesMap[node.NodeId]; ok {
			var completionSignals []CompletionSignal
			for _, edge := range outgoingEdges {
				maybeCompletionSignal := edge.GetCompletionSignal()
				if maybeCompletionSignal != nil {
					completionSignal := *maybeCompletionSignal
					completionSignals = append(completionSignals, completionSignal)
					// also add it to the right blocking signal in the right txindex
					toNode := dag.NodeMap[edge.ToNodeId]
					if blockingSignals, ok2 := blockingSignalsMap[toNode.TxIndex][node.AccessOperation]; ok2 {
						blockingSignalsMap[toNode.TxIndex][node.AccessOperation] = append(blockingSignals, completionSignal)
					} else {
						blockingSignalsMap[toNode.TxIndex][node.AccessOperation] = []CompletionSignal{completionSignal}
					}
				}
			}
			// assign completion signals for the tx index
			if signals, ok3 := completionSignalingMap[node.TxIndex][node.AccessOperation]; ok3 {
				completionSignalingMap[node.TxIndex][node.AccessOperation] = append(signals, completionSignals...)
			} else {
				completionSignalingMap[node.TxIndex][node.AccessOperation] = completionSignals
			}
		}
	}
	return
}

type CompletionSignal struct {
	FromNodeId      int
	ToNodeId        int
	AccessOperation types.AccessOperation
}

type BlockProcessRequest interface {
	GetHash() []byte
	GetTxs() [][]byte
	GetByzantineValidators() []abci.Misbehavior
	GetHeight() int64
	GetTime() time.Time
}
