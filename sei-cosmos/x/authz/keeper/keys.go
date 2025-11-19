package keeper

import (
	"github.com/cosmos/cosmos-sdk/conv"
	"github.com/cosmos/cosmos-sdk/types/address"
	"github.com/cosmos/cosmos-sdk/types/kv"
	"github.com/cosmos/cosmos-sdk/x/authz"
	seitypes "github.com/sei-protocol/sei-chain/types"
)

// Keys for store prefixes
var (
	GrantKey = []byte{0x01} // prefix for each key
)

// StoreKey is the store key string for authz
const StoreKey = authz.ModuleName

// grantStoreKey - return authorization store key
// Items are stored with the following key: values
//
// - 0x01<granterAddressLen (1 Byte)><granterAddress_Bytes><granteeAddressLen (1 Byte)><granteeAddress_Bytes><msgType_Bytes>: Grant
func grantStoreKey(grantee seitypes.AccAddress, granter seitypes.AccAddress, msgType string) []byte {
	m := conv.UnsafeStrToBytes(msgType)
	granter = address.MustLengthPrefix(granter)
	grantee = address.MustLengthPrefix(grantee)

	l := 1 + len(grantee) + len(granter) + len(m)
	var key = make([]byte, l)
	copy(key, GrantKey)
	copy(key[1:], granter)
	copy(key[1+len(granter):], grantee)
	copy(key[l-len(m):], m)
	//	fmt.Println(">>>> len", l, key)
	return key
}

// addressesFromGrantStoreKey - split granter & grantee address from the authorization key
func addressesFromGrantStoreKey(key []byte) (granterAddr, granteeAddr seitypes.AccAddress) {
	// key is of format:
	// 0x01<granterAddressLen (1 Byte)><granterAddress_Bytes><granteeAddressLen (1 Byte)><granteeAddress_Bytes><msgType_Bytes>
	kv.AssertKeyAtLeastLength(key, 2)
	granterAddrLen := int(key[1]) // remove prefix key
	kv.AssertKeyAtLeastLength(key, 3+granterAddrLen)
	granterAddr = seitypes.AccAddress(key[2 : 2+granterAddrLen])
	granteeAddrLen := int(key[2+granterAddrLen])
	kv.AssertKeyAtLeastLength(key, 4+granterAddrLen+granteeAddrLen)
	granteeAddr = seitypes.AccAddress(key[3+granterAddrLen : 3+granterAddrLen+granteeAddrLen])

	return granterAddr, granteeAddr
}

// firstAddressFromGrantStoreKey parses the first address only
func firstAddressFromGrantStoreKey(key []byte) seitypes.AccAddress {
	addrLen := key[0]
	return seitypes.AccAddress(key[1 : 1+addrLen])
}
