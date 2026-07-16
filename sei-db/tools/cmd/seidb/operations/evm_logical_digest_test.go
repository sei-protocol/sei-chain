package operations

import (
	"encoding/binary"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
	"github.com/stretchr/testify/require"
)

func TestSemanticMemiavlDigestMatchesTranslatorForCoreEVMKeys(t *testing.T) {
	rawPairs := coreEVMRawPairs()

	translatorDigest := evmDigest{}
	tr := flatkv.NewImportTranslator(0)
	pairs, err := tr.Translate(&proto.NamedChangeSet{
		Name:      keys.EVMStoreKey,
		Changeset: proto.ChangeSet{Pairs: rawPairs},
	})
	require.NoError(t, err)
	for _, p := range pairs {
		require.NoError(t, translatorDigest.consume(p.Key, p.Value))
	}
	for _, p := range tr.Finalize() {
		require.NoError(t, translatorDigest.consume(p.Key, p.Value))
	}

	semanticDigest := evmDigest{}
	accounts := make(map[string]*semanticAccountDigestState)
	for _, p := range rawPairs {
		require.NoError(t, semanticDigest.consumeSemanticMemiavlLeaf(accounts, p.Key, p.Value))
	}
	semanticDigest.finalizeSemanticAccounts(accounts)

	require.Equal(t, translatorDigest.account, semanticDigest.account)
	require.Equal(t, translatorDigest.code, semanticDigest.code)
	require.Equal(t, translatorDigest.storage, semanticDigest.storage)
	require.Equal(t, translatorDigest.legacy, semanticDigest.legacy)
}

func TestSemanticMemiavlInspectMatchesTranslatorForCoreEVMKeys(t *testing.T) {
	rawPairs := coreEVMRawPairs()

	for _, bucket := range flatkvBucketOrder {
		t.Run(bucket, func(t *testing.T) {
			translatorInspect := newTestInspectAccumulator(bucket)
			tr := flatkv.NewImportTranslator(0)
			pairs, err := tr.Translate(&proto.NamedChangeSet{
				Name:      keys.EVMStoreKey,
				Changeset: proto.ChangeSet{Pairs: rawPairs},
			})
			require.NoError(t, err)
			for _, p := range pairs {
				require.NoError(t, translatorInspect.consume(p.Key, p.Value))
			}
			for _, p := range tr.Finalize() {
				require.NoError(t, translatorInspect.consume(p.Key, p.Value))
			}

			semanticInspect := newTestInspectAccumulator(bucket)
			accounts := make(map[string]*semanticAccountDigestState)
			consume := func(bucket string, physKey, logical, _ []byte) {
				semanticInspect.consumeLogical(bucket, physKey, logical, "")
			}
			for _, p := range rawPairs {
				require.NoError(t, consumeSemanticMemiavlLeaf(accounts, p.Key, p.Value, consume, "inspect"))
			}
			finalizeSemanticAccounts(accounts, consume)

			require.Equal(t, translatorInspect.matched, semanticInspect.matched)
			require.Equal(t, translatorInspect.shards, semanticInspect.shards)
		})
	}
}

func TestInspectMemiavlRejectsUnknownNormalizationBeforeOpeningSnapshot(t *testing.T) {
	cmd := EvmLogicalDigestCmd()
	require.NoError(t, cmd.Flags().Set("backend", "memiavl"))
	require.NoError(t, cmd.Flags().Set("db-dir", "/path/that/should/not/be/opened"))
	require.NoError(t, cmd.Flags().Set("inspect-bucket", flatkvBucketStorage))
	require.NoError(t, cmd.Flags().Set("memiavl-normalization", "bogus"))

	err := runEvmLogicalDigest(cmd, nil)
	require.ErrorContains(t, err, `unknown --memiavl-normalization "bogus"`)
}

func coreEVMRawPairs() []*proto.KVPair {
	addr := bytesOfLen(keys.AddressLen, 0x42)
	slot := bytesOfLen(32, 0x07)
	storageKeyBytes := append(append([]byte{}, addr...), slot...)
	codeHash := bytesOfLen(32, 0xAB)
	storageValue := bytesOfLen(32, 0x2A)
	code := []byte{0x60, 0x2A, 0x60, 0x00}
	legacyKey := append([]byte{0x09}, addr...)
	legacyValue := []byte{0xAA, 0xBB}

	return []*proto.KVPair{
		{Key: keys.BuildEVMKey(keys.EVMKeyNonce, addr), Value: nonceBytes(7)},
		{Key: keys.BuildEVMKey(keys.EVMKeyCodeHash, addr), Value: codeHash},
		{Key: keys.BuildEVMKey(keys.EVMKeyStorage, storageKeyBytes), Value: storageValue},
		{Key: keys.BuildEVMKey(keys.EVMKeyCode, addr), Value: code},
		{Key: legacyKey, Value: legacyValue},
		// Both paths should treat these as delete-equivalent and omit them.
		{Key: keys.BuildEVMKey(keys.EVMKeyStorage, append(append([]byte{}, addr...), bytesOfLen(32, 0x08)...)), Value: make([]byte, 32)},
		{Key: keys.BuildEVMKey(keys.EVMKeyCode, bytesOfLen(keys.AddressLen, 0x99)), Value: nil},
	}
}

func newTestInspectAccumulator(bucket string) *inspectAccumulator {
	return &inspectAccumulator{
		inspectBucket: bucket,
		shards:        make(map[string]*digestBucket),
	}
}

func bytesOfLen(n int, fill byte) []byte {
	bz := make([]byte, n)
	for i := range bz {
		bz[i] = fill
	}
	return bz
}

func nonceBytes(n uint64) []byte {
	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, n)
	return bz
}

// TestLegacyForCompareOmitsMigrationMarkerRows pins the marker adjustment that
// lets a memiavl-only node, a completed node (carrying the migration-version
// marker), and an in-progress node (carrying the migration-boundary cursor) all
// produce the same legacy digest for identical EVM state. Both FlatKV-only
// MigrationStore rows are folded into the legacy bucket during the scan but must
// be XORed back out for the final cross-backend comparison.
func TestLegacyForCompareOmitsMigrationMarkerRows(t *testing.T) {
	legacyKey := append([]byte{0x09}, bytesOfLen(keys.AddressLen, 0x33)...)
	legacyVal := vtype.NewLegacyData().SetBlockHeight(10).SetValue([]byte{0xDE, 0xAD}).Serialize()
	versionVal := vtype.NewLegacyData().SetBlockHeight(20).SetValue([]byte{0x01}).Serialize()
	boundaryVal := vtype.NewLegacyData().SetBlockHeight(30).SetValue([]byte{0x02, 0x03}).Serialize()

	// memiavl-only / clean node: only the plain legacy row.
	clean := evmDigest{}
	require.NoError(t, clean.consume(legacyKey, legacyVal))

	// completed node: plain row + migration-version marker.
	completed := evmDigest{}
	require.NoError(t, completed.consume(legacyKey, legacyVal))
	require.NoError(t, completed.consume(migrationVersionPhysKey, versionVal))
	require.True(t, completed.migrationVersionFound)

	// in-progress node: plain row + migration-boundary cursor.
	inProgress := evmDigest{}
	require.NoError(t, inProgress.consume(legacyKey, legacyVal))
	require.NoError(t, inProgress.consume(migrationBoundaryPhysKey, boundaryVal))
	require.True(t, inProgress.migrationBoundaryFound)

	// Raw legacy buckets differ because each folds in its marker row.
	require.NotEqual(t, clean.legacy, completed.legacy)
	require.NotEqual(t, clean.legacy, inProgress.legacy)

	// After the marker adjustment all three agree (digest and count).
	cleanAcc, cleanCount := clean.legacyForCompare()
	compAcc, compCount := completed.legacyForCompare()
	progAcc, progCount := inProgress.legacyForCompare()
	require.Equal(t, cleanAcc, compAcc, "completed node must match clean after omitting migration-version")
	require.Equal(t, cleanCount, compCount)
	require.Equal(t, cleanAcc, progAcc, "in-progress node must match clean after omitting migration-boundary")
	require.Equal(t, cleanCount, progCount)
}

func TestCompositeAccountMergeCombinesFlatKVAndMemiavlFragments(t *testing.T) {
	addr := bytesOfLen(keys.AddressLen, 0x55)
	codeHash := bytesOfLen(32, 0xCC)
	balance := bytesOfLen(32, 0x11)
	bal, err := vtype.ParseBalance(balance)
	require.NoError(t, err)

	flatKVAccount := vtype.NewAccountData().SetBlockHeight(123).SetBalance(bal).SetNonce(9)
	accounts := make(map[string]*semanticAccountDigestState)
	require.NoError(t, mergeCompositeFlatKVAccount(accounts, ktype.EVMPhysicalKey(keys.EVMKeyNonce, addr), flatKVAccount.Serialize()))

	composite := evmDigest{}
	require.NoError(t, composite.consumeSemanticMemiavlLeaf(accounts, keys.BuildEVMKey(keys.EVMKeyCodeHash, addr), codeHash))
	composite.finalizeSemanticAccounts(accounts)

	expected := evmDigest{}
	codeHashParsed, err := vtype.ParseCodeHash(codeHash)
	require.NoError(t, err)
	fullAccount := vtype.NewAccountData().SetBlockHeight(456).SetBalance(bal).SetNonce(9).SetCodeHash(codeHashParsed)
	require.NoError(t, expected.consume(ktype.EVMPhysicalKey(keys.EVMKeyNonce, addr), fullAccount.Serialize()))

	require.Equal(t, expected.account, composite.account)
}
