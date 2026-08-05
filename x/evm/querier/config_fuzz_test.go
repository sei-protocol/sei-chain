package querier_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/testutil/configtest"
	"github.com/sei-protocol/sei-chain/testutil/fuzzing"
	"github.com/sei-protocol/sei-chain/x/evm/querier"
)

// evmQueryKeys is the [evm_query] section's read-site manifest: one key, guarded
// and checked.
//
// The default of 300000 is the same literal the app.toml template renders for
// [wasm] query_gas_limit, and both are unrelated to the wasmd in-code default of
// 3,000,000. Pinning the value here means a change to one cannot quietly be
// mistaken for a change to the other.
var evmQueryKeys = []configtest.KeySpec{
	{
		Key: "evm_query.evm_query_gas_limit", Path: "GasLimit", Cast: configtest.CastUint64,
		Checked: true,
		Why:     "default 300000; bounds gas for EVM state queries served from the Cosmos querier",
	},
}

func readEVMQuery(opts configtest.AppOpts) (any, error) { return querier.ReadConfig(opts) }

// FuzzReadConfig pins the [evm_query] gas limit against arbitrary raw values.
//
// The unsigned cast is the interesting part. cast refuses a negative into an
// unsigned conversion, and this read is checked, so an operator who writes -1
// expecting "unlimited" gets a boot failure naming the key rather than a limit of
// 0 or a wrapped 2^64. Refusing is the safe direction here — a gas limit of 0
// would fail every EVM state query on a node that came up clean.
func FuzzReadConfig(f *testing.F) {
	seeds := configtest.NewSeeds(f, fuzzing.ConfigValue)
	seeds.Add(fuzzing.KindInt64, "", int64(300000), false)
	seeds.Add(fuzzing.KindNumericString, "", int64(500000), false)
	seeds.Add(fuzzing.KindInt64, "", int64(-1), false)
	seeds.Add(fuzzing.KindString, "not-a-number", int64(0), false)
	seeds.Add(fuzzing.KindNil, "", int64(0), false)
	seeds.Add(fuzzing.KindFloat64, "", int64(7), false)

	configtest.CheckEveryRowHasADiscriminatingSeed(f, "evm_query", readEVMQuery, evmQueryKeys, seeds)

	f.Fuzz(func(t *testing.T, kind uint8, s string, n int64, b bool) {
		configtest.CheckRow(t, "evm_query", readEVMQuery, evmQueryKeys[0], fuzzing.ConfigValue(kind, s, n, b))
	})
}

// TestReadConfigAbsentKeysKeepDefaults pins the section baseline.
func TestReadConfigAbsentKeysKeepDefaults(t *testing.T) {
	configtest.CheckAbsent(t, "evm_query", readEVMQuery, querier.DefaultConfig)
}

// TestDefaultsMatchTheRecordedValues pins the evm_query defaults themselves.
//
// The absent-keys row above proves the reader returns the declared defaults; it cannot prove
// which values those are, because both sides of that comparison come from this package. This
// compares them against testdata/evm_query.golden, an independent recording, so a default that
// moves shows the new value in a diff instead of passing silently.
func TestDefaultsMatchTheRecordedValues(t *testing.T) {
	configtest.CheckDefaults(t, "evm_query", querier.DefaultConfig)
}

// TestKeyNamesMatchTheRecordedNames pins the key name itself, so that the protection does not
// depend on this row being spelled as a literal today.
func TestKeyNamesMatchTheRecordedNames(t *testing.T) {
	configtest.CheckKeyNames(t, "evm_query", evmQueryKeys)
}

// TestManifestNamesEveryField enforces the claim evmQueryKeys makes about itself: that it names
// every key the reader looks up. Left as prose the claim can drift, and it is the artifact a
// replacement implementation reads as this section's contract.
func TestManifestNamesEveryField(t *testing.T) {
	configtest.CheckManifestCoversEveryField(t, "evm_query", querier.DefaultConfig, evmQueryKeys)
}

// TestWiringMatchesTheRecord pins which checks each of this package's sections is wired to.
//
// Every other check here reports a change to what it asserts. None reports a check being removed, so
// this records the wiring and fails when it thins out.
func TestWiringMatchesTheRecord(t *testing.T) {
	configtest.CheckWiring(t)
}
