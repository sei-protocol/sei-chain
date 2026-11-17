package keeper

import (
	"bytes"
	"fmt"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/sei-protocol/sei-chain/sei-cosmos/store/prefix"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
)

const (
	anyTypeURLField protowire.Number = 1
	anyValueField   protowire.Number = 2
	// embeddedAccountField is base_account in BaseVestingAccount and
	// base_vesting_account in every other vesting account type.
	embeddedAccountField protowire.Number = 1
)

// legacyVestingAccountDepths maps the type URL of each account type the removed
// vesting module registered to the number of embeddedAccountField levels that
// separate its encoding from the BaseAccount it embeds.
var legacyVestingAccountDepths = map[string]int{
	"/cosmos.vesting.v1beta1.BaseVestingAccount":       1,
	"/cosmos.vesting.v1beta1.ContinuousVestingAccount": 2,
	"/cosmos.vesting.v1beta1.DelayedVestingAccount":    2,
	"/cosmos.vesting.v1beta1.PeriodicVestingAccount":   2,
	"/cosmos.vesting.v1beta1.PermanentLockedAccount":   2,
}

// legacyVestingAddresses maps a chain ID to the address of every account that
// chain stores under a type of the removed vesting module.
var legacyVestingAddresses = map[string][]string{
	// Creating vesting accounts has been disabled on pacific-1 since its v6.7
	// upgrade at height 236450000, so the set this lists cannot grow.
	"pacific-1": {
		"sei1ga833mj6jc6d2lye6j84stug9h2mpxcxuyw5ak",
		"sei15wfxcaz3phxa7hyl9z9wr064nc7zt6y79ywkvw",
		"sei1570e0uvdywv0wp79mz235v6zzuu69268xhvk6x",
	},
}

// legacyVestingAccounts returns the BaseAccount embedded in every account
// stored under a type of the removed vesting module. On a chain that
// legacyVestingAddresses lists it reads only the listed addresses, and on any
// other chain it scans the account store.
func (ak AccountKeeper) legacyVestingAccounts(ctx sdk.Context) ([]*types.BaseAccount, error) {
	if addresses, ok := legacyVestingAddresses[ctx.ChainID()]; ok {
		return ak.listedLegacyVestingAccounts(ctx, addresses)
	}
	return ak.scanLegacyVestingAccounts(ctx)
}

// listedLegacyVestingAccounts returns, in the order given, the BaseAccount
// embedded in each of addresses that holds an account stored under a type of
// the removed vesting module. An address holding no account, or an account of
// another type, is skipped.
func (ak AccountKeeper) listedLegacyVestingAccounts(ctx sdk.Context, addresses []string) ([]*types.BaseAccount, error) {
	store := ctx.KVStore(ak.key)
	accounts := make([]*types.BaseAccount, 0, len(addresses))
	for _, bech32Address := range addresses {
		address, err := sdk.AccAddressFromBech32(bech32Address)
		if err != nil {
			return nil, err
		}
		encoded := store.Get(types.AddressStoreKey(address))
		if encoded == nil {
			continue
		}
		account, ok, err := ak.legacyVestingAccount(address, encoded)
		if err != nil {
			return nil, err
		}
		if ok {
			accounts = append(accounts, account)
		}
	}
	return accounts, nil
}

// scanLegacyVestingAccounts returns, in store order, the BaseAccount embedded in
// every account stored under a type of the removed vesting module.
func (ak AccountKeeper) scanLegacyVestingAccounts(ctx sdk.Context) ([]*types.BaseAccount, error) {
	store := prefix.NewStore(ctx.KVStore(ak.key), types.AddressStoreKeyPrefix)
	iterator := store.Iterator(nil, nil)
	defer func() { _ = iterator.Close() }()

	var accounts []*types.BaseAccount
	for ; iterator.Valid(); iterator.Next() {
		account, ok, err := ak.legacyVestingAccount(iterator.Key(), iterator.Value())
		if err != nil {
			return nil, err
		}
		if ok {
			accounts = append(accounts, account)
		}
	}
	return accounts, nil
}

// legacyVestingAccount returns the BaseAccount embedded in the account stored at
// address as encoded, and whether encoded is of a type of the removed vesting
// module.
func (ak AccountKeeper) legacyVestingAccount(address, encoded []byte) (*types.BaseAccount, bool, error) {
	typeURL, _, err := lengthDelimitedField(encoded, anyTypeURLField)
	if err != nil {
		return nil, false, fmt.Errorf("account %X: %w", address, err)
	}
	depth, ok := legacyVestingAccountDepths[string(typeURL)]
	if !ok {
		return nil, false, nil
	}
	account, err := ak.embeddedBaseAccount(encoded, depth)
	if err != nil {
		return nil, false, fmt.Errorf("%s at %X: %w", typeURL, address, err)
	}
	// SetAccount keys the rewrite by the embedded address, so a mismatch
	// would write a second account rather than replace this one.
	if !bytes.Equal(account.GetAddress(), address) {
		return nil, false, fmt.Errorf("%s at %X embeds the base account of %q", typeURL, address, account.Address)
	}
	return account, true, nil
}

// embeddedBaseAccount decodes the BaseAccount that the vesting account encoded
// in the Any bz embeds depth levels down.
func (ak AccountKeeper) embeddedBaseAccount(bz []byte, depth int) (*types.BaseAccount, error) {
	message, err := requiredLengthDelimitedField(bz, anyValueField)
	if err != nil {
		return nil, err
	}
	for range depth {
		if message, err = requiredLengthDelimitedField(message, embeddedAccountField); err != nil {
			return nil, err
		}
	}
	var account types.BaseAccount
	if err := ak.cdc.Unmarshal(message, &account); err != nil {
		return nil, err
	}
	return &account, nil
}

// requiredLengthDelimitedField is lengthDelimitedField for a field that must be
// present.
func requiredLengthDelimitedField(bz []byte, num protowire.Number) ([]byte, error) {
	field, found, err := lengthDelimitedField(bz, num)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("field %d is missing", num)
	}
	return field, nil
}

// lengthDelimitedField returns the payload of field num of the encoded message
// bz, and whether the field is present. It fails when bz is malformed, or
// carries the field more than once or with another wire type.
func lengthDelimitedField(bz []byte, num protowire.Number) ([]byte, bool, error) {
	var field []byte
	found := false
	for len(bz) > 0 {
		fieldNum, wireType, n := protowire.ConsumeTag(bz)
		if n < 0 {
			return nil, false, protowire.ParseError(n)
		}
		bz = bz[n:]
		if fieldNum != num {
			n = protowire.ConsumeFieldValue(fieldNum, wireType, bz)
			if n < 0 {
				return nil, false, protowire.ParseError(n)
			}
			bz = bz[n:]
			continue
		}
		if wireType != protowire.BytesType {
			return nil, false, fmt.Errorf("field %d has wire type %d, want %d", num, wireType, protowire.BytesType)
		}
		if found {
			return nil, false, fmt.Errorf("field %d appears more than once", num)
		}
		field, n = protowire.ConsumeBytes(bz)
		if n < 0 {
			return nil, false, protowire.ParseError(n)
		}
		bz = bz[n:]
		found = true
	}
	return field, found, nil
}
