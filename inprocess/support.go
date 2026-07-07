//go:build inprocess

package inprocess

import (
	appparams "github.com/sei-protocol/sei-chain/app/params"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/hd"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
)

// encoding is the codec/tx-config bundle threaded through app.New and genesis
// assembly. Aliased so the call sites read without the full package path.
type encoding = appparams.EncodingConfig

// hdSecp256k1 is the default key-signing algorithm (matches testutil/network).
func hdSecp256k1() hd.PubKeyType { return hd.Secp256k1Type }

// consensusTokens converts a consensus power to a token amount at the default
// power reduction — the per-validator funding/staking unit.
func consensusTokens(power int64) sdk.Int {
	return sdk.TokensFromConsensusPower(power, sdk.DefaultPowerReduction)
}

// accountRetriever is the client-side account/sequence reader used to build txs.
func accountRetriever() client.AccountRetriever { return authtypes.AccountRetriever{} }
