package genutil_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gogo/protobuf/proto"

	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"

	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	cdctypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types"
	bankexported "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/exported"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/genutil"
	gtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/genutil/types"
)

type doNothingUnmarshalJSON struct {
	codec.JSONCodec
}

func (dnj *doNothingUnmarshalJSON) UnmarshalAsJSON(_ []byte, _ proto.Message) error {
	return nil
}

type doNothingIterator struct {
	gtypes.GenesisBalancesIterator
}

func (dni *doNothingIterator) IterateGenesisBalances(_ codec.JSONCodec, _ map[string]json.RawMessage, _ func(bankexported.GenesisBalance) bool) {
}

// Ensures that CollectTx correctly traverses directories and won't error out on encountering
// a directory during traversal of the first level. See issue https://github.com/cosmos/cosmos-sdk/issues/6788.
func TestCollectTxsHandlesDirectories(t *testing.T) {
	testDir := t.TempDir()
	// 1. We'll insert a directory as the first element before JSON file.
	subDirPath := filepath.Join(testDir, "_adir")
	if err := os.MkdirAll(subDirPath, 0755); err != nil {
		t.Fatal(err)
	}

	txDecoder := types.TxDecoder(func(txBytes []byte) (types.Tx, error) {
		return nil, nil
	})

	// 2. Ensure that we don't encounter any error traversing the directory.
	srvCtx := server.NewDefaultContext()
	_ = srvCtx
	cdc := codec.NewProtoCodec(cdctypes.NewInterfaceRegistry())
	gdoc := tmtypes.GenesisDoc{AppState: []byte("{}")}
	balItr := new(doNothingIterator)

	dnc := &doNothingUnmarshalJSON{cdc}
	if _, _, err := genutil.CollectTxs(dnc, txDecoder, "foo", testDir, gdoc, balItr); err != nil {
		t.Fatal(err)
	}
}
