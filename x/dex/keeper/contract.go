package keeper

import (
	"errors"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	appparams "github.com/sei-protocol/sei-chain/app/params"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

const ContractPrefixKey = "x-wasm-contract"

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
	gasDec := sdk.NewDec(int64(rentBalance)).Quo(gasPrice)
	return gasDec.TruncateInt().Uint64(), nil // round down
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
func (k Keeper) ChargeRentForGas(ctx sdk.Context, contractAddr string, gasUsed uint64, userProvidedGas uint64) error {
	if gasUsed <= userProvidedGas {
		// User provided can fully cover the consumed gas. Doing nothing
		return nil
	}
	gasUsed -= userProvidedGas
	contract, err := k.GetContract(ctx, contractAddr)
	if err != nil {
		return err
	}
	params := k.GetParams(ctx)
	gasPrice := sdk.NewDec(int64(gasUsed)).Mul(params.SudoCallGasPrice).RoundInt().Int64()
	if gasPrice > int64(contract.RentBalance) {
		contract.RentBalance = 0
		if err := k.SetContract(ctx, &contract); err != nil {
			return err
		}
		return errors.New("insufficient rent")
	}
	contract.RentBalance -= uint64(gasPrice)
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

func (k Keeper) DoUnregisterContract(ctx sdk.Context, contract types.ContractInfoV2) error {
	k.DeleteContract(ctx, contract.ContractAddr)
	k.RemoveAllLongBooksForContract(ctx, contract.ContractAddr)
	k.RemoveAllShortBooksForContract(ctx, contract.ContractAddr)
	k.RemoveAllPricesForContract(ctx, contract.ContractAddr)
	k.DeleteMatchResultState(ctx, contract.ContractAddr)
	k.DeleteNextOrderID(ctx, contract.ContractAddr)
	k.DeleteAllRegisteredPairsForContract(ctx, contract.ContractAddr)
	k.RemoveAllTriggeredOrders(ctx, contract.ContractAddr)
	creatorAddr, _ := sdk.AccAddressFromBech32(contract.Creator)
	if err := k.BankKeeper.SendCoins(ctx, k.AccountKeeper.GetModuleAddress(types.ModuleName), creatorAddr, sdk.NewCoins(sdk.NewCoin(appparams.BaseCoinUnit, sdk.NewInt(int64(contract.RentBalance))))); err != nil {
		return err
	}
	return nil
}
