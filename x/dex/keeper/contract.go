package keeper

import (
	"errors"
	"math"
	"math/big"
	"time"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	appparams "github.com/sei-protocol/sei-chain/app/params"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

const ContractPrefixKey = "x-wasm-contract"
const ContractMaxSudoGas uint64 = 1000000

func (k Keeper) SetContract(ctx sdk.Context, contract *types.ContractInfoV2) error {
	store := prefix.NewStore(
		ctx.KVStore(k.storeKey),
		[]byte(ContractPrefixKey),
	)
	bz, err := contract.Marshal()
	if err != nil {
		return errors.New("failed to marshal contract info")
	}
	store.Set(types.ContractKey(contract.ContractAddr), bz)
	return nil
}

func (k Keeper) DeleteContract(ctx sdk.Context, contractAddr string) {
	store := prefix.NewStore(
		ctx.KVStore(k.storeKey),
		[]byte(ContractPrefixKey),
	)
	key := types.ContractKey(contractAddr)
	store.Delete(key)
}

func (k Keeper) GetContract(ctx sdk.Context, contractAddr string) (types.ContractInfoV2, error) {
	store := prefix.NewStore(
		ctx.KVStore(k.storeKey),
		[]byte(ContractPrefixKey),
	)
	key := types.ContractKey(contractAddr)
	res := types.ContractInfoV2{}
	if !store.Has(key) {
		return res, types.ErrContractNotExists
	}
	if err := res.Unmarshal(store.Get(key)); err != nil {
		return res, types.ErrParsingContractInfo
	}
	return res, nil
}

func (k Keeper) GetContractWithoutGasCharge(ctx sdk.Context, contractAddr string) (types.ContractInfoV2, error) {
	return k.GetContract(ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx)), contractAddr)
}

func (k Keeper) GetContractGasLimit(ctx sdk.Context, contractAddr sdk.AccAddress) (uint64, error) {
	bech32ContractAddr := contractAddr.String()
	contract, err := k.GetContract(ctx, bech32ContractAddr)
	if err != nil {
		return 0, err
	}
	rentBalance := contract.RentBalance
	gasPrice := k.GetParams(ctx).SudoCallGasPrice
	if gasPrice.LTE(sdk.ZeroDec()) {
		return 0, errors.New("invalid gas price: must be positive")
	}
	gasDec := sdk.NewDecFromBigInt(new(big.Int).SetUint64(rentBalance)).Quo(gasPrice)
	if gasDec.GT(sdk.NewDecFromBigInt(new(big.Int).SetUint64(math.MaxUint64))) {
		return ContractMaxSudoGas, nil
	}
	gasLimit := gasDec.TruncateInt().Uint64()
	if gasLimit > ContractMaxSudoGas {
		// prevent excessive gas expenditure in a single contract call
		return ContractMaxSudoGas, nil
	}
	return gasLimit, nil
}

func (k Keeper) GetAllContractInfo(ctx sdk.Context) []types.ContractInfoV2 {
	store := prefix.NewStore(ctx.KVStore(k.storeKey), []byte(ContractPrefixKey))
	iterator := sdk.KVStorePrefixIterator(store, []byte{})

	defer iterator.Close()

	list := []types.ContractInfoV2{}
	for ; iterator.Valid(); iterator.Next() {
		contract := types.ContractInfoV2{}
		if err := contract.Unmarshal(iterator.Value()); err == nil {
			list = append(list, contract)
		}
	}

	return list
}

// Reduce `RentBalance` of a contract if `userProvidedGas` cannot cover `gasUsed`
func (k Keeper) ChargeRentForGas(ctx sdk.Context, contractAddr string, gasUsed uint64, gasAllowance uint64) error {
	if gasUsed <= gasAllowance {
		// Allowance can fully cover the consumed gas. Doing nothing
		return nil
	}
	gasUsed -= gasAllowance
	contract, err := k.GetContract(ctx, contractAddr)
	if err != nil {
		return err
	}
	params := k.GetParams(ctx)
	gasFeeDec := sdk.NewDecFromBigInt(new(big.Int).SetUint64(gasUsed)).Mul(params.SudoCallGasPrice)
	if gasFeeDec.GT(sdk.NewDecFromBigInt(new(big.Int).SetUint64(math.MaxUint64))) {
		gasFeeDec = sdk.NewDecFromBigInt(new(big.Int).SetUint64(math.MaxUint64))
	}
	gasFee := gasFeeDec.RoundInt().Uint64()
	if gasFee > contract.RentBalance {
		contract.RentBalance = 0
		if err := k.SetContract(ctx, &contract); err != nil {
			return err
		}
		return types.ErrInsufficientRent
	}
	contract.RentBalance -= gasFee
	return k.SetContract(ctx, &contract)
}

func (k Keeper) GetRentsForContracts(ctx sdk.Context, contractAddrs []string) map[string]uint64 {
	res := map[string]uint64{}
	for _, contractAddr := range contractAddrs {
		if contract, err := k.GetContract(ctx, contractAddr); err == nil {
			res[contractAddr] = contract.RentBalance
		}
	}
	return res
}

// Unregistrate and refund the creator
func (k Keeper) DoUnregisterContractWithRefund(ctx sdk.Context, contract types.ContractInfoV2) error {
	k.DoUnregisterContract(ctx, contract)
	creatorAddr, _ := sdk.AccAddressFromBech32(contract.Creator)
	return k.BankKeeper.SendCoins(ctx, k.AccountKeeper.GetModuleAddress(types.ModuleName), creatorAddr, sdk.NewCoins(sdk.NewCoin(appparams.BaseCoinUnit, sdk.NewIntFromBigInt(new(big.Int).SetUint64(contract.RentBalance)))))
}

// Contract unregistration will remove all orderbook data stored for the contract
func (k Keeper) DoUnregisterContract(ctx sdk.Context, contract types.ContractInfoV2) {
	k.DeleteContract(ctx, contract.ContractAddr)
	k.ClearDependenciesForContract(ctx, contract)
	k.RemoveAllLongBooksForContract(ctx, contract.ContractAddr)
	k.RemoveAllShortBooksForContract(ctx, contract.ContractAddr)
	k.RemoveAllPricesForContract(ctx, contract.ContractAddr)
	k.DeleteMatchResultState(ctx, contract.ContractAddr)
	k.DeleteNextOrderID(ctx, contract.ContractAddr)
	k.DeleteAllRegisteredPairsForContract(ctx, contract.ContractAddr)
}

func (k Keeper) SuspendContract(ctx sdk.Context, contractAddress string, reason string) error {
	contract, err := k.GetContract(ctx, contractAddress)
	if err != nil {
		return err
	}
	contract.Suspended = true
	contract.SuspensionReason = reason
	return k.SetContract(ctx, &contract)
}

func (k Keeper) ClearDependenciesForContract(ctx sdk.Context, removedContract types.ContractInfoV2) {
	// handle upstreams
	allContracts := k.GetAllContractInfo(ctx)
	for _, c := range allContracts {
		contract := c
		dependsOnRemovedContract := false
		for _, dep := range contract.Dependencies {
			if dep.Dependency == removedContract.ContractAddr {
				dependsOnRemovedContract = true
				break
			}
		}
		if !dependsOnRemovedContract {
			continue
		}
		contract.Dependencies = utils.Filter(contract.Dependencies, func(dep *types.ContractDependencyInfo) bool { return dep.Dependency != removedContract.ContractAddr })
		_ = k.SetContract(ctx, &contract)
	}

	// handle downstreams
	allContractsMap := map[string]types.ContractInfoV2{}
	for _, contract := range allContracts {
		allContractsMap[contract.ContractAddr] = contract
	}
	for _, dep := range removedContract.Dependencies {
		if dependedContract, ok := allContractsMap[dep.Dependency]; ok {
			dependedContract.NumIncomingDependencies--
			_ = k.SetContract(ctx, &dependedContract)
		}

		if dep.ImmediateElderSibling != "" {
			if immediateElderSibling, ok := allContractsMap[dep.ImmediateElderSibling]; ok {
				newDependencies := []*types.ContractDependencyInfo{}
				for _, elderSiblingDep := range immediateElderSibling.Dependencies {
					if elderSiblingDep.Dependency != dep.Dependency {
						newDependencies = append(newDependencies, elderSiblingDep)
					} else {
						newDependencies = append(newDependencies, &types.ContractDependencyInfo{
							Dependency:              elderSiblingDep.Dependency,
							ImmediateElderSibling:   elderSiblingDep.ImmediateElderSibling,
							ImmediateYoungerSibling: dep.ImmediateYoungerSibling,
						})
					}
				}
				immediateElderSibling.Dependencies = newDependencies
				_ = k.SetContract(ctx, &immediateElderSibling)
			}
		}

		if dep.ImmediateYoungerSibling != "" {
			if immediateYoungerSibling, ok := allContractsMap[dep.ImmediateYoungerSibling]; ok {
				newDependencies := []*types.ContractDependencyInfo{}
				for _, youngerSiblingDep := range immediateYoungerSibling.Dependencies {
					if youngerSiblingDep.Dependency != dep.Dependency {
						newDependencies = append(newDependencies, youngerSiblingDep)
					} else {
						newDependencies = append(newDependencies, &types.ContractDependencyInfo{
							Dependency:              youngerSiblingDep.Dependency,
							ImmediateElderSibling:   dep.ImmediateElderSibling,
							ImmediateYoungerSibling: youngerSiblingDep.ImmediateYoungerSibling,
						})
					}
				}
				immediateYoungerSibling.Dependencies = newDependencies
				_ = k.SetContract(ctx, &immediateYoungerSibling)
			}
		}
	}
}

func (k Keeper) GetAllProcessableContractInfo(ctx sdk.Context) []types.ContractInfoV2 {
	// Do not process any contract that has zero rent balance, suspended, or not require matching
	defer telemetry.MeasureSince(time.Now(), types.ModuleName, "get_all_contract_info")
	allRegisteredContracts := k.GetAllContractInfo(ctx)
	validContracts := utils.Filter(allRegisteredContracts, func(c types.ContractInfoV2) bool {
		return c.NeedOrderMatching && !c.Suspended && c.RentBalance > k.GetMinProcessableRent(ctx)
	})
	telemetry.SetGauge(float32(len(allRegisteredContracts)), types.ModuleName, "num_of_registered_contracts")
	telemetry.SetGauge(float32(len(validContracts)), types.ModuleName, "num_of_valid_contracts")
	telemetry.SetGauge(float32(len(allRegisteredContracts)-len(validContracts)), types.ModuleName, "num_of_zero_balance_contracts")
	return validContracts
}
