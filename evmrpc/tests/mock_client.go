package tests

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sei-protocol/sei-chain/evmrpc"
	seiutils "github.com/sei-protocol/sei-chain/utils"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/bytes"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/tendermint/tendermint/rpc/client/mock"
	"github.com/tendermint/tendermint/rpc/coretypes"
	tmtypes "github.com/tendermint/tendermint/types"
)

type MockClient struct {
	mock.Client
	blocks           [][][]byte
	txResults        [][]*abci.ExecTxResult
	consParamUpdates []*tmproto.ConsensusParams
	events           [][]abci.Event
}

func (c *MockClient) Block(_ context.Context, h *int64) (*coretypes.ResultBlock, error) {
	if h == nil {
		return c.getBlock(int64(len(c.blocks))), nil
	}
	return c.getBlock(*h), nil
}

func (c *MockClient) BlockByHash(_ context.Context, hash bytes.HexBytes) (*coretypes.ResultBlock, error) {
	bz := make([]byte, 8)
	if len(hash) < 8 {
		copy(bz, hash)
	} else {
		copy(bz, hash[len(hash)-8:])
	}
	return c.getBlock(int64(binary.BigEndian.Uint64(bz))), nil
}

func (c *MockClient) getBlock(i int64) *coretypes.ResultBlock {
	return &coretypes.ResultBlock{
		BlockID: tmtypes.BlockID{Hash: mockHash(i, 0)},
		Block: &tmtypes.Block{
			Data:       tmtypes.Data{Txs: seiutils.Map(c.blocks[i-1], func(tx []byte) tmtypes.Tx { return tmtypes.Tx(tx) })},
			Header:     mockBlockHeader(i),
			LastCommit: &tmtypes.Commit{Height: i},
		},
	}
}

func (c *MockClient) Genesis(context.Context) (*coretypes.ResultGenesis, error) {
	return &coretypes.ResultGenesis{Genesis: &tmtypes.GenesisDoc{InitialHeight: 1}}, nil
}

func (c *MockClient) BlockResults(_ context.Context, height *int64) (*coretypes.ResultBlockResults, error) {
	return &coretypes.ResultBlockResults{
		TxsResults:            c.txResults[*height-1],
		ConsensusParamUpdates: c.consParamUpdates[*height-1],
	}, nil
}

func (c *MockClient) recordBlockResult(txResults []*abci.ExecTxResult, consParamUpdates *tmproto.ConsensusParams, events []abci.Event) {
	c.txResults = append(c.txResults, txResults)
	c.consParamUpdates = append(c.consParamUpdates, consParamUpdates)
	c.events = append(c.events, events)
}

func (c *MockClient) Validators(ctx context.Context, height *int64, page, perPage *int) (*coretypes.ResultValidators, error) {
	return &coretypes.ResultValidators{}, nil
}

func (c *MockClient) Events(_ context.Context, req *coretypes.RequestEvents) (*coretypes.ResultEvents, error) {
	if strings.Contains(req.Filter.Query, "tm.event = 'NewBlock'") {
		var cursor int
		var err error
		if req.After != "" {
			cursor, err = strconv.Atoi(req.After)
			if err != nil {
				panic("invalid cursor")
			}
		} else {
			cursor = 0
		}
		res := &coretypes.ResultEvents{Oldest: strconv.FormatInt(int64(cursor), 10), Items: make([]*coretypes.EventItem, 0, req.MaxItems)}
		for ; cursor < len(c.blocks); cursor++ {
			if len(res.Items) >= req.MaxItems {
				res.More = true
				break
			}
			block := c.getBlock(int64(cursor + 1))
			data := tmtypes.EventDataNewBlock{
				Block:   block.Block,
				BlockID: block.BlockID,
			}
			eventData, err := json.Marshal(data)
			if err != nil {
				panic(err)
			}
			wrappedData := evmrpc.EventItemDataWrapper{
				Type:  "NewBlock",
				Value: eventData,
			}
			bz, err := json.Marshal(wrappedData)
			if err != nil {
				panic(err)
			}
			res.Items = append(res.Items, &coretypes.EventItem{
				Cursor: strconv.FormatInt(int64(cursor), 10),
				Data:   bz,
				Event:  "event",
			})
		}
		res.Newest = strconv.FormatInt(int64(cursor-1), 10)
		return res, nil
	} else {
		panic(fmt.Sprintf("event query %s is not expected to be made by EVM RPC", req.Filter.Query))
	}
}

func mockHash(height int64, prefix int64) bytes.HexBytes {
	heightBz, prefixBz := make([]byte, 8), make([]byte, 8)
	binary.BigEndian.PutUint64(heightBz, uint64(height))
	binary.BigEndian.PutUint64(prefixBz, uint64(prefix))
	return bytes.HexBytes(append(prefixBz, heightBz...))
}

func mockBlockHeader(height int64) tmtypes.Header {
	return tmtypes.Header{
		ChainID:         "test",
		Height:          height,
		Time:            time.Unix(1696941649+height, 0),
		DataHash:        mockHash(height, 1),
		AppHash:         mockHash(height, 2),
		LastResultsHash: mockHash(height, 3),
		ProposerAddress: mockHash(height, 4),
		LastBlockID: tmtypes.BlockID{
			Hash: mockHash(height-1, 0),
		},
		LastCommitHash:     mockHash(height, 5),
		ValidatorsHash:     mockHash(height, 6),
		NextValidatorsHash: mockHash(height, 7),
		ConsensusHash:      mockHash(height, 8),
		EvidenceHash:       mockHash(height, 9),
	}
}
