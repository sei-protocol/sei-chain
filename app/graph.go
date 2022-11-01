package app

import (
	"fmt"

	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
)

type DagNodeID int

type DagNode struct {
	NodeID          DagNodeID
	TxIndex         int
	AccessOperation acltypes.AccessOperation
}

type DagEdge struct {
	FromNodeID DagNodeID
	ToNodeID   DagNodeID
}

type Dag struct {
	NodeMap      map[DagNodeID]DagNode
	EdgesMap     map[DagNodeID][]DagEdge                  // maps node Id (from node) and contains edge info
	AccessOpsMap map[acltypes.AccessOperation][]DagNodeID // tracks latest node to use a specific access op
	TxIndexMap   map[int]DagNodeID                        // tracks latest node ID for a tx index
	NextID       DagNodeID
}

func (dag *Dag) GetCompletionSignal(edge DagEdge) *CompletionSignal {
	// only if tx indexes are different
	fromNode := dag.NodeMap[edge.FromNodeID]
	toNode := dag.NodeMap[edge.ToNodeID]
	if fromNode.TxIndex == toNode.TxIndex {
		return nil
	}
	return &CompletionSignal{
		FromNodeID:                fromNode.NodeID,
		ToNodeID:                  toNode.NodeID,
		CompletionAccessOperation: fromNode.AccessOperation,
		BlockedAccessOperation:    toNode.AccessOperation,
	}
}

// Order returns the number of vertices in a graph.
func (dag Dag) Order() int {
	return len(dag.NodeMap)
}

// Visit calls the do function for each neighbor w of vertex v, used by the graph acyclic validator
func (dag Dag) Visit(v int, do func(w int, c int64) (skip bool)) (aborted bool) {
	for _, edge := range dag.EdgesMap[DagNodeID(v)] {
		// just have cost as zero because we only need for acyclic validation purposes
		if do(int(edge.ToNodeID), 0) {
			return true
		}
	}
	return false
}

func NewDag() Dag {
	return Dag{
		NodeMap:      make(map[DagNodeID]DagNode),
		EdgesMap:     make(map[DagNodeID][]DagEdge),
		AccessOpsMap: make(map[acltypes.AccessOperation][]DagNodeID),
		TxIndexMap:   make(map[int]DagNodeID),
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

func (dag *Dag) AddEdge(fromIndex DagNodeID, toIndex DagNodeID) *DagEdge {
	// no-ops if the from or to node doesn't exist
	if _, ok := dag.NodeMap[fromIndex]; !ok {
		return nil
	}
	if _, ok := dag.NodeMap[toIndex]; !ok {
		return nil
	}
	newEdge := DagEdge{fromIndex, toIndex}
	dag.EdgesMap[fromIndex] = append(dag.EdgesMap[fromIndex], newEdge)
	return &newEdge
}

// This function is a helper used to build the dependency graph one access operation at a time.
// It will first add a node corresponding to the tx index and access operation (linking it to the previous most recent node for that tx if applicable)
// and then will build edges from any access operations on which the new node is dependent.
//
// This will be accomplished using the AccessOpsMap in dag which keeps track of which nodes access which resources.
// It will then create an edge between the relevant node upon which it is dependent, and this edge can later be used to build the completion signals
// that will allow the dependent goroutines to cordinate execution safely.
//
// It will also register the new node with AccessOpsMap so that future nodes that amy be dependent on this one can properly identify the dependency.
func (dag *Dag) AddNodeBuildDependency(txIndex int, accessOp acltypes.AccessOperation) {
	dagNode := dag.AddNode(txIndex, accessOp)
	// if in TxIndexMap, make an edge from the previous node index
	if lastTxNodeID, ok := dag.TxIndexMap[txIndex]; ok {
		// TODO: we actually don't necessarily need these edges, but keeping for now so we can first determine that cycles can't be missed if we remove these
		// add an edge between access ops in a transaction
		dag.AddEdge(lastTxNodeID, dagNode.NodeID)
	}
	// update tx index map
	dag.TxIndexMap[txIndex] = dagNode.NodeID

	nodeDependencies := dag.GetNodeDependencies(dagNode)
	// build edges for each of the dependencies
	for _, nodeDependency := range nodeDependencies {
		dag.AddEdge(nodeDependency.NodeID, dagNode.NodeID)
	}

	// update access ops map with the latest node id using a specific access op
	dag.AccessOpsMap[accessOp] = append(dag.AccessOpsMap[accessOp], dagNode.NodeID)
}

// This helper will identify nodes that are dependencies for the current node, and can then be used for creating edges between then for future completion signals
func (dag *Dag) GetNodeDependencies(node DagNode) (nodeDependencies []DagNode) {
	accessOp := node.AccessOperation
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
				if writeNode.TxIndex < node.TxIndex {
					// this should be the COMMIT access op for the tx
					lastTxNode := dag.NodeMap[dag.TxIndexMap[writeNode.TxIndex]]
					nodeDependencies = append(nodeDependencies, lastTxNode)
				}
			}
		}
	case acltypes.AccessType_WRITE, acltypes.AccessType_UNKNOWN:
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
				if writeNode.TxIndex < node.TxIndex {
					// we need to get the last node from that tx
					lastTxNode := dag.NodeMap[dag.TxIndexMap[writeNode.TxIndex]]
					nodeDependencies = append(nodeDependencies, lastTxNode)
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
				if readNode.TxIndex < node.TxIndex {
					nodeDependencies = append(nodeDependencies, readNode)
				}
			}
		}
	}
	return nodeDependencies
}

// returns completion signaling map and blocking signals map
func (dag *Dag) BuildCompletionSignalMaps() (completionSignalingMap map[int]map[acltypes.AccessOperation][]CompletionSignal, blockingSignalsMap map[int]map[acltypes.AccessOperation][]CompletionSignal) {
	// go through every node
	for _, node := range dag.NodeMap {
		// for each node, assign its completion signaling, and also assign blocking signals for the destination nodes
		if outgoingEdges, ok := dag.EdgesMap[node.NodeID]; ok {
			for _, edge := range outgoingEdges {
				maybeCompletionSignal := dag.GetCompletionSignal(edge)
				if maybeCompletionSignal != nil {
					completionSignal := *maybeCompletionSignal
					// add it to the right blocking signal in the right txindex
					toNode := dag.NodeMap[edge.ToNodeID]
					blockingSignalsMap[toNode.TxIndex][completionSignal.BlockedAccessOperation] = append(blockingSignalsMap[toNode.TxIndex][completionSignal.BlockedAccessOperation], completionSignal)
					// add it to the completion signal for the tx index
					completionSignalingMap[node.TxIndex][completionSignal.CompletionAccessOperation] = append(completionSignalingMap[node.TxIndex][completionSignal.CompletionAccessOperation], completionSignal)
				}

			}
		}
	}
	return
}

type CompletionSignal struct {
	FromNodeID                DagNodeID
	ToNodeID                  DagNodeID
	CompletionAccessOperation acltypes.AccessOperation // this is the access operation that must complete in order to send the signal
	BlockedAccessOperation    acltypes.AccessOperation // this is the access operation that is blocked by the completion access operation
}

var ErrCycleInDAG = fmt.Errorf("cycle detected in DAG")
