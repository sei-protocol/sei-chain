package app

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
)

type DagNode struct {
	NodeID          int
	TxIndex         int
	AccessOperation acltypes.AccessOperation
}

type DagEdge struct {
	FromNodeID      int
	ToNodeID        int
	AccessOperation *acltypes.AccessOperation
}

type Dag struct {
	NodeMap      map[int]DagNode
	EdgesMap     map[int][]DagEdge                  // maps node Id (from node) and contains edge info
	AccessOpsMap map[acltypes.AccessOperation][]int // tracks latest node to use a specific access op
	TxIndexMap   map[int]int                        // tracks latest node ID for a tx index
	NextID       int
}

func (edge *DagEdge) GetCompletionSignal() *CompletionSignal {
	// only if there is an access operation
	if edge.AccessOperation != nil {
		return &CompletionSignal{
			FromNodeID:      edge.FromNodeID,
			ToNodeID:        edge.ToNodeID,
			AccessOperation: *edge.AccessOperation,
		}
	}
	return nil
}

// Order returns the number of vertices in a graph.
func (dag Dag) Order() int {
	return len(dag.NodeMap)
}

// Visit calls the do function for each neighbor w of vertex v, used by the graph acyclic validator
func (dag Dag) Visit(v int, do func(w int, c int64) (skip bool)) (aborted bool) {
	for _, edge := range dag.EdgesMap[v] {
		// just have cost as zero because we only need for acyclic validation purposes
		if do(edge.ToNodeID, 0) {
			return true
		}
	}
	return false
}

func NewDag() Dag {
	return Dag{
		NodeMap:      make(map[int]DagNode),
		EdgesMap:     make(map[int][]DagEdge),
		AccessOpsMap: make(map[acltypes.AccessOperation][]int),
		TxIndexMap:   make(map[int]int),
		NextID:       0,
	}
}

func (dag *Dag) AddNode(txIndex int, accessOp acltypes.AccessOperation) DagNode {
	dagNode := DagNode{
		NodeID:          dag.NextID,
		TxIndex:         txIndex,
		AccessOperation: accessOp,
	}
	dag.NodeMap[dag.NextID] = dagNode
	dag.NextID++
	return dagNode
}

func (dag *Dag) AddEdge(fromIndex int, toIndex int, accessOp *acltypes.AccessOperation) *DagEdge {
	// no-ops if the from or to node doesn't exist
	if _, ok := dag.NodeMap[fromIndex]; !ok {
		return nil
	}
	if _, ok := dag.NodeMap[toIndex]; !ok {
		return nil
	}
	newEdge := DagEdge{fromIndex, toIndex, accessOp}
	dag.EdgesMap[fromIndex] = append(dag.EdgesMap[fromIndex], newEdge)
	return &newEdge
}

func (dag *Dag) AddNodeBuildDependency(ctx sdk.Context, txIndex int, accessOp acltypes.AccessOperation) {
	dagNode := dag.AddNode(txIndex, accessOp)
	// if in TxIndexMap, make an edge from the previous node index
	if lastTxNodeID, ok := dag.TxIndexMap[txIndex]; ok {
		// add an edge with no access op
		dag.AddEdge(lastTxNodeID, dagNode.NodeID, nil)
	}
	// update tx index map
	dag.TxIndexMap[txIndex] = dagNode.NodeID
	// if the blocking access ops are in access ops map, make an edge
	switch accessOp.AccessType {
	case acltypes.AccessType_READ:
		// if we need to do a read, we need latest write as a dependency
		// TODO: replace hardcoded access op dependencies with helper that generates (and also generates superseding resources too eg. Resource.ALL is blocking for Resource.KV)
		writeAccessOp := acltypes.AccessOperation{
			AccessType:         acltypes.AccessType_WRITE,
			ResourceType:       accessOp.GetResourceType(),
			IdentifierTemplate: accessOp.GetIdentifierTemplate(),
		}
		if writeNodeIDs, ok := dag.AccessOpsMap[writeAccessOp]; ok {
			for _, wn := range writeNodeIDs {
				writeNode := dag.NodeMap[wn]
				// if accessOp exists already (and from a previous transaction), we need to define a dependency on the previous message (and make a edge between the two)
				// if from a previous transaction, we need to create an edge
				if writeNode.TxIndex < dagNode.TxIndex {
					lastTxNode := dag.NodeMap[dag.TxIndexMap[writeNode.TxIndex]]
					dag.AddEdge(lastTxNode.NodeID, dagNode.NodeID, &writeAccessOp)
				}
			}
		}
	case acltypes.AccessType_WRITE:
		// if we need to do a write, we need read and write as dependencies
		writeAccessOp := acltypes.AccessOperation{
			AccessType:         acltypes.AccessType_WRITE,
			ResourceType:       accessOp.GetResourceType(),
			IdentifierTemplate: accessOp.GetIdentifierTemplate(),
		}
		if writeNodeIDs, ok := dag.AccessOpsMap[writeAccessOp]; ok {
			for _, wn := range writeNodeIDs {
				// if accessOp exists already (and from a previous transaction), we need to define a dependency on the previous message (and make a edge between the two)
				writeNode := dag.NodeMap[wn]
				// if from a previous transaction, we need to create an edge
				if writeNode.TxIndex < dagNode.TxIndex {
					// we need to get the last node from that tx
					lastTxNode := dag.NodeMap[dag.TxIndexMap[writeNode.TxIndex]]
					dag.AddEdge(lastTxNode.NodeID, dagNode.NodeID, &writeAccessOp)
				}
			}
		}
		readAccessOp := acltypes.AccessOperation{
			AccessType:         acltypes.AccessType_READ,
			ResourceType:       accessOp.GetResourceType(),
			IdentifierTemplate: accessOp.GetIdentifierTemplate(),
		}
		if readNodeIDs, ok := dag.AccessOpsMap[readAccessOp]; ok {
			for _, rn := range readNodeIDs {
				readNode := dag.NodeMap[rn]
				// if accessOp exists already (and from a previous transaction), we need to define a dependency on the previous message (and make a edge between the two)
				// if from a previous transaction, we need to create an edge
				if readNode.TxIndex < dagNode.TxIndex {
					dag.AddEdge(readNode.NodeID, dagNode.NodeID, &readAccessOp)
				}
			}
		}
	default:
		// unknown or something else, raise error
		ctx.Logger().Error("Invalid AccessControlType Received")
	}
	// update access ops map with the latest node id using a specific access op
	dag.AccessOpsMap[accessOp] = append(dag.AccessOpsMap[accessOp], dagNode.NodeID)
}

// returns completion signaling map and blocking signals map
func (dag *Dag) BuildCompletionSignalMaps() (completionSignalingMap map[int]map[acltypes.AccessOperation][]CompletionSignal, blockingSignalsMap map[int]map[acltypes.AccessOperation][]CompletionSignal) {
	// go through every node
	for _, node := range dag.NodeMap {
		// for each node, assign its completion signaling, and also assign blocking signals for the destination nodes
		if outgoingEdges, ok := dag.EdgesMap[node.NodeID]; ok {
			for _, edge := range outgoingEdges {
				maybeCompletionSignal := edge.GetCompletionSignal()
				if maybeCompletionSignal != nil {
					completionSignal := *maybeCompletionSignal
					// add it to the right blocking signal in the right txindex
					toNode := dag.NodeMap[edge.ToNodeID]
					blockingSignalsMap[toNode.TxIndex][*edge.AccessOperation] = append(blockingSignalsMap[toNode.TxIndex][*edge.AccessOperation], completionSignal)
					// add it to the completion signal for the tx index
					completionSignalingMap[node.TxIndex][*edge.AccessOperation] = append(completionSignalingMap[node.TxIndex][*edge.AccessOperation], completionSignal)
				}

			}
		}
	}
	return
}

type CompletionSignal struct {
	FromNodeID      int
	ToNodeID        int
	AccessOperation acltypes.AccessOperation
}

var ErrCycleInDAG = fmt.Errorf("cycle detected in DAG")
