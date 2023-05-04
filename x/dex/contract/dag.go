package contract

import (
	"github.com/sei-protocol/sei-chain/utils/datastructures"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

type node struct {
	contractAddr  string
	incomingNodes *datastructures.SyncSet[string]
}

// Kahn's algorithm
func TopologicalSortContractInfo(contracts []types.ContractInfoV2) ([]types.ContractInfoV2, error) {
	contractAddrToContractInfo := map[string]types.ContractInfoV2{}
	for _, contract := range contracts {
		contractAddrToContractInfo[contract.ContractAddr] = contract
	}

	res := []types.ContractInfoV2{}
	nodes := initNodes(contracts)
	frontierNodes, nonFrontierNodes := splitNodesByFrontier(nodes)
	for len(frontierNodes) > 0 {
		for _, frontierNode := range frontierNodes {
			if contract, ok := contractAddrToContractInfo[frontierNode.contractAddr]; ok {
				res = append(res, contract)
			}
			for _, nonFrontierNode := range nonFrontierNodes {
				nonFrontierNode.incomingNodes.Remove(frontierNode.contractAddr)
			}
		}
		frontierNodes, nonFrontierNodes = splitNodesByFrontier(nonFrontierNodes)
	}
	if len(nonFrontierNodes) > 0 {
		return []types.ContractInfoV2{}, types.ErrCircularContractDependency
	}
	return res, nil
}

func initNodes(contracts []types.ContractInfoV2) map[string]node {
	res := map[string]node{}
	for _, contract := range contracts {
		if _, ok := res[contract.ContractAddr]; !ok {
			emptyIncomingNodes := datastructures.NewSyncSet([]string{})
			res[contract.ContractAddr] = node{
				contractAddr:  contract.ContractAddr,
				incomingNodes: &emptyIncomingNodes,
			}
		}
		if contract.Dependencies == nil {
			continue
		}
		for _, dependentContract := range contract.Dependencies {
			dependentAddr := dependentContract.Dependency
			if _, ok := res[dependentAddr]; !ok {
				emptyIncomingNodes := datastructures.NewSyncSet([]string{})
				res[dependentAddr] = node{
					contractAddr:  dependentAddr,
					incomingNodes: &emptyIncomingNodes,
				}
			}
			res[dependentAddr].incomingNodes.Add(contract.ContractAddr)
		}
	}
	return res
}

func splitNodesByFrontier(nodes map[string]node) (map[string]node, map[string]node) {
	frontierNodes, nonFrontierNodes := map[string]node{}, map[string]node{}
	for contractAddr, node := range nodes {
		if node.incomingNodes.Size() == 0 {
			frontierNodes[contractAddr] = node
		} else {
			nonFrontierNodes[contractAddr] = node
		}
	}
	return frontierNodes, nonFrontierNodes
}
