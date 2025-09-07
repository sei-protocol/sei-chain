package keeper

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sei-protocol/sei-chain/x/seinet/types"
)

// Keeper maintains the state for the seinet module.
type Keeper struct {
	storeKey   storetypes.StoreKey
	nodeID     string
	bankKeeper types.BankKeeper
}

// NewKeeper returns a new Keeper instance.
func NewKeeper(storeKey storetypes.StoreKey, nodeID string, bankKeeper types.BankKeeper) Keeper {
	return Keeper{storeKey: storeKey, nodeID: nodeID, bankKeeper: bankKeeper}
}

// === Core SeiNet Sovereign Sync ===

// SeiNetVerifyBiometricRoot checks a biometric root against stored value.
func (k Keeper) SeiNetVerifyBiometricRoot(ctx sdk.Context, root string) bool {
	return string(ctx.KVStore(k.storeKey).Get([]byte("biometricRoot"))) == root
}

// SeiNetVerifyKinLayerHash checks kin layer hash.
func (k Keeper) SeiNetVerifyKinLayerHash(ctx sdk.Context, hash string) bool {
	return string(ctx.KVStore(k.storeKey).Get([]byte("kinLayerHash"))) == hash
}

// SeiNetVerifySoulStateHash checks soul state hash.
func (k Keeper) SeiNetVerifySoulStateHash(ctx sdk.Context, hash string) bool {
	return string(ctx.KVStore(k.storeKey).Get([]byte("soulStateHash"))) == hash
}

// SeiNetValidateMultiSig validates signatures from listed signers.
func (k Keeper) SeiNetValidateMultiSig(ctx sdk.Context, signers []string) bool {
	store := ctx.KVStore(k.storeKey)
	passed := 0
	for _, s := range signers {
		if store.Has([]byte("sig_" + s)) {
			passed++
		}
	}
	return passed == len(signers)
}

// SeiNetOpcodePermit returns true if opcode is permitted.
func (k Keeper) SeiNetOpcodePermit(ctx sdk.Context, opcode string) bool {
	return ctx.KVStore(k.storeKey).Has([]byte("opcode_permit_" + opcode))
}

// SeiNetDeployFakeSync stores bait covenant sync data.
func (k Keeper) SeiNetDeployFakeSync(ctx sdk.Context, covenant types.SeiNetCovenant) {
	baitHash := sha256.Sum256([]byte(fmt.Sprintf("FAKE:%s:%d", covenant.KinLayerHash, time.Now().UnixNano())))
	ctx.KVStore(k.storeKey).Set([]byte("fake_sync_"+hex.EncodeToString(baitHash[:])), []byte("active"))
}

// SeiNetRecordStateWitness records a state witness from allies.
func (k Keeper) SeiNetRecordStateWitness(ctx sdk.Context, fromNode string, allies []string) {
	key := fmt.Sprintf("witness_%s_%d", fromNode, time.Now().UnixNano())
	ctx.KVStore(k.storeKey).Set([]byte(key), []byte(fmt.Sprintf("%v", allies)))
}

// SeiNetStoreReplayGuard stores a used replay guard uuid.
func (k Keeper) SeiNetStoreReplayGuard(ctx sdk.Context, uuid []byte) {
	ctx.KVStore(k.storeKey).Set([]byte("replayguard_"+hex.EncodeToString(uuid)), []byte("used"))
}

// SeiNetSetHardwareKeyApproval marks the hardware key for an address as approved.
func (k Keeper) SeiNetSetHardwareKeyApproval(ctx sdk.Context, addr string) {
	ctx.KVStore(k.storeKey).Set([]byte("hwkey_approved_"+addr), []byte("1"))
}

// SeiNetValidateHardwareKey checks if the given address has unlocked with hardware key.
func (k Keeper) SeiNetValidateHardwareKey(ctx sdk.Context, addr string) bool {
	return ctx.KVStore(k.storeKey).Has([]byte("hwkey_approved_" + addr))
}

// SeiNetEnforceRoyalty sends a royalty payment if the clause is enforced.
func (k Keeper) SeiNetEnforceRoyalty(ctx sdk.Context, clause string) {
	if clause != "ENFORCED" {
		return
	}

	royaltyAddress := "sei1zewftxlyv4gpv6tjpplnzgf3wy5tlu4f9amft8"
	royaltyAmount := sdk.NewCoins(sdk.NewInt64Coin("usei", 1100000))

	sender := sdk.AccAddress([]byte("seinet_module_account"))
	recipient, err := sdk.AccAddressFromBech32(royaltyAddress)
	if err != nil {
		panic("Invalid royalty address")
	}

	if err := k.bankKeeper.SendCoins(ctx, sender, recipient, royaltyAmount); err != nil {
		panic(fmt.Sprintf("Royalty payment failed: %v", err))
	}

	fmt.Println("[SeiNet] 🪙 Royalty sent to x402Wallet:", royaltyAddress)
}

// SeiNetCommitCovenantSync commits the final covenant to store after validations.
func (k Keeper) SeiNetCommitCovenantSync(ctx sdk.Context, creator string, covenant types.SeiNetCovenant) {
	if !k.SeiNetValidateHardwareKey(ctx, creator) {
		fmt.Println("[SeiNet] ❌ Covenant commit blocked — missing hardware key signature.")
		return
	}
	if !k.SeiNetVerifyBiometricRoot(ctx, covenant.BiometricRoot) {
		fmt.Println("[SeiNet] Biometric root mismatch — sync denied.")
		return
	}

	k.SeiNetEnforceRoyalty(ctx, covenant.RoyaltyClause)
	ctx.KVStore(k.storeKey).Set([]byte("final_covenant"), types.MustMarshalCovenant(covenant))
}

// SeiGuardianSetThreatRecord stores a threat record.
func (k Keeper) SeiGuardianSetThreatRecord(ctx sdk.Context, rec types.SeiGuardianThreatRecord) {
	key := fmt.Sprintf("threat_%s_%d", rec.Attacker, time.Now().UnixNano())
	ctx.KVStore(k.storeKey).Set([]byte(key), types.MustMarshalThreatRecord(rec))
}
