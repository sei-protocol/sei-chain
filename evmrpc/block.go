package evmrpc

import (
	"context"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	rpcclient "github.com/tendermint/tendermint/rpc/client"
	"github.com/tendermint/tendermint/rpc/coretypes"
)

type BlockAPI struct {
	tmClient rpcclient.Client
}

func NewBlockAPI(tmClient rpcclient.Client) *BlockAPI {
	return &BlockAPI{tmClient: tmClient}
}

func (a *BlockAPI) GetBlockByHash(ctx context.Context, blockHash common.Hash, fullTx bool) (map[string]interface{}, error) {
	if fullTx {
		return nil, errors.New("getting block with full transactions is not supported yet")
	}
	block, err := a.tmClient.BlockByHash(ctx, blockHash[:])
	if err != nil {
		return nil, err
	}
	fields := ethapi.RPCMarshalBlock(b, inclTx, fullTx, s.b.ChainConfig())
	fields["totalDifficulty"] = (*hexutil.Big)(s.b.GetTd(ctx, b.Hash()))
	return fields, nil
}

func encodeTmBlock(
	block *coretypes.ResultBlock,
	blockRes *coretypes.ResultBlockResults,
) map[string]interface{} {
	number := big.NewInt(block.Block.Height)
	hash := common.HexToHash(string(block.BlockID.Hash))
	lastHash := common.HexToHash(string(block.Block.LastBlockID.Hash))
	appHash := common.HexToHash(string(block.Block.AppHash))
	txHash := common.HexToHash(string(block.Block.DataHash))
	resultHash := common.HexToHash(string(block.Block.LastResultsHash))
	miner := common.HexToAddress(string(block.Block.ProposerAddress))
	gasLimit, gasWanted := int64(0), int64(0)
	for _, txRes := range blockRes.TxsResults {
		gasLimit += txRes.GasWanted
		gasWanted += txRes.GasUsed
	}
	transactions := []interface{}{}
	for i, txRes := range blockRes.TxsResults {
		if hydrate {

		}
	}
	result := map[string]interface{}{
		"number":           (*hexutil.Big)(number),
		"hash":             hash,
		"parentHash":       lastHash,
		"nonce":            ethtypes.BlockNonce{}, // inapplicable to Sei
		"mixHash":          common.Hash{},         // inapplicable to Sei
		"sha3Uncles":       common.Hash{},         // inapplicable to Sei
		"logsBloom":        ethtypes.Bloom{},      // inapplicable to Sei
		"stateRoot":        appHash,
		"miner":            miner,
		"difficulty":       (*hexutil.Big)(big.NewInt(0)), // inapplicable to Sei
		"extraData":        hexutil.Bytes{},               // inapplicable to Sei
		"gasLimit":         hexutil.Uint64(gasLimit),
		"gasUsed":          hexutil.Uint64(gasWanted),
		"timestamp":        hexutil.Uint64(block.Block.Time.Unix()),
		"transactionsRoot": txHash,
		"receiptsRoot":     resultHash,
		"size":             hexutil.Uint64(block.Block.Size()),
		"uncles":           []common.Hash{}, // inapplicable to Sei
	}
}
