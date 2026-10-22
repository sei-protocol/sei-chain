package v2record

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/sei-protocol/sei-chain/evmrpc"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmbytes "github.com/sei-protocol/sei-chain/sei-tendermint/libs/bytes"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	rpcclient "github.com/sei-protocol/sei-chain/sei-tendermint/rpc/client"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

var errBlockNotFound = errors.New("block not found")

// chainClient serves the blocks a chain has finalized to the v2 RPC, in place
// of a Tendermint node. Heights start at 1.
type chainClient struct {
	rpcclient.Client
	blocks  []*coretypes.ResultBlock
	results []*coretypes.ResultBlockResults
}

func (c *chainClient) record(block *coretypes.ResultBlock, txResults []*abci.ExecTxResult, params *tmproto.ConsensusParams) {
	c.blocks = append(c.blocks, block)
	c.results = append(c.results, &coretypes.ResultBlockResults{
		Height:                block.Block.Height,
		TxsResults:            txResults,
		ConsensusParamUpdates: params,
	})
}

func (c *chainClient) latest() int64 {
	return int64(len(c.blocks))
}

func (c *chainClient) at(height *int64) (int64, error) {
	if height == nil {
		return c.latest(), nil
	}
	if *height < 1 || *height > c.latest() {
		return 0, errBlockNotFound
	}
	return *height, nil
}

func (*chainClient) EvmNextPendingNonce(common.Address) uint64 { return 0 }

func (*chainClient) GenesisInitialHeight() int64 { return 1 }

func (*chainClient) EvmTxByHash(common.Hash) (tmtypes.Tx, bool) { return nil, false }

func (*chainClient) EvmProxy(common.Address) utils.Option[*rpc.Client] {
	return utils.None[*rpc.Client]()
}

func (c *chainClient) Block(_ context.Context, height *int64) (*coretypes.ResultBlock, error) {
	h, err := c.at(height)
	if err != nil {
		return nil, err
	}
	return c.blocks[h-1], nil
}

func (c *chainClient) BlockByHash(_ context.Context, hash tmbytes.HexBytes) (*coretypes.ResultBlock, error) {
	for _, block := range c.blocks {
		if bytes.Equal(block.BlockID.Hash, hash) {
			return block, nil
		}
	}
	return nil, errBlockNotFound
}

func (c *chainClient) Header(_ context.Context, height *int64) (*coretypes.ResultHeader, error) {
	h, err := c.at(height)
	if err != nil {
		return nil, err
	}
	return &coretypes.ResultHeader{Header: &c.blocks[h-1].Block.Header}, nil
}

func (c *chainClient) BlockResults(_ context.Context, height *int64) (*coretypes.ResultBlockResults, error) {
	h, err := c.at(height)
	if err != nil {
		return nil, err
	}
	return c.results[h-1], nil
}

func (*chainClient) Genesis(context.Context) (*coretypes.ResultGenesis, error) {
	return &coretypes.ResultGenesis{Genesis: &tmtypes.GenesisDoc{InitialHeight: 1}}, nil
}

func (c *chainClient) Status(context.Context) (*coretypes.ResultStatus, error) {
	return &coretypes.ResultStatus{SyncInfo: coretypes.SyncInfo{LatestBlockHeight: c.latest(), EarliestBlockHeight: 1}}, nil
}

func (*chainClient) Validators(context.Context, *int64, *int, *int) (*coretypes.ResultValidators, error) {
	return &coretypes.ResultValidators{}, nil
}

func (*chainClient) UnconfirmedTxs(context.Context, *int, *int) (*coretypes.ResultUnconfirmedTxs, error) {
	return &coretypes.ResultUnconfirmedTxs{}, nil
}

// Events serves the NewBlock subscription the v2 RPC's block notifier reads.
func (c *chainClient) Events(_ context.Context, req *coretypes.RequestEvents) (*coretypes.ResultEvents, error) {
	if !strings.Contains(req.Filter.Query, "tm.event = 'NewBlock'") {
		return nil, errors.New("unexpected event query " + req.Filter.Query)
	}
	cursor := 0
	if req.After != "" {
		var err error
		if cursor, err = strconv.Atoi(req.After); err != nil {
			return nil, err
		}
	}
	res := &coretypes.ResultEvents{Oldest: strconv.Itoa(cursor)}
	for ; cursor < len(c.blocks); cursor++ {
		if len(res.Items) >= req.MaxItems {
			res.More = true
			break
		}
		item, err := newBlockEvent(c.blocks[cursor], cursor)
		if err != nil {
			return nil, err
		}
		res.Items = append(res.Items, item)
	}
	res.Newest = strconv.Itoa(cursor - 1)
	return res, nil
}

func newBlockEvent(block *coretypes.ResultBlock, cursor int) (*coretypes.EventItem, error) {
	data, err := json.Marshal(tmtypes.EventDataNewBlock{Block: block.Block, BlockID: block.BlockID})
	if err != nil {
		return nil, err
	}
	wrapped, err := json.Marshal(evmrpc.EventItemDataWrapper{Type: "NewBlock", Value: data})
	if err != nil {
		return nil, err
	}
	return &coretypes.EventItem{Cursor: strconv.Itoa(cursor), Data: wrapped, Event: "event"}, nil
}
