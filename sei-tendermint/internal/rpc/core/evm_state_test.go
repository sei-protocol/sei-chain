package core

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
)

type evmStateApp struct{ proxy.EVMApplication }

func (a evmStateApp) Info() *proxy.ResponseInfo {
	return &proxy.ResponseInfo{Data: "native-evm", LastBlockHeight: 17, LastBlockAppHash: []byte{42}}
}
func (a evmStateApp) LastBlockHeight() int64                        { return 17 }
func (a evmStateApp) EvmNonce(common.Address) uint64                { return 3 }
func (a evmStateApp) EvmBalance(common.Address, []byte) uint256.Int { return *uint256.NewInt(99) }
func (a evmStateApp) CheckTx(context.Context, *proxy.RequestCheckTxV2) *proxy.ResponseCheckTxV2 {
	return &proxy.ResponseCheckTxV2{ResponseCheckTx: &proxy.ResponseCheckTx{Code: 7, Log: "rejected", GasWanted: 21000}}
}

func TestEVMEnvironmentReadsStateWithoutABCI(t *testing.T) {
	app := proxy.NewEVM(evmStateApp{})
	env := &Environment{EVM: utils.Some(app)}
	require.False(t, env.App.IsPresent())
	require.Equal(t, uint64(17), env.EvmBlockNumber())
	require.Equal(t, uint64(3), env.EvmTransactionCount(common.Address{1}))
	require.Equal(t, *uint256.NewInt(99), app.EvmBalance(common.Address{1}, nil))
	info, err := env.ABCIInfo(t.Context())
	require.NoError(t, err)
	require.Equal(t, "native-evm", info.Response.Data)
	require.Equal(t, int64(17), info.Response.LastBlockHeight)
	require.Equal(t, []byte{42}, info.Response.LastBlockAppHash)
	res, err := env.CheckTx(t.Context(), &coretypes.RequestCheckTx{Tx: []byte{1}})
	require.NoError(t, err)
	require.Equal(t, uint32(7), res.Code)
	require.Equal(t, "rejected", res.Log)
	require.Equal(t, int64(21000), res.GasWanted)
	_, err = env.ABCIQuery(t.Context(), &coretypes.RequestABCIQuery{})
	require.Error(t, err)
}

// senderCacheApp records senders provided before transaction admission.
type senderCacheApp struct {
	evmStateApp
	senders map[common.Hash]common.Address
}

func (a *senderCacheApp) RememberEvmSender(hash common.Hash, sender common.Address) {
	a.senders[hash] = sender
}

func (a *senderCacheApp) ForgetEvmSender(hash common.Hash) {
	delete(a.senders, hash)
}

func TestEVMEnvironmentCachesSendersWithoutABCI(t *testing.T) {
	app := &senderCacheApp{senders: make(map[common.Hash]common.Address)}
	env := &EVMEnvironment{Environment: &Environment{}, EVMProxy: proxy.NewEVM(app)}
	hash := common.Hash{1}
	sender := common.Address{2}

	env.RememberEvmSender(hash, sender)
	require.Equal(t, sender, app.senders[hash])

	env.ForgetEvmSender(hash)
	_, cached := app.senders[hash]
	require.False(t, cached)
}
