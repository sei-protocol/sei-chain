package msgserver

import (
	"context"
	"errors"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	appparams "github.com/sei-protocol/sei-chain/app/params"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/utils/datastructures"
	"github.com/sei-protocol/sei-chain/x/dex/contract"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	dexutils "github.com/sei-protocol/sei-chain/x/dex/utils"
)

func (k msgServer) RegisterContract(goCtx context.Context, msg *types.MsgRegisterContract) (*types.MsgRegisterContractResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	if err := msg.ValidateBasic(); err != nil {
		ctx.Logger().Error(fmt.Sprintf("request invalid: %s", err))
		return nil, err
	}

	// Validation such that only the user who instantiated the contract can register contract
	contractAddr, _ := sdk.AccAddressFromBech32(msg.Contract.ContractAddr)
	contractInfo := k.Keeper.WasmKeeper.GetContractInfo(ctx, contractAddr)

	// TODO: Add wasm fixture to write unit tests to verify this behavior
	if contractInfo.Creator != msg.Creator {
		return nil, sdkerrors.ErrUnauthorized
	}

	if err := k.ValidateSuspension(ctx, msg.GetContract().GetContractAddr()); err != nil {
		ctx.Logger().Error("suspended contract")
		return &types.MsgRegisterContractResponse{}, err
	}
	if err := k.ValidateRentBalance(ctx, msg.GetContract().GetRentBalance()); err != nil {
		ctx.Logger().Error("invalid rent balance")
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
	if err := k.HandleDepositOrRefund(ctx, msg); err != nil {
		ctx.Logger().Error("failed to deposit/refund during contract registration")
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

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeRegisterContract,
		sdk.NewAttribute(types.AttributeKeyContractAddress, msg.Contract.ContractAddr),
	))

	dexutils.GetMemState(ctx.Context()).ClearContractToDependencies(ctx)
	return &types.MsgRegisterContractResponse{}, nil
}

func (k msgServer) ValidateUniqueDependencies(msg *types.MsgRegisterContract) error {
	if msg.Contract.Dependencies == nil {
		return nil
	}
	dependencySet := datastructures.NewSyncSet(utils.Map(
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
			if err == types.ErrContractNotExists {
				// old dependency doesn't exist. Do nothing.
				ctx.Logger().Info(fmt.Sprintf("existing contract %s old dependency %s does not exist", msg.Contract.ContractAddr, oldDependency.Dependency))
				continue
			}
			return err
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
		if err == types.ErrContractNotExists {
			return nil
		}
		return err
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

func (k msgServer) HandleDepositOrRefund(ctx sdk.Context, msg *types.MsgRegisterContract) error {
	creatorAddr, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return err
	}
	if existingContract, err := k.GetContract(ctx, msg.Contract.ContractAddr); err != nil {
		// brand new contract
		if msg.Contract.RentBalance > 0 {
			if err := k.BankKeeper.SendCoins(ctx, creatorAddr, k.AccountKeeper.GetModuleAddress(types.ModuleName), sdk.NewCoins(sdk.NewCoin(appparams.BaseCoinUnit, sdk.NewIntFromUint64(msg.Contract.RentBalance)))); err != nil {
				return err
			}
		}
	} else {
		if msg.Creator != existingContract.Creator {
			return sdkerrors.ErrUnauthorized
		}
		if msg.Contract.RentBalance < existingContract.RentBalance {
			// refund
			refundAmount := existingContract.RentBalance - msg.Contract.RentBalance
			if err := k.BankKeeper.SendCoins(ctx, k.AccountKeeper.GetModuleAddress(types.ModuleName), creatorAddr, sdk.NewCoins(sdk.NewCoin(appparams.BaseCoinUnit, sdk.NewIntFromUint64(refundAmount)))); err != nil {
				return err
			}
		} else if msg.Contract.RentBalance > existingContract.RentBalance {
			// deposit
			depositAmount := msg.Contract.RentBalance - existingContract.RentBalance
			if err := k.BankKeeper.SendCoins(ctx, creatorAddr, k.AccountKeeper.GetModuleAddress(types.ModuleName), sdk.NewCoins(sdk.NewCoin(appparams.BaseCoinUnit, sdk.NewIntFromUint64(depositAmount)))); err != nil {
				return err
			}
		}
	}
	return nil
}

func (k msgServer) SetNewContract(ctx sdk.Context, msg *types.MsgRegisterContract) ([]types.ContractInfoV2, error) {
	// set incoming paths for new contract
	newContract := msg.Contract
	newContract.Creator = msg.Creator
	newContract.NumIncomingDependencies = 0
	allContractInfo := k.GetAllContractInfo(ctx)
	for _, contractInfo := range allContractInfo {
		if contractInfo.Dependencies == nil {
			continue
		}
		dependencySet := datastructures.NewSyncSet(utils.Map(
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
			if contractInfo.ContractAddr == newContract.ContractAddr {
				continue
			}
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
					return []types.ContractInfoV2{}, err
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
		return []types.ContractInfoV2{}, err
	}
	allContractInfo = append(allContractInfo, *newContract)
	return allContractInfo, nil
}
