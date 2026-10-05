package store

import (
	"encoding/json"
	"errors"
	"os"
	"testing"

	tmtime "github.com/sei-protocol/sei-chain/sei-tendermint/libs/time"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// Blocks from before v6.4 carry a LastCommitHash that the current validation
// rejects, but the store must still serve them.
func TestLoadBlockServesPreV64Block(t *testing.T) {
	bz, err := os.ReadFile("testdata/pacific-1-202095401.json")
	require.NoError(t, err)
	var res coretypes.ResultBlock
	require.NoError(t, json.Unmarshal(bz, &res))
	pb, err := res.Block.ToProto()
	require.NoError(t, err)
	_, err = types.BlockFromProto(pb)
	require.True(t, errors.Is(err, types.ErrLastCommitHash))

	bs, _ := newInMemoryBlockStore()
	parts, err := res.Block.MakePartSet(types.BlockPartSizeBytes)
	require.NoError(t, err)
	require.Equal(t, res.BlockID.PartSetHeader, parts.Header())
	bs.SaveBlock(res.Block, parts, makeTestCommit(res.Block.Height, tmtime.Now()))

	require.Equal(t, res.BlockID.Hash, bs.LoadBlock(res.Block.Height).Hash())
	require.Equal(t, res.BlockID.Hash, bs.LoadBlockByHash(res.BlockID.Hash).Hash())
}

func TestLoadBlockPanicsWhenPartsDoNotMatchBlockMeta(t *testing.T) {
	header := types.Header{Height: 1, ChainID: "block_test", Time: tmtime.Now()}
	block := newBlock(header, makeTestCommit(1, tmtime.Now()))
	header.ChainID = "other_chain"
	other := newBlock(header, makeTestCommit(1, tmtime.Now()))

	bs, _ := newInMemoryBlockStore()
	bs.SaveBlock(block, makeValidMultiPartSet(t, other), makeTestCommit(1, tmtime.Now()))

	_, _, panicErr := doFn(func() (any, error) { return bs.LoadBlock(1), nil })
	require.True(t, panicErr != nil)
}
