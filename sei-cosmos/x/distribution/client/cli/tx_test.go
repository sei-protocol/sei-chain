package cli_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/spf13/pflag"

	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil/testdata"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/distribution/client/cli"

	"github.com/stretchr/testify/require"

	"github.com/stretchr/testify/assert"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

func Test_splitAndCall_NoMessages(t *testing.T) {
	clientCtx := client.Context{}

	err := cli.NewSplitAndApply(nil, clientCtx, nil, nil, 10)
	assert.NoError(t, err, "")
}

func Test_splitAndCall_Splitting(t *testing.T) {
	clientCtx := client.Context{}

	addr := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())

	// Add five messages
	msgs := []sdk.Msg{
		testdata.NewTestMsg(addr),
		testdata.NewTestMsg(addr),
		testdata.NewTestMsg(addr),
		testdata.NewTestMsg(addr),
		testdata.NewTestMsg(addr),
	}

	// Keep track of number of calls
	const chunkSize = 2

	callCount := 0
	err := cli.NewSplitAndApply(
		func(clientCtx client.Context, fs *pflag.FlagSet, msgs ...sdk.Msg) error {
			callCount++

			assert.NotNil(t, clientCtx)
			assert.NotNil(t, msgs)

			if callCount < 3 {
				assert.Equal(t, len(msgs), 2)
			} else {
				assert.Equal(t, len(msgs), 1)
			}

			return nil
		},
		clientCtx, nil, msgs, chunkSize)

	assert.NoError(t, err, "")
	assert.Equal(t, 3, callCount)
}

func TestParseProposal(t *testing.T) {
	encodingConfig := app.MakeEncodingConfig()

	okJSON := testutil.WriteToNewTempFile(t, `
{
  "title": "Community Pool Spend",
  "description": "Pay me some Atoms!",
  "recipient": "cosmos1s5afhd6gxevu37mkqcvvsj8qeylhn0rz46zdlq",
  "amount": "1000usei",
  "deposit": "1000usei"
}
`)

	proposal, err := cli.ParseCommunityPoolSpendProposalWithDeposit(encodingConfig.Marshaler, okJSON.Name())
	require.NoError(t, err)

	require.Equal(t, "Community Pool Spend", proposal.Title)
	require.Equal(t, "Pay me some Atoms!", proposal.Description)
	require.Equal(t, "cosmos1s5afhd6gxevu37mkqcvvsj8qeylhn0rz46zdlq", proposal.Recipient)
	require.Equal(t, "1000usei", proposal.Deposit)
	require.Equal(t, "1000usei", proposal.Amount)
}
