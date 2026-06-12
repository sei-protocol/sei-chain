package bankquery

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/precompiles/bank"
	pquery "github.com/sei-protocol/sei-chain/precompiles/query"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkquery "github.com/sei-protocol/sei-chain/sei-cosmos/types/query"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	balanceMethod           = "/cosmos.bank.v1beta1.Query/Balance"
	allBalancesMethod       = "/cosmos.bank.v1beta1.Query/AllBalances"
	spendableBalancesMethod = "/cosmos.bank.v1beta1.Query/SpendableBalances"
	totalSupplyMethod       = "/cosmos.bank.v1beta1.Query/TotalSupply"
	supplyOfMethod          = "/cosmos.bank.v1beta1.Query/SupplyOf"
	paramsMethod            = "/cosmos.bank.v1beta1.Query/Params"
	denomMetadataMethod     = "/cosmos.bank.v1beta1.Query/DenomMetadata"
	denomsMetadataMethod    = "/cosmos.bank.v1beta1.Query/DenomsMetadata"
)

func Registry() pquery.Registry {
	abi := bank.GetABI()
	address := common.HexToAddress(bank.BankAddress)
	return pquery.NewRegistry(
		pquery.Bind(pquery.Binding[banktypes.QueryBalanceRequest, banktypes.QueryBalanceResponse]{
			FullMethod:    balanceMethod,
			Precompile:    address,
			ABI:           abi,
			ABIMethod:     bank.BalanceForAddressMethod,
			Pack:          packBalance,
			Unpack:        unpackBalance,
			ResponseShape: pquery.ExactProtobufShape,
		}),
		pquery.Bind(pquery.Binding[banktypes.QueryAllBalancesRequest, banktypes.QueryAllBalancesResponse]{
			FullMethod:    allBalancesMethod,
			Precompile:    address,
			ABI:           abi,
			ABIMethod:     bank.AllBalancesForAddressMethod,
			Pack:          packAllBalances,
			Unpack:        unpackAllBalances,
			ResponseShape: pquery.ExactProtobufShape,
		}),
		pquery.Bind(pquery.Binding[banktypes.QuerySpendableBalancesRequest, banktypes.QuerySpendableBalancesResponse]{
			FullMethod:    spendableBalancesMethod,
			Precompile:    address,
			ABI:           abi,
			ABIMethod:     bank.SpendableBalancesForAddressMethod,
			Pack:          packSpendableBalances,
			Unpack:        unpackSpendableBalances,
			ResponseShape: pquery.ExactProtobufShape,
		}),
		pquery.Bind(pquery.Binding[banktypes.QueryTotalSupplyRequest, banktypes.QueryTotalSupplyResponse]{
			FullMethod:    totalSupplyMethod,
			Precompile:    address,
			ABI:           abi,
			ABIMethod:     bank.TotalSupplyMethod,
			Pack:          pquery.PackNoArgs[banktypes.QueryTotalSupplyRequest],
			Unpack:        unpackTotalSupply,
			ResponseShape: pquery.ExactProtobufShape,
		}),
		pquery.Bind(pquery.Binding[banktypes.QuerySupplyOfRequest, banktypes.QuerySupplyOfResponse]{
			FullMethod:    supplyOfMethod,
			Precompile:    address,
			ABI:           abi,
			ABIMethod:     bank.SupplyMethod,
			Pack:          packSupplyOf,
			Unpack:        unpackSupplyOf,
			ResponseShape: pquery.ExactProtobufShape,
		}),
		pquery.Bind(pquery.Binding[banktypes.QueryParamsRequest, banktypes.QueryParamsResponse]{
			FullMethod:    paramsMethod,
			Precompile:    address,
			ABI:           abi,
			ABIMethod:     bank.ParamsMethod,
			Pack:          pquery.PackNoArgs[banktypes.QueryParamsRequest],
			Unpack:        unpackParams,
			ResponseShape: pquery.ExactProtobufShape,
		}),
		pquery.Bind(pquery.Binding[banktypes.QueryDenomMetadataRequest, banktypes.QueryDenomMetadataResponse]{
			FullMethod:    denomMetadataMethod,
			Precompile:    address,
			ABI:           abi,
			ABIMethod:     bank.DenomMetadataMethod,
			Pack:          packDenomMetadata,
			Unpack:        unpackDenomMetadata,
			ResponseShape: pquery.ExactProtobufShape,
		}),
		pquery.Bind(pquery.Binding[banktypes.QueryDenomsMetadataRequest, banktypes.QueryDenomsMetadataResponse]{
			FullMethod:    denomsMetadataMethod,
			Precompile:    address,
			ABI:           abi,
			ABIMethod:     bank.DenomsMetadataMethod,
			Pack:          pquery.PackNoArgs[banktypes.QueryDenomsMetadataRequest],
			Unpack:        unpackDenomsMetadata,
			ResponseShape: pquery.ExactProtobufShape,
		}),
	)
}

func packBalance(_ context.Context, _ *pquery.Env, req *banktypes.QueryBalanceRequest) ([]interface{}, error) {
	if req.Address == "" {
		return nil, status.Error(codes.InvalidArgument, "address cannot be empty")
	}
	if req.Denom == "" {
		return nil, status.Error(codes.InvalidArgument, "invalid denom")
	}
	if _, err := sdk.AccAddressFromBech32(req.Address); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return []interface{}{req.Address, req.Denom}, nil
}

func unpackBalance(_ context.Context, _ *pquery.Env, req *banktypes.QueryBalanceRequest, out []interface{}, resp *banktypes.QueryBalanceResponse) error {
	amount, err := pquery.SingleBigInt(out)
	if err != nil {
		return err
	}
	coin := sdk.NewCoin(req.Denom, sdk.NewIntFromBigInt(amount))
	resp.Balance = &coin
	return nil
}

func packAllBalances(_ context.Context, _ *pquery.Env, req *banktypes.QueryAllBalancesRequest) ([]interface{}, error) {
	if req.Address == "" {
		return nil, status.Error(codes.InvalidArgument, "address cannot be empty")
	}
	if _, err := sdk.AccAddressFromBech32(req.Address); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return []interface{}{req.Address}, nil
}

func unpackAllBalances(_ context.Context, _ *pquery.Env, req *banktypes.QueryAllBalancesRequest, out []interface{}, resp *banktypes.QueryAllBalancesResponse) error {
	coins, err := pquery.CoinsFromOutput(out)
	if err != nil {
		return err
	}
	paged, pageRes, err := pquery.PaginateCoins(coins, req.Pagination)
	if err != nil {
		return err
	}
	resp.Balances = paged
	resp.Pagination = pageRes
	return nil
}

func packSpendableBalances(_ context.Context, _ *pquery.Env, req *banktypes.QuerySpendableBalancesRequest) ([]interface{}, error) {
	if req.Address == "" {
		return nil, status.Error(codes.InvalidArgument, "address cannot be empty")
	}
	if _, err := sdk.AccAddressFromBech32(req.Address); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return []interface{}{req.Address}, nil
}

func unpackSpendableBalances(_ context.Context, _ *pquery.Env, req *banktypes.QuerySpendableBalancesRequest, out []interface{}, resp *banktypes.QuerySpendableBalancesResponse) error {
	coins, err := pquery.CoinsFromOutput(out)
	if err != nil {
		return err
	}
	paged, pageRes, err := pquery.PaginateCoins(coins, req.Pagination)
	if err != nil {
		return err
	}
	resp.Balances = paged
	resp.Pagination = pageRes
	return nil
}

func packSupplyOf(_ context.Context, _ *pquery.Env, req *banktypes.QuerySupplyOfRequest) ([]interface{}, error) {
	if req.Denom == "" {
		return nil, status.Error(codes.InvalidArgument, "invalid denom")
	}
	return []interface{}{req.Denom}, nil
}

func unpackSupplyOf(_ context.Context, _ *pquery.Env, req *banktypes.QuerySupplyOfRequest, out []interface{}, resp *banktypes.QuerySupplyOfResponse) error {
	amount, err := pquery.SingleBigInt(out)
	if err != nil {
		return err
	}
	resp.Amount = sdk.NewCoin(req.Denom, sdk.NewIntFromBigInt(amount))
	return nil
}

func unpackTotalSupply(_ context.Context, _ *pquery.Env, req *banktypes.QueryTotalSupplyRequest, out []interface{}, resp *banktypes.QueryTotalSupplyResponse) error {
	coins, err := pquery.CoinsFromOutput(out)
	if err != nil {
		return err
	}
	paged, pageRes, err := pquery.PaginateCoins(coins, req.Pagination)
	if err != nil {
		return err
	}
	resp.Supply = paged.Sort()
	resp.Pagination = pageRes
	return nil
}

func unpackParams(_ context.Context, _ *pquery.Env, _ *banktypes.QueryParamsRequest, out []interface{}, resp *banktypes.QueryParamsResponse) error {
	if len(out) != 1 {
		return fmt.Errorf("expected 1 params output but got %d", len(out))
	}
	params, err := paramsFromValue(reflect.ValueOf(out[0]))
	if err != nil {
		return err
	}
	resp.Params = params
	return nil
}

func packDenomMetadata(_ context.Context, _ *pquery.Env, req *banktypes.QueryDenomMetadataRequest) ([]interface{}, error) {
	if req.Denom == "" {
		return nil, status.Error(codes.InvalidArgument, "invalid denom")
	}
	return []interface{}{req.Denom}, nil
}

func unpackDenomMetadata(_ context.Context, _ *pquery.Env, _ *banktypes.QueryDenomMetadataRequest, out []interface{}, resp *banktypes.QueryDenomMetadataResponse) error {
	if len(out) != 1 {
		return fmt.Errorf("expected 1 metadata output but got %d", len(out))
	}
	metadata, err := metadataFromValue(reflect.ValueOf(out[0]))
	if err != nil {
		return err
	}
	resp.Metadata = metadata
	return nil
}

func unpackDenomsMetadata(_ context.Context, _ *pquery.Env, req *banktypes.QueryDenomsMetadataRequest, out []interface{}, resp *banktypes.QueryDenomsMetadataResponse) error {
	if len(out) != 1 {
		return fmt.Errorf("expected 1 metadatas output but got %d", len(out))
	}
	metadatas, err := metadatasFromValue(reflect.ValueOf(out[0]))
	if err != nil {
		return err
	}
	paged, pageRes, err := paginateMetadata(metadatas, req.Pagination)
	if err != nil {
		return err
	}
	resp.Metadatas = paged
	resp.Pagination = pageRes
	return nil
}

func metadatasFromValue(value reflect.Value) ([]banktypes.Metadata, error) {
	if value.Kind() != reflect.Slice {
		return nil, fmt.Errorf("expected metadata slice but got %s", value.Kind())
	}
	metadatas := make([]banktypes.Metadata, 0, value.Len())
	for i := 0; i < value.Len(); i++ {
		metadata, err := metadataFromValue(value.Index(i))
		if err != nil {
			return nil, err
		}
		metadatas = append(metadatas, metadata)
	}
	return metadatas, nil
}

func metadataFromValue(value reflect.Value) (banktypes.Metadata, error) {
	description, err := pquery.FieldString(value, "Description")
	if err != nil {
		return banktypes.Metadata{}, err
	}
	base, err := pquery.FieldString(value, "Base")
	if err != nil {
		return banktypes.Metadata{}, err
	}
	display, err := pquery.FieldString(value, "Display")
	if err != nil {
		return banktypes.Metadata{}, err
	}
	name, err := pquery.FieldString(value, "Name")
	if err != nil {
		return banktypes.Metadata{}, err
	}
	symbol, err := pquery.FieldString(value, "Symbol")
	if err != nil {
		return banktypes.Metadata{}, err
	}
	denomUnitsValue := pquery.Field(value, "DenomUnits")
	if !denomUnitsValue.IsValid() {
		return banktypes.Metadata{}, errors.New("metadata missing DenomUnits")
	}
	if denomUnitsValue.Kind() != reflect.Slice {
		return banktypes.Metadata{}, fmt.Errorf("expected DenomUnits slice but got %s", denomUnitsValue.Kind())
	}
	denomUnits := make([]*banktypes.DenomUnit, 0, denomUnitsValue.Len())
	for i := 0; i < denomUnitsValue.Len(); i++ {
		unitValue := denomUnitsValue.Index(i)
		denom, err := pquery.FieldString(unitValue, "Denom")
		if err != nil {
			return banktypes.Metadata{}, err
		}
		exponent, err := pquery.FieldUint32(unitValue, "Exponent")
		if err != nil {
			return banktypes.Metadata{}, err
		}
		aliases, err := pquery.FieldStringSlice(unitValue, "Aliases")
		if err != nil {
			return banktypes.Metadata{}, err
		}
		denomUnits = append(denomUnits, &banktypes.DenomUnit{
			Denom:    denom,
			Exponent: exponent,
			Aliases:  aliases,
		})
	}
	return banktypes.Metadata{
		Description: description,
		DenomUnits:  denomUnits,
		Base:        base,
		Display:     display,
		Name:        name,
		Symbol:      symbol,
	}, nil
}

func paramsFromValue(value reflect.Value) (banktypes.Params, error) {
	defaultSendEnabled, err := pquery.FieldBool(value, "DefaultSendEnabled")
	if err != nil {
		return banktypes.Params{}, err
	}
	sendEnabledValue := pquery.Field(value, "SendEnabled")
	if !sendEnabledValue.IsValid() {
		return banktypes.Params{}, errors.New("params missing SendEnabled")
	}
	if sendEnabledValue.Kind() != reflect.Slice {
		return banktypes.Params{}, fmt.Errorf("expected SendEnabled slice but got %s", sendEnabledValue.Kind())
	}
	sendEnabled := make([]*banktypes.SendEnabled, 0, sendEnabledValue.Len())
	for i := 0; i < sendEnabledValue.Len(); i++ {
		item := sendEnabledValue.Index(i)
		denom, err := pquery.FieldString(item, "Denom")
		if err != nil {
			return banktypes.Params{}, err
		}
		enabled, err := pquery.FieldBool(item, "Enabled")
		if err != nil {
			return banktypes.Params{}, err
		}
		sendEnabled = append(sendEnabled, &banktypes.SendEnabled{Denom: denom, Enabled: enabled})
	}
	return banktypes.Params{SendEnabled: sendEnabled, DefaultSendEnabled: defaultSendEnabled}, nil
}

func paginateMetadata(metadatas []banktypes.Metadata, req *sdkquery.PageRequest) ([]banktypes.Metadata, *sdkquery.PageResponse, error) {
	return pquery.Paginate(metadatas, req, func(metadata banktypes.Metadata) []byte {
		return []byte(metadata.Base)
	})
}
