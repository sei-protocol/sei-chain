package evmonly

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
)

func TestOpenTemporaryReceiptStoreUsesGigaBackendAndRemovesDirectory(t *testing.T) {
	store, err := OpenTemporaryReceiptStore(t.TempDir())
	require.NoError(t, err)
	temporaryStore := store.(*temporaryReceiptStore)
	require.Equal(t, "littidx", receipt.BackendTypeName(temporaryStore.ReceiptStore))
	require.DirExists(t, temporaryStore.directory)

	require.NoError(t, store.Close())
	_, err = os.Stat(temporaryStore.directory)
	require.ErrorIs(t, err, os.ErrNotExist)
	require.NoError(t, store.Close())
}
