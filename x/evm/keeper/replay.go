package keeper

import (
	"context"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/misc/eip4844"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/lib/ethapi"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/x/evm/state/replay"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

type ReplayServer struct {
	*msgServer

	rpcClient          *rpc.Client
	interblockCache    *replay.InterBlockCache
	genesisBlockNumber int64
}

func (server *ReplayServer) ReplayTransaction(
	goCtx context.Context,
	msg *types.MsgEVMTransaction,
) (serverRes *types.MsgEVMTransactionResponse, err error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeter())
	tx, _ := msg.AsTransaction()
	txHash := tx.Hash()
	txResult, err := GetTransactionByHash(server.rpcClient, txHash)
	if err != nil {
		return nil, err
	}
	header, err := GetBlockHeaderByHash(server.rpcClient, *txResult.BlockHash)
	if err != nil {
		return nil, err
	}

	var (
		baseFee     *big.Int
		blobBaseFee *big.Int
		random      *common.Hash
	)

	if header.BaseFee != nil {
		baseFee = new(big.Int).Set(header.BaseFee)
	}
	if header.ExcessBlobGas != nil {
		blobBaseFee = eip4844.CalcBlobFee(*header.ExcessBlobGas)
	}
	if header.Difficulty.Cmp(common.Big0) == 0 {
		random = &header.MixDigest
	}
	blockCtx := &vm.BlockContext{
		CanTransfer: core.CanTransfer,
		Transfer:    core.Transfer,
		GetHash:     core.GetHashFn(header, &HeaderGetter{RpcClient: server.rpcClient}),
		Coinbase:    header.Coinbase,
		GasLimit:    header.GasLimit,
		BlockNumber: txResult.BlockNumber.ToInt(),
		Time:        header.Time,
		Difficulty:  header.Difficulty,
		BaseFee:     baseFee,
		Random:      random,
		BlobBaseFee: blobBaseFee,
	}
	cfg := params.MainnetChainConfig
	signer := ethtypes.MakeSigner(cfg, header.Number, header.Time)
	emsg, err := core.TransactionToMessage(tx, signer, baseFee)
	if err != nil {
		return nil, err
	}
	txCtx := core.NewEVMTxContext(emsg)
	stateDB := replay.NewReplayStateDB(ctx, server, server.rpcClient, server.genesisBlockNumber, server.interblockCache)
	evmInstance := vm.NewEVM(*blockCtx, txCtx, stateDB, cfg, vm.Config{})
	gp := core.GasPool(blockCtx.GasLimit)
	st := core.NewStateTransition(evmInstance, emsg, &gp)
	res, err := st.TransitionDb()
	if err != nil {
		return nil, err
	}
	if res.Err != nil {
		return nil, err
	}
	return &types.MsgEVMTransactionResponse{
		GasUsed:    res.UsedGas,
		ReturnData: res.ReturnData,
		VmError:    res.Err.Error(),
		Hash:       txHash.Hex(),
	}, nil
}

func GetTransactionByHash(rpcClient *rpc.Client, hash common.Hash) (*ethapi.RPCTransaction, error) {
	result := &ethapi.RPCTransaction{}
	method := "eth_getTransactionByHash"
	if err := rpcClient.Call(result, method, hash); err != nil {
		return nil, err
	}
	return result, nil
}

func GetBlockHeaderByHash(rpcClient *rpc.Client, hash common.Hash) (*ethtypes.Header, error) {
	result := &ethtypes.Header{}
	method := "eth_getBlockByHash"
	if err := rpcClient.Call(result, method, hash, false); err != nil {
		return nil, err
	}
	return result, nil
}

type HeaderGetter struct {
	RpcClient *rpc.Client
}

func (hg *HeaderGetter) GetHeader(hash common.Hash, num uint64) *ethtypes.Header {
	result := &ethtypes.Header{}
	method := "eth_getBlockByHash"
	if err := hg.RpcClient.Call(result, method, hash, false); err != nil {
		panic(err)
	}
	return result
}

func (hg *HeaderGetter) Engine() consensus.Engine {
	panic("should never be called")
}
