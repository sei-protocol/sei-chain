package keeper

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sei-protocol/sei-chain/x/seinet/types"
)

// CreateVault instantiates a new SeiVault with mood, entropy and soul sigil context.
func (k Keeper) CreateVault(ctx sdk.Context, owner sdk.AccAddress, soulSigilHash string) (types.SeiVault, error) {
	if owner.Empty() {
		return types.SeiVault{}, fmt.Errorf("owner address is required")
	}
	if strings.TrimSpace(soulSigilHash) == "" {
		return types.SeiVault{}, fmt.Errorf("soul sigil hash must be provided")
	}

	vault := types.ZeroVault()
	vault.Id = k.getNextVaultID(ctx)
	vault.Owner = owner.String()
	vault.SoulSigil = types.VaultSoulSigil{Holder: owner.String(), SigilHash: soulSigilHash}
	vault.KinKey = types.VaultKinKey{Epoch: uint64(ctx.BlockHeight()), Key: k.deriveKinKey(ctx, owner, vault.Id, soulSigilHash)}
	vault.Mood = "nascent"
	vault.Entropy = uint64(len(soulSigilHash))
	vault.HoloPresence = types.VaultHoloPresence{LastHeartbeatUnix: ctx.BlockTime().Unix(), LastProof: "initialization", LastBlockHeight: ctx.BlockHeight()}
	vault.LastIntrospection = ctx.BlockHeight()

	k.setVault(ctx, vault)
	return vault, nil
}

// DepositToVault moves funds into the module vault and updates the vault state.
func (k Keeper) DepositToVault(ctx sdk.Context, vaultID uint64, depositor sdk.AccAddress, amount sdk.Coins) (types.SeiVault, error) {
	if depositor.Empty() {
		return types.SeiVault{}, fmt.Errorf("depositor address is required")
	}
	if !amount.IsValid() || amount.Empty() {
		return types.SeiVault{}, fmt.Errorf("deposit amount must be positive")
	}

	vault, found := k.GetVault(ctx, vaultID)
	if !found {
		return types.SeiVault{}, fmt.Errorf("vault %d not found", vaultID)
	}

	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, depositor, types.SeinetVaultAccount, amount); err != nil {
		return types.SeiVault{}, fmt.Errorf("failed to escrow deposit: %w", err)
	}

	vault.Balance = vault.Balance.Add(amount...)
	vault.Payword.PendingAmount = vault.Payword.PendingAmount.Add(amount...)
	vault.Payword.Active = true
	vault.Mood, vault.Entropy = k.updateMoodAndEntropy(ctx, vault, amount, "deposit")

	k.setVault(ctx, vault)
	return vault, nil
}

// IntrospectVault refreshes holo presence, rotates kin keys and prepares payword state.
func (k Keeper) IntrospectVault(ctx sdk.Context, vaultID uint64, presenceProof string, paywordSig []byte) (types.VaultIntrospection, error) {
	if strings.TrimSpace(presenceProof) == "" {
		return types.VaultIntrospection{}, fmt.Errorf("presence proof must be provided")
	}

	vault, found := k.GetVault(ctx, vaultID)
	if !found {
		return types.VaultIntrospection{}, fmt.Errorf("vault %d not found", vaultID)
	}

	observations := []string{}

	vault.HoloPresence.LastProof = presenceProof
	vault.HoloPresence.LastHeartbeatUnix = ctx.BlockTime().Unix()
	vault.HoloPresence.LastBlockHeight = ctx.BlockHeight()
	observations = append(observations, "holo-presence attested")

	owner, err := sdk.AccAddressFromBech32(vault.Owner)
	if err != nil {
		return types.VaultIntrospection{}, fmt.Errorf("invalid stored vault owner: %w", err)
	}

	kinKey := k.deriveKinKey(ctx, owner, vault.Id, presenceProof)
	vault.KinKey = types.VaultKinKey{Epoch: uint64(ctx.BlockHeight()), Key: kinKey}
	observations = append(observations, fmt.Sprintf("kin-key rotated@%d", ctx.BlockHeight()))

	if len(paywordSig) > 0 {
		vault.Payword.LastSignature = paywordSig
		vault.Payword.Active = true
		observations = append(observations, "payword signature received")
	}

	vault.LastIntrospection = ctx.BlockHeight()
	vault.Mood, vault.Entropy = k.updateMoodAndEntropy(ctx, vault, sdk.NewCoins(), presenceProof)

	paywordTriggered := len(paywordSig) > 0 && !vault.Payword.PendingAmount.IsZero()

	k.setVault(ctx, vault)

	return types.VaultIntrospection{
		Vault:            vault,
		Observations:     observations,
		PaywordTriggered: paywordTriggered,
	}, nil
}

// ExecutePaywordSettlement settles pending payword withdrawals and clears pending state.
func (k Keeper) ExecutePaywordSettlement(ctx sdk.Context, vaultID uint64) (types.SeiVault, error) {
	vault, found := k.GetVault(ctx, vaultID)
	if !found {
		return types.SeiVault{}, fmt.Errorf("vault %d not found", vaultID)
	}
	if vault.Payword.PendingAmount.Empty() {
		return vault, nil
	}

	owner, err := sdk.AccAddressFromBech32(vault.Owner)
	if err != nil {
		return types.SeiVault{}, fmt.Errorf("invalid stored vault owner: %w", err)
	}

	if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.SeinetVaultAccount, owner, vault.Payword.PendingAmount); err != nil {
		return types.SeiVault{}, fmt.Errorf("payword settlement failed: %w", err)
	}

	updatedBalance, err := vault.Balance.Sub(vault.Payword.PendingAmount...)
	if err != nil {
		return types.SeiVault{}, fmt.Errorf("failed to update vault balance: %w", err)
	}
	vault.Balance = updatedBalance
	vault.Payword.PendingAmount = sdk.NewCoins()
	vault.Payword.SettlementCount++
	vault.Payword.LastSettlementHeight = ctx.BlockHeight()

	k.setVault(ctx, vault)
	return vault, nil
}

// GetVault retrieves a vault by id.
func (k Keeper) GetVault(ctx sdk.Context, id uint64) (types.SeiVault, bool) {
	store := ctx.KVStore(k.storeKey)
	key := make([]byte, len(types.VaultKeyPrefix)+8)
	copy(key, types.VaultKeyPrefix)
	binary.BigEndian.PutUint64(key[len(types.VaultKeyPrefix):], id)
	bz := store.Get(key)
	if len(bz) == 0 {
		return types.SeiVault{}, false
	}
	return types.MustUnmarshalVault(bz), true
}

func (k Keeper) setVault(ctx sdk.Context, vault types.SeiVault) {
	store := ctx.KVStore(k.storeKey)
	key := make([]byte, len(types.VaultKeyPrefix)+8)
	copy(key, types.VaultKeyPrefix)
	binary.BigEndian.PutUint64(key[len(types.VaultKeyPrefix):], vault.Id)
	store.Set(key, types.MustMarshalVault(vault))
}

func (k Keeper) getNextVaultID(ctx sdk.Context) uint64 {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get(types.VaultSequenceKey)
	var id uint64
	if len(bz) != 0 {
		id = binary.BigEndian.Uint64(bz)
	}
	id++
	nbz := make([]byte, 8)
	binary.BigEndian.PutUint64(nbz, id)
	store.Set(types.VaultSequenceKey, nbz)
	return id
}

func (k Keeper) deriveKinKey(ctx sdk.Context, owner sdk.AccAddress, vaultID uint64, seed string) []byte {
	hasher := sha256.New()
	hasher.Write(owner.Bytes())
	height := make([]byte, 8)
	binary.BigEndian.PutUint64(height, uint64(ctx.BlockHeight()))
	hasher.Write(height)
	hasher.Write([]byte(seed))
	hasher.Write([]byte(fmt.Sprintf("#%d", vaultID)))
	digest := hasher.Sum(nil)
	return []byte(hex.EncodeToString(digest))
}

func (k Keeper) updateMoodAndEntropy(ctx sdk.Context, vault types.SeiVault, delta sdk.Coins, signal string) (string, uint64) {
	entropy := vault.Entropy + uint64(len(signal))
	if !delta.Empty() {
		entropy += uint64(delta.Len())
	}

	mood := vault.Mood
	if mood == "" {
		mood = "nascent"
	}

	switch {
	case !delta.Empty():
		mood = "gratified"
	case strings.Contains(strings.ToLower(signal), "scan"):
		mood = "attentive"
	default:
		if ctx.BlockTime().Unix()%2 == 0 {
			mood = "curious"
		}
	}

	return mood, entropy
}
