package contract

import (
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

type node struct {
	contractAddr  string
	incomingNodes *utils.StringSet
}

// Kahn's algorithm
func TopologicalSortContractInfo(contracts []types.ContractInfo) ([]types.ContractInfo, error) {
	contractAddrToContractInfo := map[string]types.ContractInfo{}
	for _, contract := range contracts {
		contractAddrToContractInfo[contract.ContractAddr] = contract
	}

	res := []types.ContractInfo{}
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
		return []types.ContractInfo{}, types.ErrCircularContractDependency
	}
	return res, nil
}

func initNodes(contracts []types.ContractInfo) map[string]node {
	res := map[string]node{}
	for _, contract := range contracts {
		if _, ok := res[contract.ContractAddr]; !ok {
			emptyIncomingNodes := utils.NewStringSet([]string{})
			res[contract.ContractAddr] = node{
				contractAddr:  contract.ContractAddr,
				incomingNodes: &emptyIncomingNodes,
			}
		}
		if contract.DependentContractAddrs == nil {
			continue
		}
		for _, dependentContract := range contract.DependentContractAddrs {
			if _, ok := res[dependentContract]; !ok {
				emptyIncomingNodes := utils.NewStringSet([]string{})
				res[dependentContract] = node{
					contractAddr:  dependentContract,
					incomingNodes: &emptyIncomingNodes,
				}
			}
			res[dependentContract].incomingNodes.Add(contract.ContractAddr)
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
