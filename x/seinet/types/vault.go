package types

import (
	"encoding/json"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Vault storage keys.
var (
	// VaultKeyPrefix stores individual vaults keyed by their id.
	VaultKeyPrefix = []byte{0x31}
	// VaultSequenceKey keeps track of the incremental vault id counter.
	VaultSequenceKey = []byte{0x32}
)

// VaultKinKey captures the rotating epoch key for withdrawal permissions.
type VaultKinKey struct {
	Epoch uint64 `json:"epoch"`
	Key   []byte `json:"key"`
}

// VaultSoulSigil binds an address to its off-chain NFT soul sigil hash.
type VaultSoulSigil struct {
	Holder    string `json:"holder"`
	SigilHash string `json:"sigil_hash"`
}

// VaultHoloPresence tracks the last live presence attestation.
type VaultHoloPresence struct {
	LastHeartbeatUnix int64  `json:"last_heartbeat_unix"`
	LastProof         string `json:"last_proof"`
	LastBlockHeight   int64  `json:"last_block_height"`
}

// VaultPayword keeps settlement metadata for post-withdrawal flows.
type VaultPayword struct {
	PendingAmount        sdk.Coins `json:"pending_amount"`
	SettlementCount      uint64    `json:"settlement_count"`
	LastSettlementHeight int64     `json:"last_settlement_height"`
	LastSignature        []byte    `json:"last_signature"`
	Active               bool      `json:"active"`
}

// SeiVault embeds the spiritual state for an account bound vault.
type SeiVault struct {
	Id                uint64            `json:"id"`
	Owner             string            `json:"owner"`
	Balance           sdk.Coins         `json:"balance"`
	KinKey            VaultKinKey       `json:"kin_key"`
	SoulSigil         VaultSoulSigil    `json:"soul_sigil"`
	HoloPresence      VaultHoloPresence `json:"holo_presence"`
	Payword           VaultPayword      `json:"payword"`
	Mood              string            `json:"mood"`
	Entropy           uint64            `json:"entropy"`
	LastIntrospection int64             `json:"last_introspection"`
}

// VaultIntrospection captures the outcome of an introspection pass.
type VaultIntrospection struct {
	Vault            SeiVault `json:"vault"`
	Observations     []string `json:"observations"`
	PaywordTriggered bool     `json:"payword_triggered"`
}

// PaywordRoyaltyClauseEnforced represents the clause passed to the royalty pipeline.
const PaywordRoyaltyClauseEnforced = "ENFORCED"

// MustMarshalVault marshals a vault or panics.
func MustMarshalVault(v SeiVault) []byte {
	bz, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return bz
}

// MustUnmarshalVault unmarshals a vault or panics.
func MustUnmarshalVault(bz []byte) SeiVault {
	var v SeiVault
	if err := json.Unmarshal(bz, &v); err != nil {
		panic(err)
	}
	return v
}

// MustMarshalIntrospection marshals an introspection record or panics.
func MustMarshalIntrospection(introspection VaultIntrospection) []byte {
	bz, err := json.Marshal(introspection)
	if err != nil {
		panic(err)
	}
	return bz
}

// MustUnmarshalIntrospection unmarshals an introspection record or panics.
func MustUnmarshalIntrospection(bz []byte) VaultIntrospection {
	var introspection VaultIntrospection
	if err := json.Unmarshal(bz, &introspection); err != nil {
		panic(err)
	}
	return introspection
}

// ZeroVault returns a zero-value vault placeholder with coins initialized.
func ZeroVault() SeiVault {
	return SeiVault{
		Balance: sdk.NewCoins(),
		Payword: VaultPayword{PendingAmount: sdk.NewCoins()},
	}
}
