package operations

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb/mvcc"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

// newTestReceiptStore returns the directory of a new store holding value under
// key in storeKey at version, with its earliest version set to earliest.
func newTestReceiptStore(t *testing.T, storeKey string, version, earliest int64, key, value string) string {
	t.Helper()
	dir := t.TempDir()
	store, err := mvcc.OpenDB(dir, config.DefaultStateStoreConfig())
	require.NoError(t, err)
	require.NoError(t, store.ApplyChangesetSync(version, []*proto.NamedChangeSet{{
		Name:      storeKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{Key: []byte(key), Value: []byte(value)}}},
	}}))
	require.NoError(t, store.SetEarliestVersion(earliest, true))
	require.NoError(t, store.Close())
	return dir
}

func runMergeReceiptStore(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := MergeReceiptStoreCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestReceiptStoreKeyIsTheEVMModules(t *testing.T) {
	require.Equal(t, evmtypes.ReceiptStoreKey, receiptStoreKey)
}

// After a merge the target serves the source's receipts at its latest version,
// where a node looks receipts up.
func TestMergeReceiptStoreServesBothStores(t *testing.T) {
	sourceDir := newTestReceiptStore(t, receiptStoreKey, 10, 0, "tx-a", "receipt-a")
	targetDir := newTestReceiptStore(t, receiptStoreKey, 100, 11, "tx-b", "receipt-b")

	out, err := runMergeReceiptStore(t, "--source", sourceDir, "--target", targetDir, "--verify-every", "1")
	require.NoError(t, err)
	require.Contains(t, out, "entries written: 1 ")
	require.Contains(t, out, "target versions now: 0-100")
	require.Contains(t, out, "entries read back through the store's read path: 1")

	store, err := mvcc.OpenDB(targetDir, config.DefaultStateStoreConfig())
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	for key, want := range map[string]string{"tx-a": "receipt-a", "tx-b": "receipt-b"} {
		got, err := store.Get(receiptStoreKey, store.GetLatestVersion(), []byte(key))
		require.NoError(t, err)
		require.Equal(t, []byte(want), got, key)
	}
}

// Heights between the two stores that neither covers refuse the merge unless
// --allow-gap is set, and an allowed merge reports them.
func TestMergeReceiptStoreGap(t *testing.T) {
	newPair := func(t *testing.T) (sourceDir, targetDir string) {
		return newTestReceiptStore(t, receiptStoreKey, 10, 0, "tx-a", "receipt-a"),
			newTestReceiptStore(t, receiptStoreKey, 100, 90, "tx-b", "receipt-b")
	}

	t.Run("refused by default", func(t *testing.T) {
		sourceDir, targetDir := newPair(t)
		_, err := runMergeReceiptStore(t, "--source", sourceDir, "--target", targetDir)
		require.ErrorContains(t, err, "neither DB covers versions 11-89")
	})

	t.Run("allowed", func(t *testing.T) {
		sourceDir, targetDir := newPair(t)
		out, err := runMergeReceiptStore(t, "--source", sourceDir, "--target", targetDir, "--allow-gap")
		require.NoError(t, err)
		require.Contains(t, out, "target versions now: 0-100")
		require.Contains(t, out, "versions neither store held, now in the target's range: 11-89")
	})
}

func TestMergeReceiptStoreRefusesOtherStores(t *testing.T) {
	sourceDir := newTestReceiptStore(t, "bank", 10, 0, "k", "v")
	targetDir := newTestReceiptStore(t, receiptStoreKey, 100, 90, "tx-b", "receipt-b")

	_, err := runMergeReceiptStore(t, "--source", sourceDir, "--target", targetDir)
	require.ErrorContains(t, err, `holds store "bank"`)
}
