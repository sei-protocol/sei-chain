package msgserver

import (
	"context"
	"errors"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/dex/contract"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func (k msgServer) RegisterContract(goCtx context.Context, msg *types.MsgRegisterContract) (*types.MsgRegisterContractResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	// TODO: add validation such that only the user who stored the code can register contract

	if err := k.ValidateBasics(msg); err != nil {
		ctx.Logger().Error(fmt.Sprintf("request invalid: %s", err))
		return &types.MsgRegisterContractResponse{}, err
	}
	if err := k.ValidateUniqueDependencies(msg); err != nil {
		ctx.Logger().Error(fmt.Sprintf("dependencies of contract %s are not unique", msg.Contract.ContractAddr))
		return &types.MsgRegisterContractResponse{}, err
	}
	if err := k.RemoveExistingDependencies(ctx, msg); err != nil {
		ctx.Logger().Error("failed to remove existing dependencies")
		return &types.MsgRegisterContractResponse{}, err
	}
	if err := k.UpdateOldSiblings(ctx, msg); err != nil {
		ctx.Logger().Error("failed to update old siblings")
		return &types.MsgRegisterContractResponse{}, err
	}
	if err := k.UpdateNewDependencies(ctx, msg); err != nil {
		ctx.Logger().Error("failed to update new dependencies")
		return &types.MsgRegisterContractResponse{}, err
	}
	allContractInfo, err := k.SetNewContract(ctx, msg)
	if err != nil {
		ctx.Logger().Error("failed to set new contract")
		return &types.MsgRegisterContractResponse{}, err
	}
	if _, err := contract.TopologicalSortContractInfo(allContractInfo); err != nil {
		ctx.Logger().Error("contract caused a circular dependency")
		return &types.MsgRegisterContractResponse{}, err
	}

	return &types.MsgRegisterContractResponse{}, nil
}

func (k msgServer) ValidateBasics(msg *types.MsgRegisterContract) error {
	if msg.Contract == nil {
		return errors.New("empty contract info")
	}
	if msg.Contract.ContractAddr == "" {
		return errors.New("contract address is empty")
	}
	return nil
}

func (k msgServer) ValidateUniqueDependencies(msg *types.MsgRegisterContract) error {
	if msg.Contract.Dependencies == nil {
		return nil
	}
	dependencySet := utils.NewStringSet(utils.Map(
		msg.Contract.Dependencies, func(c *types.ContractDependencyInfo) string { return c.Dependency },
	))
	if dependencySet.Size() < len(msg.Contract.Dependencies) {
		return errors.New("duplicated contract dependencies")
	}
	return nil
}

func (k msgServer) RemoveExistingDependencies(ctx sdk.Context, msg *types.MsgRegisterContract) error {
	contractInfo, err := k.GetContract(ctx, msg.Contract.ContractAddr)
	if err != nil {
		// contract is being added for the first time
		ctx.Logger().Info(fmt.Sprintf("adding contract %s for the first time", msg.Contract.ContractAddr))
		return nil
	}
	if contractInfo.Dependencies == nil {
		ctx.Logger().Info(fmt.Sprintf("existing contract %s has no dependency", msg.Contract.ContractAddr))
		return nil
	}
	// update old dependency's NumIncomingPaths
	for _, oldDependency := range contractInfo.Dependencies {
		dependencyInfo, err := k.GetContract(ctx, oldDependency.Dependency)
		if err != nil {
			// old dependency doesn't exist. Do nothing.
			ctx.Logger().Info(fmt.Sprintf("existing contract %s old dependency %s does not exist", msg.Contract.ContractAddr, oldDependency.Dependency))
			continue
		}
		dependencyInfo.NumIncomingDependencies--
		if err := k.SetContract(ctx, &dependencyInfo); err != nil {
			return err
		}
	}
	return nil
}

func (k msgServer) UpdateOldSiblings(ctx sdk.Context, msg *types.MsgRegisterContract) error {
	contractInfo, err := k.GetContract(ctx, msg.Contract.ContractAddr)
	if err != nil {
		return nil
	}
	// update siblings for old dependencies
	for _, oldDependency := range contractInfo.Dependencies {
		elder := oldDependency.ImmediateElderSibling
		younger := oldDependency.ImmediateYoungerSibling
		if younger != "" {
			ctx.Logger().Info(fmt.Sprintf("update younger sibling %s of %s for dependency %s", younger, msg.Contract.ContractAddr, oldDependency.Dependency))
			youngContract, err := k.GetContract(ctx, younger)
			if err != nil {
				return err
			}
			for _, youngDependency := range youngContract.Dependencies {
				if youngDependency.Dependency != oldDependency.Dependency {
					continue
				}
				youngDependency.ImmediateElderSibling = elder
				if err := k.SetContract(ctx, &youngContract); err != nil {
					return err
				}
				break
			}
		}
		if elder != "" {
			ctx.Logger().Info(fmt.Sprintf("update elder sibling %s of %s for dependency %s", elder, msg.Contract.ContractAddr, oldDependency.Dependency))
			elderContract, err := k.GetContract(ctx, elder)
			if err != nil {
				return err
			}
			for _, elderDependency := range elderContract.Dependencies {
				if elderDependency.Dependency != oldDependency.Dependency {
					continue
				}
				elderDependency.ImmediateYoungerSibling = younger
				if err := k.SetContract(ctx, &elderContract); err != nil {
					return err
				}
				break
			}
		}
	}
	return nil
}

func (k msgServer) UpdateNewDependencies(ctx sdk.Context, msg *types.MsgRegisterContract) error {
	if msg.Contract.Dependencies == nil {
		return nil
	}

	for _, dependency := range msg.Contract.Dependencies {
		contractInfo, err := k.GetContract(ctx, dependency.Dependency)
		if err != nil {
			// validate that all dependency contracts exist
			return err
		}
		// update incoming paths for dependency contracts
		contractInfo.NumIncomingDependencies++
		if err := k.SetContract(ctx, &contractInfo); err != nil {
			return err
		}
	}
	return nil
}

func (k msgServer) SetNewContract(ctx sdk.Context, msg *types.MsgRegisterContract) ([]types.ContractInfo, error) {
	// set incoming paths for new contract
	newContract := msg.Contract
	newContract.NumIncomingDependencies = 0
	allContractInfo := k.GetAllContractInfo(ctx)
	for _, contractInfo := range allContractInfo {
		if contractInfo.Dependencies == nil {
			continue
		}
		dependencySet := utils.NewStringSet(utils.Map(
			contractInfo.Dependencies, func(c *types.ContractDependencyInfo) string { return c.Dependency },
		))
		if dependencySet.Contains(msg.Contract.ContractAddr) {
			newContract.NumIncomingDependencies++
		}
	}

	// set immediate siblings
	for _, dependency := range newContract.Dependencies {
		// a newly added/updated contract is always the youngest among its siblings
		dependency.ImmediateYoungerSibling = ""
		found := false
		for _, contractInfo := range allContractInfo {
			for _, otherDependency := range contractInfo.Dependencies {
				if otherDependency.ImmediateYoungerSibling != "" {
					continue
				}
				if otherDependency.Dependency != dependency.Dependency {
					continue
				}
				dependency.ImmediateElderSibling = contractInfo.ContractAddr
				otherDependency.ImmediateYoungerSibling = newContract.ContractAddr
				contractInfo := contractInfo
				if err := k.SetContract(ctx, &contractInfo); err != nil {
					return []types.ContractInfo{}, err
				}
				found = true
				break
			}
			if found {
				break
			}
		}
		if !found {
			dependency.ImmediateElderSibling = ""
		}
	}

	// always override contract info so that it can be updated
	if err := k.SetContract(ctx, newContract); err != nil {
		return []types.ContractInfo{}, err
	}
	allContractInfo = append(allContractInfo, *newContract)
	return allContractInfo, nil
}
