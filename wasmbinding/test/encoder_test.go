package wasmbinding

import (
	"encoding/json"
	"testing"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	wasmtypes "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"
	wasmvmtypes "github.com/sei-protocol/sei-chain/sei-wasmvm/types"
	"github.com/sei-protocol/sei-chain/wasmbinding"
	"github.com/stretchr/testify/require"
)

// TestCustomEncoderRefusesTokenfactoryMessages requires every tokenfactory custom
// message a contract can send to be refused as an unknown Sei message.
func TestCustomEncoderRefusesTokenfactoryMessages(t *testing.T) {
	sender := sdk.AccAddress("tokenfactory-sender1")
	for _, msg := range []string{
		`{"create_denom":{"subdenom":"subdenom"}}`,
		`{"mint_tokens":{"amount":{"denom":"factory/creator/subdenom","amount":"100"}}}`,
		`{"burn_tokens":{"amount":{"denom":"factory/creator/subdenom","amount":"10"}}}`,
		`{"change_admin":{"denom":"factory/creator/subdenom","new_admin_address":"admin"}}`,
		`{"set_metadata":{"metadata":{"base":"factory/creator/subdenom"}}}`,
	} {
		encoded, err := wasmbinding.CustomEncoder(sender, json.RawMessage(msg), wasmvmtypes.MessageInfo{}, wasmtypes.CodeInfo{})
		require.Equal(t, wasmvmtypes.UnsupportedRequest{Kind: "Unknown Sei Wasm Message"}, err, msg)
		require.Empty(t, encoded, msg)
	}
}
