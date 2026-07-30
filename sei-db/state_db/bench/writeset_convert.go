package bench

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	ethcrypto "github.com/ethereum/go-ethereum/crypto"
)

// This file converts a debug_traceCall prestateTracer diffMode result
// ({"pre": {...}, "post": {...}}) into a WriteSet for replay.
//
// Mapping rules (v1):
//   - storage slots present in post           -> storage write with the post value
//   - storage slots present in pre, not post  -> storage delete (slot zeroed)
//   - nonce changed                           -> nonce write (8-byte big-endian)
//   - code changed                            -> code write + codehash write
//     (keccak256 of the code) + codesize raw write (0x09||addr, 8-byte length),
//     mirroring x/evm's deploy path
//   - account present in pre, absent in post (SELFDESTRUCT) -> deletes for the
//     account's nonce, code, codehash, and codesize keys, plus the storage
//     deletes from the rule above. Only slots present in pre can be deleted;
//     a real account wipe also removes slots the trace never touched, so
//     self-destruct replays are a lower bound on the true delete volume.
//   - balance changes are bank-module writes and are NOT converted; they are
//     counted in SkippedBalanceChanges so callers can see what was dropped
//     (a removed account's balance zeroing is counted the same way)
//
// Known v1 fidelity gap: deploying to a previously-unassociated address also
// writes the Sei<->EVM address mapping (two raw keys, 0x01||evm and 0x02||sei)
// and creates a Sei account, via x/evm's SetCode -> SetAddressMapping path. Those
// writes are NOT emitted here because a prestate trace does not reveal the prior
// association state, so we cannot tell whether the mapping write actually fired
// (the same reason balance changes are skipped). New-contract deploy replays
// therefore slightly undercount apply/commit cost; emitting them conditionally
// is left to a future revision.
//
// Addresses and slots are emitted in sorted order so conversion output is
// deterministic for a given trace.

// codeSizeKeyPrefix mirrors x/evm/types.CodeSizeKeyPrefix. It routes to the
// legacy key family, which the sei-db keys package intentionally does not
// enumerate, so the byte is duplicated here the same way keys/evm.go
// duplicates the other prefixes.
var codeSizeKeyPrefix = []byte{0x09}

// prestateAccount is one account entry in a prestateTracer result.
type prestateAccount struct {
	Balance string            `json:"balance,omitempty"`
	Nonce   *uint64           `json:"nonce,omitempty"`
	Code    string            `json:"code,omitempty"`
	Storage map[string]string `json:"storage,omitempty"`
}

// prestateDiff is the diffMode payload of a prestateTracer trace.
type prestateDiff struct {
	Pre  map[string]prestateAccount `json:"pre"`
	Post map[string]prestateAccount `json:"post"`
}

// ConvertResult carries the converted write set plus conversion statistics.
type ConvertResult struct {
	WriteSet *WriteSet
	// SkippedBalanceChanges counts balance changes that were not converted
	// because balances live in the bank module (out of v1 scope): accounts
	// with a post-state balance, plus removed accounts whose pre-state
	// balance was zeroed.
	SkippedBalanceChanges int
}

// ConvertPrestateDiffFile reads a prestateTracer diffMode JSON file (either
// the raw {"pre","post"} object or a JSON-RPC response with that object under
// "result") and converts it into a single-block WriteSet.
func ConvertPrestateDiffFile(path string) (*ConvertResult, error) {
	data, err := os.ReadFile(path) //nolint:gosec // benchmark input path supplied by the operator
	if err != nil {
		return nil, fmt.Errorf("read trace file: %w", err)
	}
	return ConvertPrestateDiff(data)
}

// ConvertPrestateDiff converts prestateTracer diffMode JSON bytes into a
// single-block WriteSet.
func ConvertPrestateDiff(data []byte) (*ConvertResult, error) {
	var rpcEnvelope struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(data, &rpcEnvelope); err == nil && len(rpcEnvelope.Result) > 0 {
		data = rpcEnvelope.Result
	}

	var diff prestateDiff
	if err := json.Unmarshal(data, &diff); err != nil {
		return nil, fmt.Errorf("parse prestate diff: %w", err)
	}
	if diff.Post == nil {
		return nil, fmt.Errorf("trace has no post state; was the tracer run with diffMode=true?")
	}

	result := &ConvertResult{}
	var writes []WriteSetEntry

	for _, addr := range sortedKeys(diff.Post) {
		post := diff.Post[addr]
		pre := diff.Pre[addr]

		writes = append(writes, convertStorage(addr, pre, post)...)

		if post.Nonce != nil {
			nonce := make([]byte, 8)
			binary.BigEndian.PutUint64(nonce, *post.Nonce)
			writes = append(writes, WriteSetEntry{
				Kind:    WriteKindNonce,
				Address: addr,
				Value:   hex.EncodeToString(nonce),
			})
		}

		if post.Code != "" && post.Code != pre.Code {
			codeWrites, err := convertCode(addr, post.Code)
			if err != nil {
				return nil, err
			}
			writes = append(writes, codeWrites...)
		}

		if post.Balance != "" {
			result.SkippedBalanceChanges++
		}
	}

	// Slots that were zeroed appear in pre but not post: emit deletes. Membership
	// is tested on the normalized (padded) slot key because the write pass
	// normalizes the post slot the same way; comparing raw hex could miss a match
	// when pre and post encode the same slot differently (padded vs unpadded, 0x
	// prefix, case), emitting a spurious delete that clobbers the write — deletes
	// are appended after writes, and both engines apply last-write-wins per key.
	for _, addr := range sortedKeys(diff.Pre) {
		pre := diff.Pre[addr]
		post, inPost := diff.Post[addr]
		postSlots := make(map[string]struct{}, len(post.Storage))
		for slot := range post.Storage {
			postSlots[padTo32(slot)] = struct{}{}
		}
		for _, slot := range sortedKeys(pre.Storage) {
			if _, stillSet := postSlots[padTo32(slot)]; !stillSet {
				writes = append(writes, WriteSetEntry{
					Kind:    WriteKindStorage,
					Address: addr,
					Slot:    padTo32(slot),
					Delete:  true,
				})
			}
		}
		if !inPost {
			removalWrites, err := convertAccountRemoval(addr, pre)
			if err != nil {
				return nil, err
			}
			writes = append(writes, removalWrites...)
			if pre.Balance != "" {
				result.SkippedBalanceChanges++
			}
		}
	}

	if len(writes) == 0 {
		return nil, fmt.Errorf("trace produced no convertible writes")
	}
	result.WriteSet = &WriteSet{Blocks: []WriteSetBlock{{Writes: writes}}}
	if err := result.WriteSet.Validate(); err != nil {
		return nil, fmt.Errorf("converted write set is invalid: %w", err)
	}
	return result, nil
}

// convertStorage emits writes for every slot present in the post state.
// Slots and values are padded to 32 bytes: x/evm writes fixed 32-byte values,
// and some tracers emit unpadded hex.
func convertStorage(addr string, _, post prestateAccount) []WriteSetEntry {
	writes := make([]WriteSetEntry, 0, len(post.Storage))
	for _, slot := range sortedKeys(post.Storage) {
		writes = append(writes, WriteSetEntry{
			Kind:    WriteKindStorage,
			Address: addr,
			Slot:    padTo32(slot),
			Value:   padTo32(post.Storage[slot]),
		})
	}
	return writes
}

// convertCode emits the code, codehash, and codesize writes that a contract
// deployment produces. It deliberately omits the Sei<->EVM address-mapping and
// account-creation writes that SetCode also performs for a previously
// unassociated address; see the fidelity-gap note in the file header.
func convertCode(addr, codeHex string) ([]WriteSetEntry, error) {
	code, err := hex.DecodeString(strings.TrimPrefix(codeHex, "0x"))
	if err != nil {
		return nil, fmt.Errorf("address %s: invalid code hex: %w", addr, err)
	}
	addrBytes, err := decodeHexField("address", addr, 20)
	if err != nil {
		return nil, err
	}
	size := make([]byte, 8)
	binary.BigEndian.PutUint64(size, uint64(len(code)))
	return []WriteSetEntry{
		{Kind: WriteKindCode, Address: addr, Value: hex.EncodeToString(code)},
		{Kind: WriteKindCodeHash, Address: addr, Value: hex.EncodeToString(ethcrypto.Keccak256(code))},
		{Kind: WriteKindRaw, Key: hex.EncodeToString(append(codeSizeKeyPrefix, addrBytes...)), Value: hex.EncodeToString(size)},
	}, nil
}

// convertAccountRemoval emits the account-level deletes for an address that is
// present in pre but absent from post (the diffMode shape a SELFDESTRUCT
// produces): nonce, and — when the account had code — code, codehash, and
// codesize. Storage-slot deletes are handled by the caller's delete pass; see
// the file header for why slots absent from pre cannot be deleted.
func convertAccountRemoval(addr string, pre prestateAccount) ([]WriteSetEntry, error) {
	var writes []WriteSetEntry
	if pre.Nonce != nil {
		writes = append(writes, WriteSetEntry{Kind: WriteKindNonce, Address: addr, Delete: true})
	}
	if pre.Code != "" {
		addrBytes, err := decodeHexField("address", addr, 20)
		if err != nil {
			return nil, err
		}
		writes = append(writes,
			WriteSetEntry{Kind: WriteKindCode, Address: addr, Delete: true},
			WriteSetEntry{Kind: WriteKindCodeHash, Address: addr, Delete: true},
			WriteSetEntry{Kind: WriteKindRaw, Key: hex.EncodeToString(append(codeSizeKeyPrefix, addrBytes...)), Delete: true},
		)
	}
	return writes, nil
}

// padTo32 left-pads a hex slot to 32 bytes, tolerating a 0x prefix. Some
// tracers emit unpadded slot keys; store keys are always 32 bytes.
func padTo32(slot string) string {
	trimmed := strings.TrimPrefix(slot, "0x")
	if len(trimmed) >= 64 {
		return trimmed
	}
	return strings.Repeat("0", 64-len(trimmed)) + trimmed
}

// sortedKeys returns the map's keys in sorted order for deterministic output.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
