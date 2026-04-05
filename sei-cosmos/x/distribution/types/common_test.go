package types

import (
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/ed25519"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

// nolint:deadcode,unused,varcheck
var (
	delPk1       = ed25519.GenPrivKey().PubKey()
	delPk2       = ed25519.GenPrivKey().PubKey()
	delAddr1     = sdk.AccAddress(delPk1.Address())
	delAddr2     = sdk.AccAddress(delPk2.Address())
	emptyDelAddr sdk.AccAddress

	valPk1       = ed25519.GenPrivKey().PubKey()
	valAddr1     = sdk.ValAddress(valPk1.Address())
	emptyValAddr sdk.ValAddress
)
