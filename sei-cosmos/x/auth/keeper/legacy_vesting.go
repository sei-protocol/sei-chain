package keeper

import (
	"bytes"
	"fmt"

	"google.golang.org/protobuf/encoding/protowire"

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

// RewriteLegacyVestingAccounts continues the rewrite Migrate3to4 starts, scanning at most
// maxAccounts accounts from where the previous call stopped and storing each one held under a type
// of the removed vesting module as the BaseAccount it embeds. It returns true when no rewrite is
// pending.
func (ak AccountKeeper) RewriteLegacyVestingAccounts(ctx sdk.Context, maxAccounts int) (done bool) {
	store := ctx.KVStore(ak.key)
	start := store.Get(types.LegacyVestingRewriteCursorKey)
	if start == nil {
		return true
	}
	accounts, next := ak.nextLegacyVestingAccounts(ctx, start, maxAccounts)
	for _, account := range accounts {
		ak.SetAccount(ctx, account)
		logger.Info("rewrote legacy vesting account as a base account", "address", account.Address)
	}
	if next == nil {
		store.Delete(types.LegacyVestingRewriteCursorKey)
		logger.Info("finished rewriting legacy vesting accounts")
		return true
	}
	store.Set(types.LegacyVestingRewriteCursorKey, next)
	return false
}

// startLegacyVestingRewrite schedules RewriteLegacyVestingAccounts to scan every account from the
// first.
func (ak AccountKeeper) startLegacyVestingRewrite(ctx sdk.Context) {
	ctx.KVStore(ak.key).Set(types.LegacyVestingRewriteCursorKey, types.AddressStoreKeyPrefix)
}

// nextLegacyVestingAccounts scans at most maxAccounts accounts from the store key start, and returns
// the BaseAccount embedded in each one stored under a vesting module type, with the key of the first
// account left unscanned, or nil when the scan reached the last account.
func (ak AccountKeeper) nextLegacyVestingAccounts(
	ctx sdk.Context,
	start []byte,
	maxAccounts int,
) (accounts []*types.BaseAccount, next []byte) {
	iterator := ctx.KVStore(ak.key).Iterator(start, sdk.PrefixEndBytes(types.AddressStoreKeyPrefix))
	defer func() { _ = iterator.Close() }()

	for scanned := 0; iterator.Valid(); iterator.Next() {
		if scanned == maxAccounts {
			return accounts, append([]byte(nil), iterator.Key()...)
		}
		scanned++
		account, err := ak.legacyVestingAccountAt(iterator.Key(), iterator.Value())
		if err != nil {
			// Halting the chain mid-rewrite would be worse than leaving the account as it is.
			logger.Error("left an account that cannot be rewritten under its vesting type",
				"key", fmt.Sprintf("%X", iterator.Key()), "err", err)
			continue
		}
		if account != nil {
			accounts = append(accounts, account)
		}
	}
	return accounts, nil
}

// legacyVestingAccountAt returns the BaseAccount embedded in the account encoded as bz under the
// account store key, or nil when bz does not encode a vesting module type.
func (ak AccountKeeper) legacyVestingAccountAt(key, bz []byte) (*types.BaseAccount, error) {
	depth, ok := legacyVestingTypeDepth(bz)
	if !ok {
		return nil, nil
	}
	account, err := ak.embeddedBaseAccount(bz, depth)
	if err != nil {
		return nil, err
	}
	// SetAccount keys the rewrite by the embedded address, so a mismatch
	// would write a second account rather than replace this one.
	if address := key[len(types.AddressStoreKeyPrefix):]; !bytes.Equal(account.GetAddress(), address) {
		return nil, fmt.Errorf("account at %X embeds the base account of %q", address, account.Address)
	}
	return account, nil
}

// legacyVestingTypeDepth reports whether bz encodes an account of a type the removed vesting module
// registered, and if so how deep its encoding embeds the BaseAccount.
func legacyVestingTypeDepth(bz []byte) (int, bool) {
	typeURL, _, err := lengthDelimitedField(bz, anyTypeURLField)
	if err != nil {
		return 0, false
	}
	depth, ok := legacyVestingAccountDepths[string(typeURL)]
	return depth, ok
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
