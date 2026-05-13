package bankquery

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"slices"

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
			Pack:          packNoArgs[banktypes.QueryTotalSupplyRequest],
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
			Pack:          packNoArgs[banktypes.QueryParamsRequest],
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
			Pack:          packNoArgs[banktypes.QueryDenomsMetadataRequest],
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
	amount, err := singleBigInt(out)
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
	coins, err := coinsFromOutput(out)
	if err != nil {
		return err
	}
	paged, pageRes, err := paginateCoins(coins, req.Pagination)
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
	coins, err := coinsFromOutput(out)
	if err != nil {
		return err
	}
	paged, pageRes, err := paginateCoins(coins, req.Pagination)
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
	amount, err := singleBigInt(out)
	if err != nil {
		return err
	}
	resp.Amount = sdk.NewCoin(req.Denom, sdk.NewIntFromBigInt(amount))
	return nil
}

func unpackTotalSupply(_ context.Context, _ *pquery.Env, req *banktypes.QueryTotalSupplyRequest, out []interface{}, resp *banktypes.QueryTotalSupplyResponse) error {
	coins, err := coinsFromOutput(out)
	if err != nil {
		return err
	}
	paged, pageRes, err := paginateCoins(coins, req.Pagination)
	if err != nil {
		return err
	}
	resp.Supply = paged
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

func packNoArgs[Req any](_ context.Context, _ *pquery.Env, _ *Req) ([]interface{}, error) {
	return nil, nil
}

func singleBigInt(out []interface{}) (*big.Int, error) {
	if len(out) != 1 {
		return nil, fmt.Errorf("expected 1 output but got %d", len(out))
	}
	amount, ok := out[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("expected *big.Int output but got %T", out[0])
	}
	return amount, nil
}

func coinsFromOutput(out []interface{}) (sdk.Coins, error) {
	if len(out) != 1 {
		return nil, fmt.Errorf("expected 1 coin output but got %d", len(out))
	}
	value := reflect.ValueOf(out[0])
	if value.Kind() != reflect.Slice {
		return nil, fmt.Errorf("expected coin slice but got %T", out[0])
	}
	coins := make(sdk.Coins, 0, value.Len())
	for i := 0; i < value.Len(); i++ {
		amount, err := fieldBigInt(value.Index(i), "Amount")
		if err != nil {
			return nil, err
		}
		denom, err := fieldString(value.Index(i), "Denom")
		if err != nil {
			return nil, err
		}
		coins = append(coins, sdk.NewCoin(denom, sdk.NewIntFromBigInt(amount)))
	}
	return sdk.NewCoins(coins...), nil
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
	description, err := fieldString(value, "Description")
	if err != nil {
		return banktypes.Metadata{}, err
	}
	base, err := fieldString(value, "Base")
	if err != nil {
		return banktypes.Metadata{}, err
	}
	display, err := fieldString(value, "Display")
	if err != nil {
		return banktypes.Metadata{}, err
	}
	name, err := fieldString(value, "Name")
	if err != nil {
		return banktypes.Metadata{}, err
	}
	symbol, err := fieldString(value, "Symbol")
	if err != nil {
		return banktypes.Metadata{}, err
	}
	denomUnitsValue := field(value, "DenomUnits")
	if !denomUnitsValue.IsValid() {
		return banktypes.Metadata{}, errors.New("metadata missing DenomUnits")
	}
	if denomUnitsValue.Kind() != reflect.Slice {
		return banktypes.Metadata{}, fmt.Errorf("expected DenomUnits slice but got %s", denomUnitsValue.Kind())
	}
	denomUnits := make([]*banktypes.DenomUnit, 0, denomUnitsValue.Len())
	for i := 0; i < denomUnitsValue.Len(); i++ {
		unitValue := denomUnitsValue.Index(i)
		denom, err := fieldString(unitValue, "Denom")
		if err != nil {
			return banktypes.Metadata{}, err
		}
		exponent, err := fieldUint32(unitValue, "Exponent")
		if err != nil {
			return banktypes.Metadata{}, err
		}
		aliases, err := fieldStringSlice(unitValue, "Aliases")
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
	defaultSendEnabled, err := fieldBool(value, "DefaultSendEnabled")
	if err != nil {
		return banktypes.Params{}, err
	}
	sendEnabledValue := field(value, "SendEnabled")
	if !sendEnabledValue.IsValid() {
		return banktypes.Params{}, errors.New("params missing SendEnabled")
	}
	if sendEnabledValue.Kind() != reflect.Slice {
		return banktypes.Params{}, fmt.Errorf("expected SendEnabled slice but got %s", sendEnabledValue.Kind())
	}
	sendEnabled := make([]*banktypes.SendEnabled, 0, sendEnabledValue.Len())
	for i := 0; i < sendEnabledValue.Len(); i++ {
		item := sendEnabledValue.Index(i)
		denom, err := fieldString(item, "Denom")
		if err != nil {
			return banktypes.Params{}, err
		}
		enabled, err := fieldBool(item, "Enabled")
		if err != nil {
			return banktypes.Params{}, err
		}
		sendEnabled = append(sendEnabled, &banktypes.SendEnabled{Denom: denom, Enabled: enabled})
	}
	return banktypes.Params{SendEnabled: sendEnabled, DefaultSendEnabled: defaultSendEnabled}, nil
}

func field(value reflect.Value, name string) reflect.Value {
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return reflect.Value{}
	}
	return value.FieldByName(name)
}

func fieldString(value reflect.Value, name string) (string, error) {
	field := field(value, name)
	if !field.IsValid() || field.Kind() != reflect.String {
		return "", fmt.Errorf("expected string field %s", name)
	}
	return field.String(), nil
}

func fieldBool(value reflect.Value, name string) (bool, error) {
	field := field(value, name)
	if !field.IsValid() || field.Kind() != reflect.Bool {
		return false, fmt.Errorf("expected bool field %s", name)
	}
	return field.Bool(), nil
}

func fieldUint32(value reflect.Value, name string) (uint32, error) {
	field := field(value, name)
	if !field.IsValid() {
		return 0, fmt.Errorf("expected uint32 field %s", name)
	}
	if field.Kind() < reflect.Uint || field.Kind() > reflect.Uint64 {
		return 0, fmt.Errorf("expected uint field %s", name)
	}
	if field.Uint() > math.MaxUint32 {
		return 0, fmt.Errorf("field %s overflows uint32", name)
	}
	return uint32(field.Uint()), nil //nolint:gosec // bounded by MaxUint32 check above
}

func fieldBigInt(value reflect.Value, name string) (*big.Int, error) {
	field := field(value, name)
	if !field.IsValid() {
		return nil, fmt.Errorf("expected *big.Int field %s", name)
	}
	amount, ok := field.Interface().(*big.Int)
	if !ok {
		return nil, fmt.Errorf("expected *big.Int field %s", name)
	}
	return amount, nil
}

func fieldStringSlice(value reflect.Value, name string) ([]string, error) {
	field := field(value, name)
	if !field.IsValid() || field.Kind() != reflect.Slice {
		return nil, fmt.Errorf("expected string slice field %s", name)
	}
	aliases := make([]string, 0, field.Len())
	for i := 0; i < field.Len(); i++ {
		if field.Index(i).Kind() != reflect.String {
			return nil, fmt.Errorf("expected string element in field %s", name)
		}
		aliases = append(aliases, field.Index(i).String())
	}
	return aliases, nil
}

func paginateCoins(coins sdk.Coins, req *sdkquery.PageRequest) (sdk.Coins, *sdkquery.PageResponse, error) {
	items, pageRes, err := paginate(coins, req, func(coin sdk.Coin) []byte {
		return []byte(coin.Denom)
	})
	if err != nil {
		return nil, nil, err
	}
	return sdk.Coins(items), pageRes, nil
}

func paginateMetadata(metadatas []banktypes.Metadata, req *sdkquery.PageRequest) ([]banktypes.Metadata, *sdkquery.PageResponse, error) {
	return paginate(metadatas, req, func(metadata banktypes.Metadata) []byte {
		return []byte(metadata.Base)
	})
}

func paginate[T any](items []T, req *sdkquery.PageRequest, keyFn func(T) []byte) ([]T, *sdkquery.PageResponse, error) {
	if req == nil {
		req = &sdkquery.PageRequest{}
	}
	if req.Offset > 0 && len(req.Key) > 0 {
		return nil, nil, fmt.Errorf("invalid request, either offset or key is expected, got both")
	}

	ordered := slices.Clone(items)
	if req.Reverse {
		slices.Reverse(ordered)
	}
	orderedLen := uint64(len(ordered)) //nolint:gosec // len is non-negative and used only as an in-memory page bound

	limit := req.Limit
	countTotal := req.CountTotal
	if limit == 0 {
		limit = sdkquery.DefaultLimit
		countTotal = true
	}

	start := req.Offset
	keyPagination := len(req.Key) > 0
	if keyPagination {
		start = orderedLen
		for i, item := range ordered {
			if bytes.Equal(keyFn(item), req.Key) {
				start = uint64(i) //nolint:gosec // i is bounded by len(ordered)
				break
			}
		}
	}

	if start > orderedLen {
		start = orderedLen
	}
	end := start + limit
	if end < start || end > orderedLen {
		end = orderedLen
	}

	var nextKey []byte
	if end < orderedLen {
		nextKey = keyFn(ordered[end])
	}
	pageRes := &sdkquery.PageResponse{NextKey: nextKey}
	if countTotal && !keyPagination {
		pageRes.Total = orderedLen
	}

	return ordered[int(start):int(end)], pageRes, nil //nolint:gosec // start and end are bounded by len(ordered)
}
