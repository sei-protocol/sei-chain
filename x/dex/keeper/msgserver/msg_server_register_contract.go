package msgserver

import (
	"context"
	"errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/dex/contract"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func (k msgServer) RegisterContract(goCtx context.Context, msg *types.MsgRegisterContract) (*types.MsgRegisterContractResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	// TODO: add validation such that only the user who stored the code can register contract

	if err := k.validateUniqueDependencies(msg); err != nil {
		return &types.MsgRegisterContractResponse{}, err
	}
	if err := k.removeExistingDependencies(ctx, msg); err != nil {
		return &types.MsgRegisterContractResponse{}, err
	}
	if err := k.updateNewDependencies(ctx, msg); err != nil {
		return &types.MsgRegisterContractResponse{}, err
	}
	allContractInfo, err := k.setNewContract(ctx, msg)
	if err != nil {
		return &types.MsgRegisterContractResponse{}, err
	}
	if _, err := contract.TopologicalSortContractInfo(allContractInfo); err != nil {
		return &types.MsgRegisterContractResponse{}, err
	}

	return &types.MsgRegisterContractResponse{}, nil
}

func (k msgServer) validateUniqueDependencies(msg *types.MsgRegisterContract) error {
	if msg.Contract.DependentContractAddrs == nil {
		return nil
	}
	dependencySet := utils.NewStringSet(msg.Contract.DependentContractAddrs)
	if dependencySet.Size() < len(msg.Contract.DependentContractAddrs) {
		return errors.New("duplicated contract dependencies")
	}
	return nil
}

func (k msgServer) removeExistingDependencies(ctx sdk.Context, msg *types.MsgRegisterContract) error {
	contractInfo, err := k.GetContract(ctx, msg.Contract.ContractAddr)
	if err != nil {
		// contract is being added for the first time
		return nil
	}
	if contractInfo.DependentContractAddrs == nil {
		return nil
	}
	for _, oldDependency := range contractInfo.DependentContractAddrs {
		dependencyInfo, err := k.GetContract(ctx, oldDependency)
		if err != nil {
			// old dependency doesn't exist. Do nothing.
			continue
		}
		dependencyInfo.NumIncomingPaths--
		if err := k.SetContract(ctx, &dependencyInfo); err != nil {
			return err
		}
	}
	return nil
}

func (k msgServer) updateNewDependencies(ctx sdk.Context, msg *types.MsgRegisterContract) error {
	if msg.Contract.DependentContractAddrs == nil {
		return nil
	}

	for _, contractAddr := range msg.Contract.DependentContractAddrs {
		contractInfo, err := k.GetContract(ctx, contractAddr)
		if err != nil {
			// validate that all dependency contracts exist
			return err
		}
		// update incoming paths for dependency contracts
		contractInfo.NumIncomingPaths++
		if err := k.SetContract(ctx, &contractInfo); err != nil {
			return err
		}
	}
	return nil
}

func (k msgServer) setNewContract(ctx sdk.Context, msg *types.MsgRegisterContract) ([]types.ContractInfo, error) {
	// set incoming paths for new contract
	newContract := msg.Contract
	newContract.NumIncomingPaths = 0
	allContractInfo := k.GetAllContractInfo(ctx)
	for _, contractInfo := range allContractInfo {
		if contractInfo.DependentContractAddrs == nil {
			continue
		}
		dependencies := utils.NewStringSet(contractInfo.DependentContractAddrs)
		if dependencies.Contains(msg.Contract.ContractAddr) {
			newContract.NumIncomingPaths++
		}
	}

	// always override contract info so that it can be updated
	if err := k.SetContract(ctx, newContract); err != nil {
		return []types.ContractInfo{}, err
	}
	allContractInfo = append(allContractInfo, *newContract)
	return allContractInfo, nil
}
