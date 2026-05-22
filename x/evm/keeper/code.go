package keeper

import (
	"encoding/binary"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/prefix"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

// codeSizeEncodedLen is the fixed width of the big-endian uint64 encoding
// used for the cached code-size lookup.
const codeSizeEncodedLen = 8

// GetCode returns the contract bytecode stored at addr, or nil if no code
// is set. An explicitly-stored empty byte slice is also returned as nil;
// callers that need to distinguish "no account" from "account with empty
// code" should consult GetCodeHash instead.
func (k *Keeper) GetCode(ctx sdk.Context, addr common.Address) []byte {
	code := k.PrefixStore(ctx, types.CodeKeyPrefix).Get(addr[:])
	if len(code) == 0 {
		return nil
	}
	return code
}

// SetCode writes addr's bytecode and the derived code-size and code-hash
// indexes. Three values are persisted to avoid loading the full bytecode
// for size/hash lookups.
//
// Side effect: unless `code` is an EIP-7702 delegation designator, SetCode
// also installs an address mapping between addr and its cast Sei address
// (if one does not already exist). This is what binds a newly-deployed
// contract to a Sei account; delegations are deliberately excluded because
// the delegating account already has its own pubkey-derived Sei address
// and creating a cast mapping would conflict with it.
//
// Calling SetCode with nil/empty code is permitted and still creates the
// address mapping — it is the normal path for marking a contract account
// as existing without bytecode (e.g. CREATE before code deployment).
func (k *Keeper) SetCode(ctx sdk.Context, addr common.Address, code []byte) {
	if code == nil {
		code = []byte{}
	}
	key := addr[:]

	k.PrefixStore(ctx, types.CodeKeyPrefix).Set(key, code)

	sizeBz := make([]byte, codeSizeEncodedLen)
	binary.BigEndian.PutUint64(sizeBz, uint64(len(code)))
	k.PrefixStore(ctx, types.CodeSizeKeyPrefix).Set(key, sizeBz)

	hash := crypto.Keccak256Hash(code)
	k.PrefixStore(ctx, types.CodeHashKeyPrefix).Set(key, hash[:])

	// EIP-7702 delegations are stored verbatim but must NOT create a
	// cast-address mapping — see function doc.
	if _, isDelegation := ethtypes.ParseDelegation(code); isDelegation {
		return
	}
	if _, alreadyMapped := k.GetSeiAddress(ctx, addr); !alreadyMapped {
		k.SetAddressMapping(ctx, k.GetSeiAddressOrDefault(ctx, addr), addr)
	}
}

// GetCodeHash returns addr's code hash following Ethereum's three-way
// account semantics:
//
//   - Account does not exist (no code, no balance, no nonce) → Hash{}.
//   - Account exists but has no code (balance > 0 or nonce > 0) → EmptyCodeHash.
//   - Account has code → Keccak256(code).
//
// The distinction matters: EXTCODEHASH returning Hash{} signals a
// nonexistent account to the EVM, while EmptyCodeHash signals an existing
// EOA or empty contract. See `TestGetCodeHashWithNonceButZeroBalance`.
func (k *Keeper) GetCodeHash(ctx sdk.Context, addr common.Address) common.Hash {
	if bz := k.PrefixStore(ctx, types.CodeHashKeyPrefix).Get(addr[:]); bz != nil {
		return common.BytesToHash(bz)
	}

	balance := k.GetBalance(ctx, k.GetSeiAddressOrDefault(ctx, addr))
	if balance.Cmp(utils.Big0) == 0 && k.GetNonce(ctx, addr) == 0 {
		return common.Hash{}
	}
	return ethtypes.EmptyCodeHash
}

// GetCodeSize returns the byte length of addr's code. Reads the cached
// size index rather than loading the full bytecode.
func (k *Keeper) GetCodeSize(ctx sdk.Context, addr common.Address) int {
	bz := k.PrefixStore(ctx, types.CodeSizeKeyPrefix).Get(addr[:])
	if bz == nil {
		return 0
	}
	return int(binary.BigEndian.Uint64(bz)) //nolint:gosec // bounded by code size
}

// IterateAllCode walks every (address, code) pair in the code store.
// The callback returns `stop=true` to halt iteration early.
func (k *Keeper) IterateAllCode(ctx sdk.Context, cb func(addr common.Address, code []byte) (stop bool)) {
	iter := prefix.NewStore(ctx.KVStore(k.storeKey), types.CodeKeyPrefix).Iterator(nil, nil)
	defer func() { _ = iter.Close() }()
	for ; iter.Valid(); iter.Next() {
		if cb(common.BytesToAddress(iter.Key()), iter.Value()) {
			return
		}
	}
}
