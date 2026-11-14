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

// LegacyVestingRewrite is the outcome of one RewriteLegacyVestingAccounts call. Done reports that
// no account is left to scan, and Skipped holds one error for each account left in place because it
// could not be rewritten.
type LegacyVestingRewrite struct {
	Done    bool
	Skipped []error
}

// RewriteLegacyVestingAccounts scans up to maxAccounts of the accounts Migrate3to4 scheduled and not
// yet scanned, and stores each one held under a type of the removed vesting module as the BaseAccount
// it embeds. It panics when maxAccounts is not positive.
func (ak AccountKeeper) RewriteLegacyVestingAccounts(ctx sdk.Context, maxAccounts int) LegacyVestingRewrite {
	if maxAccounts <= 0 {
		panic(fmt.Sprintf("legacy vesting account rewrite batch size must be positive, got %d", maxAccounts))
	}
	store := ctx.KVStore(ak.key)
	start := store.Get(types.LegacyVestingRewriteCursorKey)
	if start == nil {
		return LegacyVestingRewrite{Done: true}
	}
	accounts, skipped, next := ak.nextLegacyVestingAccounts(ctx, start, maxAccounts)
	for _, account := range accounts {
		ak.SetAccount(ctx, account)
		logger.Info("rewrote legacy vesting account as a base account", "address", account.Address)
	}
	if next == nil {
		store.Delete(types.LegacyVestingRewriteCursorKey)
		logger.Info("finished rewriting legacy vesting accounts")
		return LegacyVestingRewrite{Done: true, Skipped: skipped}
	}
	store.Set(types.LegacyVestingRewriteCursorKey, next)
	return LegacyVestingRewrite{Skipped: skipped}
}

// startLegacyVestingRewrite schedules every account for RewriteLegacyVestingAccounts to scan.
func (ak AccountKeeper) startLegacyVestingRewrite(ctx sdk.Context) {
	ctx.KVStore(ak.key).Set(types.LegacyVestingRewriteCursorKey, types.AddressStoreKeyPrefix)
}

// nextLegacyVestingAccounts scans at most maxAccounts accounts from the store key start. It returns
// the BaseAccount embedded in each one stored under a vesting module type, an error for each such
// account it cannot convert, and the key of the first account left unscanned, or nil when the scan
// reached the last account.
func (ak AccountKeeper) nextLegacyVestingAccounts(
	ctx sdk.Context,
	start []byte,
	maxAccounts int,
) (accounts []*types.BaseAccount, skipped []error, next []byte) {
	iterator := ctx.KVStore(ak.key).Iterator(start, sdk.PrefixEndBytes(types.AddressStoreKeyPrefix))
	defer func() { _ = iterator.Close() }()

	for scanned := 0; iterator.Valid(); iterator.Next() {
		if scanned == maxAccounts {
			return accounts, skipped, append([]byte(nil), iterator.Key()...)
		}
		scanned++
		address := iterator.Key()[len(types.AddressStoreKeyPrefix):]
		account, err := ak.legacyVestingAccountAt(address, iterator.Value())
		if err != nil {
			// Halting the chain mid-rewrite would be worse than leaving the account as it is.
			logger.Error("left an account that cannot be rewritten under its vesting type",
				"address", fmt.Sprintf("%X", address), "err", err)
			skipped = append(skipped, err)
			continue
		}
		if account != nil {
			accounts = append(accounts, account)
		}
	}
	return accounts, skipped, nil
}

// legacyVestingAccountAt returns the BaseAccount embedded in the account stored as bz under address,
// or nil when bz is not stored under a type of the removed vesting module. It fails when the type of
// bz cannot be read, or when bz is a vesting account that does not decode or embeds another address.
func (ak AccountKeeper) legacyVestingAccountAt(address, bz []byte) (*types.BaseAccount, error) {
	typeURL, _, err := lengthDelimitedField(bz, anyTypeURLField)
	if err != nil {
		return nil, fmt.Errorf("account at %X: %w", address, err)
	}
	depth, ok := legacyVestingAccountDepths[string(typeURL)]
	if !ok {
		return nil, nil
	}
	account, err := ak.embeddedBaseAccount(bz, depth)
	if err != nil {
		return nil, fmt.Errorf("vesting account at %X: %w", address, err)
	}
	// SetAccount keys a write by the embedded address, so reading this account as another would
	// move its next write to that other address.
	if !bytes.Equal(account.GetAddress(), address) {
		return nil, fmt.Errorf("vesting account at %X embeds the base account of %q", address, account.Address)
	}
	return account, nil
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
