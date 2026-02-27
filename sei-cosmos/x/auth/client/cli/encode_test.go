package cli_test

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/client/cli"
)

func TestGetCommandEncode(t *testing.T) {
	encodingConfig := app.MakeEncodingConfig()

	cmd := cli.GetEncodeCommand()
	_ = testutil.ApplyMockIODiscardOutErr(cmd)

	txCfg := encodingConfig.TxConfig

	// Build a test transaction
	builder := txCfg.NewTxBuilder()
	builder.SetGasLimit(50000)
	builder.SetFeeAmount(sdk.Coins{sdk.NewInt64Coin("atom", 150)})
	builder.SetMemo("foomemo")
	jsonEncoded, err := txCfg.TxJSONEncoder()(builder.GetTx())
	require.NoError(t, err)

	txFile := testutil.WriteToNewTempFile(t, string(jsonEncoded))
	txFileName := txFile.Name()

	ctx := context.Background()
	clientCtx := client.Context{}.
		WithTxConfig(encodingConfig.TxConfig).
		WithCodec(encodingConfig.Marshaler)
	ctx = context.WithValue(ctx, client.ClientContextKey, &clientCtx)

	cmd.SetArgs([]string{txFileName})
	err = cmd.ExecuteContext(ctx)
	require.NoError(t, err)
}

func TestGetCommandDecode(t *testing.T) {
	encodingConfig := app.MakeEncodingConfig()

	clientCtx := client.Context{}.
		WithTxConfig(encodingConfig.TxConfig).
		WithCodec(encodingConfig.Marshaler)

	cmd := cli.GetDecodeCommand()
	_ = testutil.ApplyMockIODiscardOutErr(cmd)

	txCfg := encodingConfig.TxConfig
	clientCtx = clientCtx.WithTxConfig(txCfg)

	// Build a test transaction
	builder := txCfg.NewTxBuilder()
	builder.SetGasLimit(50000)
	builder.SetFeeAmount(sdk.Coins{sdk.NewInt64Coin("atom", 150)})
	builder.SetMemo("foomemo")

	// Encode transaction
	txBytes, err := clientCtx.TxConfig.TxEncoder()(builder.GetTx())
	require.NoError(t, err)

	// Convert the transaction into base64 encoded string
	base64Encoded := base64.StdEncoding.EncodeToString(txBytes)

	ctx := context.Background()
	ctx = context.WithValue(ctx, client.ClientContextKey, &clientCtx)

	// Execute the command
	cmd.SetArgs([]string{base64Encoded})
	require.NoError(t, cmd.ExecuteContext(ctx))
}
