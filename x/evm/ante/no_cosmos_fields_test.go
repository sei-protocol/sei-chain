package ante_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	signing "github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/sei-protocol/sei-chain/x/evm/ante"
)

func TestEVMNoCosmosFieldsDecorator(t *testing.T) {
	decorator := ante.NewEVMNoCosmosFieldsDecorator()
	ctx := sdk.Context{}

	// Valid EVM tx: all forbidden fields empty
	tx := mockTx{
		body:      &txtypes.TxBody{},
		authInfo:  &txtypes.AuthInfo{Fee: &txtypes.Fee{}},
		signature: nil,
	}
	_, err := decorator.AnteHandle(ctx, tx, false, func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.NoError(t, err)

	// Memo set
	tx.body.Memo = "not empty"
	_, err = decorator.AnteHandle(ctx, tx, false, nil)
	require.ErrorContains(t, err, "memo must be empty")
	tx.body.Memo = ""

	// TimeoutHeight set
	tx.body.TimeoutHeight = 1
	_, err = decorator.AnteHandle(ctx, tx, false, nil)
	require.ErrorContains(t, err, "timeout_height must be zero")
	tx.body.TimeoutHeight = 0

	// ExtensionOptions set
	tx.body.ExtensionOptions = []*codectypes.Any{{}}
	_, err = decorator.AnteHandle(ctx, tx, false, nil)
	require.ErrorContains(t, err, "extension options must be empty")
	tx.body.ExtensionOptions = nil

	// NonCriticalExtensionOptions set
	tx.body.NonCriticalExtensionOptions = []*codectypes.Any{{}}
	_, err = decorator.AnteHandle(ctx, tx, false, nil)
	require.ErrorContains(t, err, "extension options must be empty")
	tx.body.NonCriticalExtensionOptions = nil

	// SignerInfos set
	tx.authInfo.SignerInfos = []*txtypes.SignerInfo{{}}
	_, err = decorator.AnteHandle(ctx, tx, false, nil)
	require.ErrorContains(t, err, "signer_infos must be empty")
	tx.authInfo.SignerInfos = nil

	// Fee.Amount set
	tx.authInfo.Fee.Amount = sdk.NewCoins(sdk.NewInt64Coin("usei", 1))
	_, err = decorator.AnteHandle(ctx, tx, false, nil)
	require.ErrorContains(t, err, "fee amount must be empty")
	tx.authInfo.Fee.Amount = nil

	// Fee.Payer set
	tx.authInfo.Fee.Payer = "not empty"
	_, err = decorator.AnteHandle(ctx, tx, false, nil)
	require.ErrorContains(t, err, "fee payer and granter must be empty")
	tx.authInfo.Fee.Payer = ""

	// Fee.Granter set
	tx.authInfo.Fee.Granter = "not empty"
	_, err = decorator.AnteHandle(ctx, tx, false, nil)
	require.ErrorContains(t, err, "fee payer and granter must be empty")
	tx.authInfo.Fee.Granter = ""

	// Signatures set
	tx.signature = []signing.SignatureV2{{}}
	_, err = decorator.AnteHandle(ctx, tx, false, nil)
	require.ErrorContains(t, err, "signatures must be empty")
	tx.signature = nil
}
