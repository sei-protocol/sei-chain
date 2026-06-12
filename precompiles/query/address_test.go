package query

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	addrprecompile "github.com/sei-protocol/sei-chain/precompiles/addr"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/stretchr/testify/require"
)

type fakeAddressCaller struct {
	t       *testing.T
	evmAddr common.Address
	seiAddr sdk.AccAddress
	err     error
}

func (f fakeAddressCaller) CallContract(_ context.Context, msg ethereum.CallMsg, _ *big.Int) ([]byte, error) {
	require.Equal(f.t, common.HexToAddress(addrprecompile.AddrAddress), *msg.To)
	if f.err != nil {
		return nil, f.err
	}

	contractABI := addrprecompile.GetABI()
	method, err := contractABI.MethodById(msg.Data[:4])
	require.NoError(f.t, err)
	switch method.Name {
	case addrprecompile.GetEvmAddressMethod:
		return method.Outputs.Pack(f.evmAddr)
	case addrprecompile.GetSeiAddressMethod:
		return method.Outputs.Pack(f.seiAddr.String())
	default:
		f.t.Fatalf("unexpected method %s", method.Name)
		return nil, nil
	}
}

func TestAddressHelpersUseAssociationPrecompile(t *testing.T) {
	seiAddr := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	evmAddr := common.HexToAddress("0x0000000000000000000000000000000000000042")
	env := &Env{caller: fakeAddressCaller{t: t, evmAddr: evmAddr, seiAddr: seiAddr}}

	gotEVM, err := env.EVMAddressForSeiAddress(context.Background(), seiAddr.String(), AllowCastAddress)
	require.NoError(t, err)
	require.Equal(t, evmAddr, gotEVM)

	gotSei, err := env.SeiAddressForEVMAddress(context.Background(), evmAddr, AllowCastAddress)
	require.NoError(t, err)
	require.Equal(t, seiAddr, gotSei)
}

func TestAddressHelpersCastOnlyWhenAllowed(t *testing.T) {
	seiAddr := sdk.AccAddress(bytes.Repeat([]byte{2}, 20))
	evmAddr := common.BytesToAddress(bytes.Repeat([]byte{3}, 20))
	env := &Env{caller: fakeAddressCaller{t: t, err: errors.New("not associated")}}

	gotEVM, err := env.EVMAddressForSeiAddress(context.Background(), seiAddr.String(), AllowCastAddress)
	require.NoError(t, err)
	require.Equal(t, common.BytesToAddress(seiAddr), gotEVM)

	gotSei, err := env.SeiAddressForEVMAddress(context.Background(), evmAddr, AllowCastAddress)
	require.NoError(t, err)
	require.Equal(t, sdk.AccAddress(evmAddr[:]), gotSei)

	_, err = env.EVMAddressForSeiAddress(context.Background(), seiAddr.String(), RequireAssociation)
	require.Error(t, err)
	_, err = env.SeiAddressForEVMAddress(context.Background(), evmAddr, RequireAssociation)
	require.Error(t, err)
}

func TestAddressHelpersDoNotCastUnexpectedLookupErrors(t *testing.T) {
	seiAddr := sdk.AccAddress(bytes.Repeat([]byte{4}, 20))
	evmAddr := common.BytesToAddress(bytes.Repeat([]byte{5}, 20))
	env := &Env{caller: fakeAddressCaller{t: t, err: errors.New("rpc unavailable")}}

	_, err := env.EVMAddressForSeiAddress(context.Background(), seiAddr.String(), AllowCastAddress)
	require.ErrorContains(t, err, "rpc unavailable")

	_, err = env.SeiAddressForEVMAddress(context.Background(), evmAddr, AllowCastAddress)
	require.ErrorContains(t, err, "rpc unavailable")
}
