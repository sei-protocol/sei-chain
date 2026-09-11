package operations

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"
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
	require.Equal(t, translatorDigest.misc, semanticDigest.misc)
}

func TestSemanticMemiavlDigestReportsZeroCensus(t *testing.T) {
	liveZeroCodeHashAddr := bytesOfLen(keys.AddressLen, 0x21)
	zeroAccountAddr := bytesOfLen(keys.AddressLen, 0x22)
	zeroStorageAddr := bytesOfLen(keys.AddressLen, 0x23)
	zeroStorageSlot := bytesOfLen(32, 0x24)
	zeroStorageKey := append(append([]byte{}, zeroStorageAddr...), zeroStorageSlot...)
	// A plain EOA carries no code-hash row at all. It reads as absent on either backend, so it
	// must not be counted with the accounts whose stored all-zero row FlatKV normalizes away.
	liveNoCodeHashRowAddr := bytesOfLen(keys.AddressLen, 0x26)

	rawPairs := []*proto.KVPair{
		{Key: keys.BuildEVMKey(keys.EVMKeyNonce, liveZeroCodeHashAddr), Value: nonceBytes(7)},
		{Key: keys.BuildEVMKey(keys.EVMKeyCodeHash, liveZeroCodeHashAddr), Value: make([]byte, 32)},
		{Key: keys.BuildEVMKey(keys.EVMKeyNonce, zeroAccountAddr), Value: nonceBytes(0)},
		{Key: keys.BuildEVMKey(keys.EVMKeyCodeHash, zeroAccountAddr), Value: make([]byte, 32)},
		{Key: keys.BuildEVMKey(keys.EVMKeyCode, bytesOfLen(keys.AddressLen, 0x25)), Value: nil},
		{Key: keys.BuildEVMKey(keys.EVMKeyStorage, zeroStorageKey), Value: make([]byte, 32)},
		{Key: keys.BuildEVMKey(keys.EVMKeyNonce, liveNoCodeHashRowAddr), Value: nonceBytes(5)},
	}

	d := evmDigest{census: &evmZeroCensus{}}
	accounts := make(map[string]*semanticAccountDigestState)
	for _, p := range rawPairs {
		require.NoError(t, d.consumeSemanticMemiavlLeaf(accounts, p.Key, p.Value))
	}
	d.finalizeSemanticAccounts(accounts)

	require.Equal(t, &evmZeroCensus{
		ZeroAccounts:                    1,
		ZeroCodeHashRows:                2,
		LiveAccountsWithZeroCodeHashRow: 1,
		LiveAccountsWithoutCodeHashRow:  1,
		EmptyCodeValues:                 1,
		ZeroStorageSlots:                1,
	}, d.census)
	require.Equal(t, uint64(2), d.account.count, "only the two live accounts should remain in the digest")

	proseBuf, _ := captureDigestOutput(t, false)
	require.NoError(t, d.emit(testDigestContext()))
	require.Contains(t, proseBuf.String(), "zero_codehash_rows=2")
	require.Contains(t, proseBuf.String(), "live_accounts_with_zero_codehash_row=1")
	require.Contains(t, proseBuf.String(), "live_accounts_without_codehash_row=1")

	_, jsonBuf := captureDigestOutput(t, true)
	require.NoError(t, d.emit(testDigestContext()))
	var got evmDigestJSON
	require.NoError(t, json.Unmarshal(jsonBuf.Bytes(), &got))
	require.Equal(t, d.census, got.ZeroCensus)
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
				require.NoError(t, consumeSemanticMemiavlLeaf(accounts, p.Key, p.Value, consume, nil, "inspect"))
			}
			finalizeSemanticAccounts(accounts, consume, nil)

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
	balance := bytesOfLen(32, 0x5E)
	storageValue := bytesOfLen(32, 0x2A)
	code := []byte{0x60, 0x2A, 0x60, 0x00}
	miscKey := append([]byte{0x09}, addr...)
	miscValue := []byte{0xAA, 0xBB}

	return []*proto.KVPair{
		{Key: keys.BuildEVMKey(keys.EVMKeyNonce, addr), Value: nonceBytes(7)},
		{Key: keys.BuildEVMKey(keys.EVMKeyCodeHash, addr), Value: codeHash},
		{Key: keys.BuildEVMKey(keys.EVMKeyBalance, addr), Value: balance},
		{Key: keys.BuildEVMKey(keys.EVMKeyStorage, storageKeyBytes), Value: storageValue},
		{Key: keys.BuildEVMKey(keys.EVMKeyCode, addr), Value: code},
		{Key: miscKey, Value: miscValue},
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

// TestMiscForCompareOmitsMigrationMarkerRows pins the marker adjustment that
// lets a memiavl-only node, a completed node (carrying the migration-version
// marker), and an in-progress node (carrying the migration-boundary cursor) all
// produce the same misc digest for identical EVM state. Both FlatKV-only
// MigrationStore rows are folded into the misc bucket during the scan but must
// be XORed back out for the final cross-backend comparison.
func TestMiscForCompareOmitsMigrationMarkerRows(t *testing.T) {
	miscKey := append([]byte{0x09}, bytesOfLen(keys.AddressLen, 0x33)...)
	miscVal := vtype.NewMiscData().SetBlockHeight(10).SetValue([]byte{0xDE, 0xAD}).Serialize()
	versionVal := vtype.NewMiscData().SetBlockHeight(20).SetValue([]byte{0x01}).Serialize()
	boundaryVal := vtype.NewMiscData().SetBlockHeight(30).SetValue([]byte{0x02, 0x03}).Serialize()

	// memiavl-only / clean node: only the plain misc row.
	clean := evmDigest{}
	require.NoError(t, clean.consume(miscKey, miscVal))

	// completed node: plain row + migration-version marker.
	completed := evmDigest{}
	require.NoError(t, completed.consume(miscKey, miscVal))
	require.NoError(t, completed.consume(migrationVersionPhysKey, versionVal))
	require.True(t, completed.migrationVersionFound)

	// in-progress node: plain row + migration-boundary cursor.
	inProgress := evmDigest{}
	require.NoError(t, inProgress.consume(miscKey, miscVal))
	require.NoError(t, inProgress.consume(migrationBoundaryPhysKey, boundaryVal))
	require.True(t, inProgress.migrationBoundaryFound)

	// Raw misc buckets differ because each folds in its marker row.
	require.NotEqual(t, clean.misc, completed.misc)
	require.NotEqual(t, clean.misc, inProgress.misc)

	// After the marker adjustment all three agree (digest and count).
	cleanAcc, cleanCount := clean.miscForCompare()
	compAcc, compCount := completed.miscForCompare()
	progAcc, progCount := inProgress.miscForCompare()
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

// captureDigestOutput points the package output digestOut at buffers for one test and
// restores it afterwards. A non-nil jsonReport is what puts emit into JSON mode,
// so passing jsonMode here selects the same branch the --json flag selects.
func captureDigestOutput(t *testing.T, jsonMode bool) (prose, jsonReport *bytes.Buffer) {
	t.Helper()
	saved := digestOut
	t.Cleanup(func() { digestOut = saved })

	prose, jsonReport = &bytes.Buffer{}, &bytes.Buffer{}
	digestOut = digestSink{prose: prose}
	if jsonMode {
		digestOut.jsonReport = jsonReport
	}
	return prose, jsonReport
}

// digestOverCoreEVMKeys builds a digest with every bucket populated.
func digestOverCoreEVMKeys(t *testing.T) evmDigest {
	t.Helper()
	d := evmDigest{}
	accounts := make(map[string]*semanticAccountDigestState)
	for _, p := range coreEVMRawPairs() {
		require.NoError(t, d.consumeSemanticMemiavlLeaf(accounts, p.Key, p.Value))
	}
	d.finalizeSemanticAccounts(accounts)
	return d
}

func testDigestContext() digestPrintContext {
	return digestPrintContext{
		backend:         "memiavl",
		mode:            memiavlOpenModeSnapshot,
		dbDir:           "/data/state_commit/memiavl",
		source:          "snapshot-40000/evm",
		normalization:   memiavlNormSemantic,
		requestedHeight: 40000,
		version:         40000,
	}
}

// TestDigestJSONReportCarriesTheSameNumbersAsTheProse pins the claim that makes
// the JSON form safe to adopt: a caller that switches from scraping the text
// report to decoding the object reads the same values. Both forms are rendered
// from one report, so this fails the moment a number is computed twice.
func TestDigestJSONReportCarriesTheSameNumbersAsTheProse(t *testing.T) {
	d := digestOverCoreEVMKeys(t)
	ctx := testDigestContext()

	proseBuf, _ := captureDigestOutput(t, false)
	require.NoError(t, d.emit(ctx))
	text := proseBuf.String()

	_, jsonBuf := captureDigestOutput(t, true)
	require.NoError(t, d.emit(ctx))

	var got evmDigestJSON
	require.NoError(t, json.Unmarshal(jsonBuf.Bytes(), &got))

	for _, b := range []struct {
		label  string
		bucket evmDigestBucketJSON
	}{
		{"account", got.Account},
		{"code", got.Code},
		{"storage", got.Storage},
		{"misc", got.Misc},
	} {
		require.Contains(t, text,
			fmt.Sprintf("count=%d bucket_digest=%s", b.bucket.Count, b.bucket.Digest),
			"%s bucket disagrees between the two forms", b.label)
	}
	require.Contains(t, text, fmt.Sprintf("count=%d digest=%s", got.Final.Count, got.Final.Digest))

	// The context the reading was taken under travels with the numbers, so a
	// stored object still says which backend and height produced it.
	require.Equal(t, ctx.backend, got.Backend)
	require.Equal(t, ctx.version, got.Version)
	require.Equal(t, ctx.source, got.Source)

	// JSON mode keeps stdout to the object alone.
	require.NotContains(t, jsonBuf.String(), "EVM logical digest report")
}

// TestStdoutLogWarningStaysOffStdout pins the one place this warning must not go.
// It warns that a stray line would corrupt the report, so emitting it onto the
// report would be the fault it exists to report.
func TestStdoutLogWarningStaysOffStdout(t *testing.T) {
	t.Setenv("SEI_LOG_OUTPUT", "")

	proseBuf, jsonBuf := captureDigestOutput(t, true)
	warnIfLogsShareStdout()

	require.Contains(t, proseBuf.String(), "SEI_LOG_OUTPUT")
	require.Empty(t, jsonBuf.String(), "the warning reached the report it warns about")
}

// TestStdoutLogWarningIsSilentWhenRedirected pins that the warning names a real
// condition rather than firing on every run, since one that always fires is one
// a caller learns to filter out.
func TestStdoutLogWarningIsSilentWhenRedirected(t *testing.T) {
	t.Setenv("SEI_LOG_OUTPUT", "stderr")

	proseBuf, _ := captureDigestOutput(t, true)
	warnIfLogsShareStdout()

	require.Empty(t, proseBuf.String())
}

// TestCensusFreeDigestReadsAsUnmeasuredInBothForms pins that the two forms agree
// on a census that was never taken. The prose omits the block, so an object
// carrying six zeros would say "measured, and all zero" where the text says
// "not measured" — the drift the single report exists to prevent.
func TestCensusFreeDigestReadsAsUnmeasuredInBothForms(t *testing.T) {
	d := digestOverCoreEVMKeys(t)
	require.Nil(t, d.census, "this helper stands in for the backends that take no census")
	ctx := testDigestContext()

	proseBuf, _ := captureDigestOutput(t, false)
	require.NoError(t, d.emit(ctx))
	require.NotContains(t, proseBuf.String(), "Zero-value memiavl census")

	_, jsonBuf := captureDigestOutput(t, true)
	require.NoError(t, d.emit(ctx))
	require.NotContains(t, jsonBuf.String(), "zero_census")

	var got evmDigestJSON
	require.NoError(t, json.Unmarshal(jsonBuf.Bytes(), &got))
	require.Nil(t, got.ZeroCensus)
}

// TestCensusIsAllOrNothingAcrossBothCounterLevels pins the invariant behind the
// single census field. The row counters are raised during the leaf scan and the
// account counters at finalize, so while those two read separate arguments a
// path could count one level and not the other, and a report showing real row
// counts beside zero_accounts=0 invites that zero to be read as a finding.
// Sharing one field is what makes the partial state unreachable.
func TestCensusIsAllOrNothingAcrossBothCounterLevels(t *testing.T) {
	// Rows that populate a row counter (an all-zero code-hash) and an account
	// counter (that same address being otherwise empty), so a half-counted census
	// would be visible here.
	addr := bytesOfLen(keys.AddressLen, 0x41)
	rawPairs := []struct{ Key, Value []byte }{
		{Key: keys.BuildEVMKey(keys.EVMKeyNonce, addr), Value: nonceBytes(0)},
		{Key: keys.BuildEVMKey(keys.EVMKeyCodeHash, addr), Value: make([]byte, 32)},
	}

	for _, tc := range []struct {
		name    string
		census  *evmZeroCensus
		wantNil bool
	}{
		{"a path that takes no census counts neither level", nil, true},
		{"a path that takes one counts both", &evmZeroCensus{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := evmDigest{census: tc.census}
			accounts := make(map[string]*semanticAccountDigestState)
			for _, p := range rawPairs {
				require.NoError(t, d.consumeSemanticMemiavlLeaf(accounts, p.Key, p.Value))
			}
			d.finalizeSemanticAccounts(accounts)

			if tc.wantNil {
				require.Nil(t, d.census)
				return
			}
			require.NotZero(t, d.census.ZeroCodeHashRows, "row counter not raised")
			require.NotZero(t, d.census.ZeroAccounts, "account counter not raised")
		})
	}
}

// TestDigestJSONNamesTheMarkerAdjustmentsBehindTheMiscBucket pins the field that
// tells an in-progress reading from a completed one. The misc digest is the
// adjusted value in both cases, which is what lets them compare equal, so
// without the named adjustments a caller cannot recover which node it read.
func TestDigestJSONNamesTheMarkerAdjustmentsBehindTheMiscBucket(t *testing.T) {
	miscKey := append([]byte{0x09}, bytesOfLen(keys.AddressLen, 0x33)...)
	miscVal := vtype.NewMiscData().SetBlockHeight(10).SetValue([]byte{0xDE, 0xAD}).Serialize()
	boundaryVal := vtype.NewMiscData().SetBlockHeight(30).SetValue([]byte{0x02, 0x03}).Serialize()

	clean := evmDigest{}
	require.NoError(t, clean.consume(miscKey, miscVal))

	inProgress := evmDigest{}
	require.NoError(t, inProgress.consume(miscKey, miscVal))
	require.NoError(t, inProgress.consume(migrationBoundaryPhysKey, boundaryVal))

	ctx := testDigestContext()
	cleanReport := clean.report(ctx)
	progReport := inProgress.report(ctx)

	require.Empty(t, cleanReport.MarkerAdjustments)
	require.Equal(t, []string{"migration/migration-boundary"}, progReport.MarkerAdjustments)
	require.Equal(t, cleanReport.Misc, progReport.Misc,
		"the misc bucket must already have the marker XORed out")

	// An absent adjustment list encodes as [], never null, so a caller can range
	// over it without a nil check.
	encoded, err := json.Marshal(cleanReport)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"marker_adjustments":[]`)
}

// TestDigestJSONIsOneLine pins the framing a scheduled caller relies on: one run
// produces one line, so its result can be read without parsing the stream.
func TestDigestJSONIsOneLine(t *testing.T) {
	d := digestOverCoreEVMKeys(t)

	_, jsonBuf := captureDigestOutput(t, true)
	require.NoError(t, d.emit(testDigestContext()))

	out := jsonBuf.String()
	require.Equal(t, 1, strings.Count(out, "\n"))
	require.True(t, strings.HasSuffix(out, "\n"))
}

// TestJSONWithInspectBucketIsRefused pins the refusal rather than the silence it
// replaces: inspect mode produces no digest report, so honouring --json there
// would leave stdout empty and a caller waiting on an object that never comes.
func TestJSONWithInspectBucketIsRefused(t *testing.T) {
	cmd := EvmLogicalDigestCmd()
	cmd.SetArgs([]string{
		"--backend", "flatkv",
		"--db-dir", t.TempDir(),
		"--inspect-bucket", "storage",
		"--json",
	})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true

	err := cmd.Execute()
	require.ErrorContains(t, err, "--inspect-bucket")
}
