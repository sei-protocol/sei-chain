package app

import (
	"fmt"
	"time"

	acltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	mapset "github.com/deckarep/golang-set"
	"github.com/sei-protocol/sei-chain/utils/metrics"
)

type DagNodeID int

// Alias for mapping resource identifier to dag node IDs
type ResourceIdentifierNodeIDMapping = map[string][]DagNodeID

type ResourceAccess struct {
	ResourceType acltypes.ResourceType
	AccessType   acltypes.AccessType
}

type DagNode struct {
	NodeID          DagNodeID
	MessageIndex    int
	TxIndex         int
	AccessOperation acltypes.AccessOperation
}

type DagEdge struct {
	FromNodeID DagNodeID
	ToNodeID   DagNodeID
}

type Dag struct {
	NodeMap           map[DagNodeID]DagNode
	EdgesMap          map[DagNodeID][]DagEdge                            // maps node Id (from node) and contains edge info
	ResourceAccessMap map[ResourceAccess]ResourceIdentifierNodeIDMapping // maps resource type and access type to identifiers + node IDs
	TxIndexMap        map[int]DagNodeID                                  // tracks latest node ID for a tx index
	NextID            DagNodeID
}

// Alias for mapping MessageIndexId -> AccessOperations -> CompletionSignals
type MessageCompletionSignalMapping = map[int]map[acltypes.AccessOperation][]CompletionSignal

type CompletionSignal struct {
	FromNodeID                DagNodeID
	ToNodeID                  DagNodeID
	CompletionAccessOperation acltypes.AccessOperation // this is the access operation that must complete in order to send the signal
	BlockedAccessOperation    acltypes.AccessOperation // this is the access operation that is blocked by the completion access operation
	Channel                   chan interface{}
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
		// channel used for signalling
		Channel: make(chan interface{}),
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
		NodeMap:           make(map[DagNodeID]DagNode),
		EdgesMap:          make(map[DagNodeID][]DagEdge),
		ResourceAccessMap: make(map[ResourceAccess]ResourceIdentifierNodeIDMapping),
		TxIndexMap:        make(map[int]DagNodeID),
		NextID:            0,
	}
}

func GetResourceAccess(accessOp acltypes.AccessOperation) ResourceAccess {
	return ResourceAccess{
		accessOp.ResourceType,
		accessOp.AccessType,
	}
}

func (dag *Dag) AddNode(messageIndex int, txIndex int, accessOp acltypes.AccessOperation) DagNode {
	dagNode := DagNode{
		NodeID:          dag.NextID,
		MessageIndex:    messageIndex,
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
func (dag *Dag) AddNodeBuildDependency(messageIndex int, txIndex int, accessOp acltypes.AccessOperation) {
	defer metrics.MeasureBuildDagDuration(time.Now(), "AddNodeBuildDependency")
	dagNode := dag.AddNode(messageIndex, txIndex, accessOp)
	// update tx index map
	dag.TxIndexMap[txIndex] = dagNode.NodeID

	nodeDependencies := dag.GetNodeDependencies(dagNode)
	// build edges for each of the dependencies
	for _, nodeDependency := range nodeDependencies {
		dag.AddEdge(nodeDependency, dagNode.NodeID)
	}

	// update access ops map with the latest node id using a specific access op
	resourceAccess := GetResourceAccess(accessOp)
	if _, exists := dag.ResourceAccessMap[resourceAccess]; !exists {
		dag.ResourceAccessMap[resourceAccess] = make(ResourceIdentifierNodeIDMapping)
	}
	dag.ResourceAccessMap[resourceAccess][accessOp.IdentifierTemplate] = append(dag.ResourceAccessMap[resourceAccess][accessOp.IdentifierTemplate], dagNode.NodeID)
}

func getAllNodeIDsFromIdentifierMapping(mapping ResourceIdentifierNodeIDMapping) (allNodeIDs []DagNodeID) {
	for _, nodeIDs := range mapping {
		allNodeIDs = append(allNodeIDs, nodeIDs...)
	}
	return
}

func (dag *Dag) getDependencyWrites(node DagNode, dependentResource acltypes.ResourceType) mapset.Set {
	nodeIDs := mapset.NewSet()
	writeResourceAccess := ResourceAccess{
		dependentResource,
		acltypes.AccessType_WRITE,
	}
	if identifierNodeMapping, ok := dag.ResourceAccessMap[writeResourceAccess]; ok {
		var nodeIDsMaybeDependency []DagNodeID
		if dependentResource != node.AccessOperation.ResourceType {
			// we can add all node IDs as dependencies if applicable
			nodeIDsMaybeDependency = getAllNodeIDsFromIdentifierMapping(identifierNodeMapping)
		} else {
			// TODO: otherwise we need to have partial filtering on identifiers
			// for now, lets just perform exact matching on identifiers
			nodeIDsMaybeDependency = identifierNodeMapping[node.AccessOperation.IdentifierTemplate]
		}
		for _, wn := range nodeIDsMaybeDependency {
			writeNode := dag.NodeMap[wn]
			// if accessOp exists already (and from a previous transaction), we need to define a dependency on the previous message (and make a edge between the two)
			// if from a previous transaction, we need to create an edge
			if writeNode.TxIndex < node.TxIndex {
				// this should be the COMMIT access op for the tx
				lastTxNode := dag.NodeMap[dag.TxIndexMap[writeNode.TxIndex]]
				nodeIDs.Add(lastTxNode.NodeID)
			}
		}
	}
	return nodeIDs
}

func (dag *Dag) getDependencyUnknowns(node DagNode, dependentResource acltypes.ResourceType) mapset.Set {
	nodeIDs := mapset.NewSet()
	unknownResourceAccess := ResourceAccess{
		dependentResource,
		acltypes.AccessType_UNKNOWN,
	}
	if identifierNodeMapping, ok := dag.ResourceAccessMap[unknownResourceAccess]; ok {
		var nodeIDsMaybeDependency []DagNodeID
		if dependentResource != node.AccessOperation.ResourceType {
			// we can add all node IDs as dependencies if applicable
			nodeIDsMaybeDependency = getAllNodeIDsFromIdentifierMapping(identifierNodeMapping)
		} else {
			// TODO: otherwise we need to have partial filtering on identifiers
			// for now, lets just perform exact matching on identifiers
			nodeIDsMaybeDependency = identifierNodeMapping[node.AccessOperation.IdentifierTemplate]
		}
		for _, un := range nodeIDsMaybeDependency {
			uNode := dag.NodeMap[un]
			// if accessOp exists already (and from a previous transaction), we need to define a dependency on the previous message (and make a edge between the two)
			// if from a previous transaction, we need to create an edge
			if uNode.TxIndex < node.TxIndex {
				// this should be the COMMIT access op for the tx
				lastTxNode := dag.NodeMap[dag.TxIndexMap[uNode.TxIndex]]
				nodeIDs.Add(lastTxNode.NodeID)
			}
		}
	}
	return nodeIDs
}

func (dag *Dag) getDependencyReads(node DagNode, dependentResource acltypes.ResourceType) mapset.Set {
	nodeIDs := mapset.NewSet()
	readResourceAccess := ResourceAccess{
		dependentResource,
		acltypes.AccessType_READ,
	}
	if identifierNodeMapping, ok := dag.ResourceAccessMap[readResourceAccess]; ok {
		var nodeIDsMaybeDependency []DagNodeID
		if dependentResource != node.AccessOperation.ResourceType {
			// we can add all node IDs as dependencies if applicable
			nodeIDsMaybeDependency = getAllNodeIDsFromIdentifierMapping(identifierNodeMapping)
		} else {
			// TODO: otherwise we need to have partial filtering on identifiers
			// for now, lets just perform exact matching on identifiers
			nodeIDsMaybeDependency = identifierNodeMapping[node.AccessOperation.IdentifierTemplate]
		}
		for _, rn := range nodeIDsMaybeDependency {
			readNode := dag.NodeMap[rn]
			// if from a previous transaction, we need to create an edge
			if readNode.TxIndex < node.TxIndex {
				nodeIDs.Add(readNode.NodeID)
			}
		}
	}
	return nodeIDs
}

// given a node, and a dependent Resource, generate a set of nodes that are dependencies
func (dag *Dag) getNodeDependenciesForResource(node DagNode, dependentResource acltypes.ResourceType) mapset.Set {
	nodeIDs := mapset.NewSet()
	switch node.AccessOperation.AccessType {
	case acltypes.AccessType_READ:
		// for a read, we are blocked on prior writes and unknown
		nodeIDs = nodeIDs.Union(dag.getDependencyWrites(node, dependentResource))
		nodeIDs = nodeIDs.Union(dag.getDependencyUnknowns(node, dependentResource))
	case acltypes.AccessType_WRITE, acltypes.AccessType_UNKNOWN:
		// for write / unknown, we're blocked on prior writes, reads, and unknowns
		nodeIDs = nodeIDs.Union(dag.getDependencyWrites(node, dependentResource))
		nodeIDs = nodeIDs.Union(dag.getDependencyUnknowns(node, dependentResource))
		nodeIDs = nodeIDs.Union(dag.getDependencyReads(node, dependentResource))
	}
	return nodeIDs
}

// This helper will identify nodes that are dependencies for the current node, and can then be used for creating edges between then for future completion signals
func (dag *Dag) GetNodeDependencies(node DagNode) []DagNodeID {
	accessOp := node.AccessOperation
	// get all parent resource types, we'll need to create edges for any of these
	parentResources := accessOp.ResourceType.GetResourceDependencies()
	nodeIDSet := mapset.NewSet()
	for _, resource := range parentResources {
		nodeIDSet = nodeIDSet.Union(dag.getNodeDependenciesForResource(node, resource))
	}
	nodeDependencies := make([]DagNodeID, nodeIDSet.Cardinality())
	for i, x := range nodeIDSet.ToSlice() {
		nodeDependencies[i] = x.(DagNodeID)
	}
	return nodeDependencies
}

// returns completion signaling map and blocking signals map
func (dag *Dag) BuildCompletionSignalMaps() (
	completionSignalingMap map[int]MessageCompletionSignalMapping,
	blockingSignalsMap map[int]MessageCompletionSignalMapping,
) {
	defer metrics.MeasureBuildDagDuration(time.Now(), "BuildCompletionSignalMaps")
	completionSignalingMap = make(map[int]MessageCompletionSignalMapping)
	blockingSignalsMap = make(map[int]MessageCompletionSignalMapping)
	// go through every node
	for _, node := range dag.NodeMap {
		// for each node, assign its completion signaling, and also assign blocking signals for the destination nodes
		if outgoingEdges, ok := dag.EdgesMap[node.NodeID]; ok {
			for _, edge := range outgoingEdges {
				maybeCompletionSignal := dag.GetCompletionSignal(edge)
				if maybeCompletionSignal != nil {
					completionSignal := *maybeCompletionSignal

					toNode := dag.NodeMap[edge.ToNodeID]
					if _, exists := blockingSignalsMap[toNode.TxIndex]; !exists {
						blockingSignalsMap[toNode.TxIndex] = make(MessageCompletionSignalMapping)
					}
					if _, exists := blockingSignalsMap[toNode.TxIndex][toNode.MessageIndex]; !exists {
						blockingSignalsMap[toNode.TxIndex][toNode.MessageIndex] = make(map[acltypes.AccessOperation][]CompletionSignal)
					}
					// add it to the right blocking signal in the right txindex
					prevBlockSignalMapping := blockingSignalsMap[toNode.TxIndex][toNode.MessageIndex][completionSignal.BlockedAccessOperation]
					blockingSignalsMap[toNode.TxIndex][toNode.MessageIndex][completionSignal.BlockedAccessOperation] = append(prevBlockSignalMapping, completionSignal)

					if _, exists := completionSignalingMap[node.TxIndex]; !exists {
						completionSignalingMap[node.TxIndex] = make(MessageCompletionSignalMapping)
					}
					if _, exists := completionSignalingMap[node.TxIndex][node.MessageIndex]; !exists {
						completionSignalingMap[node.TxIndex][node.MessageIndex] = make(map[acltypes.AccessOperation][]CompletionSignal)
					}
					// add it to the completion signal for the tx index
					prevCompletionSignalMapping := completionSignalingMap[node.TxIndex][node.MessageIndex][completionSignal.CompletionAccessOperation]
					completionSignalingMap[node.TxIndex][node.MessageIndex][completionSignal.CompletionAccessOperation] = append(prevCompletionSignalMapping, completionSignal)
				}

			}
		}
	}
	return completionSignalingMap, blockingSignalsMap
}

var ErrCycleInDAG = fmt.Errorf("cycle detected in DAG")
