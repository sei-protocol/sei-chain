package query

import (
	"context"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	addrprecompile "github.com/sei-protocol/sei-chain/precompiles/addr"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

type AddressPolicy uint8

const (
	RequireAssociation AddressPolicy = iota
	AllowCastAddress
)

func (e *Env) EVMAddressForSeiAddress(ctx context.Context, sei string, policy AddressPolicy) (common.Address, error) {
	seiAddr, err := sdk.AccAddressFromBech32(sei)
	if err != nil {
		return common.Address{}, err
	}
	evmAddr, err := e.associatedEVMAddress(ctx, sei)
	if err == nil {
		return evmAddr, nil
	}
	if policy == RequireAssociation || !isAssociationMissing(err) {
		return common.Address{}, err
	}
	return common.BytesToAddress(seiAddr), nil
}

func (e *Env) SeiAddressForEVMAddress(ctx context.Context, evm common.Address, policy AddressPolicy) (sdk.AccAddress, error) {
	seiAddr, err := e.associatedSeiAddress(ctx, evm)
	if err == nil {
		return seiAddr, nil
	}
	if policy == RequireAssociation || !isAssociationMissing(err) {
		return nil, err
	}
	return sdk.AccAddress(evm[:]), nil
}

func (e *Env) associatedEVMAddress(ctx context.Context, sei string) (common.Address, error) {
	contractABI := addrprecompile.GetABI()
	input, err := contractABI.Pack(addrprecompile.GetEvmAddressMethod, sei)
	if err != nil {
		return common.Address{}, err
	}
	output, err := e.EthCall(ctx, common.HexToAddress(addrprecompile.AddrAddress), input)
	if err != nil {
		return common.Address{}, err
	}
	values, err := contractABI.Unpack(addrprecompile.GetEvmAddressMethod, output)
	if err != nil {
		return common.Address{}, err
	}
	if len(values) != 1 {
		return common.Address{}, fmt.Errorf("expected 1 addr precompile output but got %d", len(values))
	}
	evmAddr, ok := values[0].(common.Address)
	if !ok {
		return common.Address{}, fmt.Errorf("expected common.Address addr precompile output but got %T", values[0])
	}
	return evmAddr, nil
}

func (e *Env) associatedSeiAddress(ctx context.Context, evm common.Address) (sdk.AccAddress, error) {
	contractABI := addrprecompile.GetABI()
	input, err := contractABI.Pack(addrprecompile.GetSeiAddressMethod, evm)
	if err != nil {
		return nil, err
	}
	output, err := e.EthCall(ctx, common.HexToAddress(addrprecompile.AddrAddress), input)
	if err != nil {
		return nil, err
	}
	values, err := contractABI.Unpack(addrprecompile.GetSeiAddressMethod, output)
	if err != nil {
		return nil, err
	}
	if len(values) != 1 {
		return nil, fmt.Errorf("expected 1 addr precompile output but got %d", len(values))
	}
	seiAddr, ok := values[0].(string)
	if !ok {
		return nil, fmt.Errorf("expected string addr precompile output but got %T", values[0])
	}
	return sdk.AccAddressFromBech32(seiAddr)
}

func isAssociationMissing(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not associated") || strings.Contains(msg, "not linked")
}
