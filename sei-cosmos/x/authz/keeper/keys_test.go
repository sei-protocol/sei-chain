package keeper

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/address"
	bank "github.com/cosmos/cosmos-sdk/x/bank/types"
)

var granter = sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
var grantee = sdk.AccAddress(ed25519.GenPrivKey().PubKey().Address())
var msgType = bank.SendAuthorization{}.MsgTypeURL()

func TestGrantkey(t *testing.T) {
	fmt.Printf("granter: %s, grantee: %s", granter, grantee)
	require := require.New(t)
	key := grantStoreKey(grantee, granter, msgType)
	require.Len(key, len(GrantKey)+len(address.MustLengthPrefix(grantee))+len(address.MustLengthPrefix(granter))+len([]byte(msgType)))

	// Test standard addresses
	granter1, grantee1 := addressesFromGrantStoreKey(grantStoreKey(grantee, granter, msgType))
	require.Equal(granter, granter1)
	require.Equal(grantee, grantee1)

	// Test addresses with special / non-standard lengths
	specialGranter := sdk.AccAddress("granter")
	specialGrantee := sdk.AccAddress("granteeeee")
	key = grantStoreKey(specialGrantee, specialGranter, msgType)
	require.Len(key, len(GrantKey)+len(address.MustLengthPrefix(specialGrantee))+len(address.MustLengthPrefix(specialGranter))+len([]byte(msgType)))
	granter1, grantee1 = addressesFromGrantStoreKey(grantStoreKey(specialGrantee, specialGranter, msgType))
	require.Equal(specialGranter, granter1)
	require.Equal(specialGrantee, grantee1)
}
