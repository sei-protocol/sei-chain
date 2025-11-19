package types

import (
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// nolint:deadcode,unused,varcheck
var (
	delPk1       = ed25519.GenPrivKey().PubKey()
	delPk2       = ed25519.GenPrivKey().PubKey()
	delPk3       = ed25519.GenPrivKey().PubKey()
	delAddr1     = seitypes.AccAddress(delPk1.Address())
	delAddr2     = seitypes.AccAddress(delPk2.Address())
	delAddr3     = seitypes.AccAddress(delPk3.Address())
	emptyDelAddr seitypes.AccAddress

	valPk1       = ed25519.GenPrivKey().PubKey()
	valPk2       = ed25519.GenPrivKey().PubKey()
	valPk3       = ed25519.GenPrivKey().PubKey()
	valAddr1     = seitypes.ValAddress(valPk1.Address())
	valAddr2     = seitypes.ValAddress(valPk2.Address())
	valAddr3     = seitypes.ValAddress(valPk3.Address())
	emptyValAddr seitypes.ValAddress

	emptyPubkey cryptotypes.PubKey
)
