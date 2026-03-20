package receipt

import (
	"github.com/ethereum/go-ethereum/common"
	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

func newTestContext() (sdk.Context, storetypes.StoreKey) {
	storeKey := storetypes.NewKVStoreKey("evm")
	tkey := storetypes.NewTransientStoreKey("evm_transient")
	ctx := testutil.DefaultContext(storeKey, tkey).WithBlockHeight(1)
	return ctx, storeKey
}

func makeTestReceipt(txHash common.Hash, blockNumber uint64, txIndex uint32, addr common.Address, topics []common.Hash) *types.Receipt {
	topicHex := make([]string, 0, len(topics))
	for _, topic := range topics {
		topicHex = append(topicHex, topic.Hex())
	}

	return &types.Receipt{
		TxHashHex:        txHash.Hex(),
		BlockNumber:      blockNumber,
		TransactionIndex: txIndex,
		Logs: []*types.Log{
			{
				Address: addr.Hex(),
				Topics:  topicHex,
				Data:    []byte{0x1},
				Index:   0,
			},
		},
	}
}
