package query

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"slices"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkquery "github.com/sei-protocol/sei-chain/sei-cosmos/types/query"
)

// PackNoArgs adapts no-argument precompile query methods to a binding packer.
func PackNoArgs[Req any](_ context.Context, _ *Env, _ *Req) ([]interface{}, error) {
	return nil, nil
}

// SingleBigInt extracts a single *big.Int ABI output.
func SingleBigInt(out []interface{}) (*big.Int, error) {
	if len(out) != 1 {
		return nil, fmt.Errorf("expected 1 output but got %d", len(out))
	}
	amount, ok := out[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("expected *big.Int output but got %T", out[0])
	}
	return amount, nil
}

// CoinsFromOutput decodes a single ABI output slice containing Amount and Denom fields.
func CoinsFromOutput(out []interface{}) (sdk.Coins, error) {
	if len(out) != 1 {
		return nil, fmt.Errorf("expected 1 coin output but got %d", len(out))
	}
	value := reflect.ValueOf(out[0])
	if value.Kind() != reflect.Slice {
		return nil, fmt.Errorf("expected coin slice but got %T", out[0])
	}
	coins := make(sdk.Coins, 0, value.Len())
	for i := 0; i < value.Len(); i++ {
		amount, err := FieldBigInt(value.Index(i), "Amount")
		if err != nil {
			return nil, err
		}
		denom, err := FieldString(value.Index(i), "Denom")
		if err != nil {
			return nil, err
		}
		coins = append(coins, sdk.NewCoin(denom, sdk.NewIntFromBigInt(amount)))
	}
	return sdk.NewCoins(coins...), nil
}

// Field returns a named field from a struct or pointer to a struct.
func Field(value reflect.Value, name string) reflect.Value {
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return reflect.Value{}
	}
	return value.FieldByName(name)
}

// FieldString returns a named string field from a struct-like value.
func FieldString(value reflect.Value, name string) (string, error) {
	field := Field(value, name)
	if !field.IsValid() || field.Kind() != reflect.String {
		return "", fmt.Errorf("expected string field %s", name)
	}
	return field.String(), nil
}

// FieldBool returns a named bool field from a struct-like value.
func FieldBool(value reflect.Value, name string) (bool, error) {
	field := Field(value, name)
	if !field.IsValid() || field.Kind() != reflect.Bool {
		return false, fmt.Errorf("expected bool field %s", name)
	}
	return field.Bool(), nil
}

// FieldUint32 returns a named unsigned integer field narrowed to uint32.
func FieldUint32(value reflect.Value, name string) (uint32, error) {
	field := Field(value, name)
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

// FieldBigInt returns a named *big.Int field from a struct-like value.
func FieldBigInt(value reflect.Value, name string) (*big.Int, error) {
	field := Field(value, name)
	if !field.IsValid() {
		return nil, fmt.Errorf("expected *big.Int field %s", name)
	}
	amount, ok := field.Interface().(*big.Int)
	if !ok {
		return nil, fmt.Errorf("expected *big.Int field %s", name)
	}
	return amount, nil
}

// FieldStringSlice returns a named []string field from a struct-like value.
func FieldStringSlice(value reflect.Value, name string) ([]string, error) {
	field := Field(value, name)
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

// PaginateCoins applies Cosmos query pagination to an in-memory coin set.
func PaginateCoins(coins sdk.Coins, req *sdkquery.PageRequest) (sdk.Coins, *sdkquery.PageResponse, error) {
	items, pageRes, err := Paginate(coins, req, func(coin sdk.Coin) []byte {
		return []byte(coin.Denom)
	})
	if err != nil {
		return nil, nil, err
	}
	return sdk.Coins(items), pageRes, nil
}

// Paginate applies Cosmos query pagination to an in-memory ordered slice.
func Paginate[T any](items []T, req *sdkquery.PageRequest, keyFn func(T) []byte) ([]T, *sdkquery.PageResponse, error) {
	if req == nil {
		req = &sdkquery.PageRequest{}
	}
	if req.Offset > 0 && req.Key != nil {
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
