package core

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
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
func (a evmStateApp) LastBlockHeight() int64                       { return 17 }
func (a evmStateApp) EvmNonce(common.Address) uint64               { return 3 }
func (a evmStateApp) EvmCommittedNonce(addr common.Address) uint64 { return a.EvmNonce(addr) }
func (a evmStateApp) EvmBalance(common.Address) uint256.Int        { return *uint256.NewInt(99) }
func (a evmStateApp) CheckTx(proxy.EvmTx) (proxy.AdmittedTx, error) {
	return proxy.AdmittedTx{}, errors.New("rejected")
}

func signedEVMStateTestTx(t *testing.T, gas uint64) *ethtypes.Transaction {
	t.Helper()
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	tx, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.LegacyTx{Gas: gas}), ethtypes.LatestSignerForChainID(big.NewInt(1)), key)
	require.NoError(t, err)
	return tx
}

func TestEVMEnvironmentReadsStateWithoutABCI(t *testing.T) {
	app := proxy.NewEVM(evmStateApp{})
	env := &Environment{EVM: utils.Some(app)}
	require.False(t, env.App.IsPresent())
	require.Equal(t, uint64(17), env.EvmBlockNumber())
	require.Equal(t, uint64(3), env.EvmTransactionCount(common.Address{1}))
	require.Equal(t, *uint256.NewInt(99), app.EvmBalance(common.Address{1}))
	info, err := env.ABCIInfo(t.Context())
	require.NoError(t, err)
	require.Equal(t, "native-evm", info.Response.Data)
	require.Equal(t, int64(17), info.Response.LastBlockHeight)
	require.Equal(t, []byte{42}, info.Response.LastBlockAppHash)
	res, err := env.CheckTx(t.Context(), &coretypes.RequestCheckTx{Tx: utils.OrPanic1(signedEVMStateTestTx(t, 0).MarshalBinary())})
	require.NoError(t, err)
	require.Equal(t, uint32(1), res.Code)
	require.NotEmpty(t, res.Log)
	require.Equal(t, int64(0), res.GasWanted)
	_, err = env.ABCIQuery(t.Context(), &coretypes.RequestABCIQuery{})
	require.Error(t, err)
}

// senderCacheApp records the cached sender delivered to admission.
type senderCacheApp struct {
	evmStateApp
	sender utils.Option[common.Address]
}

func (a *senderCacheApp) CheckTx(tx proxy.EvmTx) (proxy.AdmittedTx, error) {
	a.sender = utils.Some(tx.Sender())
	return proxy.AdmittedTx{}, nil
}

func TestEVMEnvironmentCachesSendersWithoutABCI(t *testing.T) {
	app := &senderCacheApp{}
	env := &EVMEnvironment{Environment: &Environment{}, EVMProxy: proxy.NewEVM(app)}
	tx := signedEVMStateTestTx(t, 0)
	sender := common.Address{2}
	env.RememberEvmSender(tx.Hash(), sender)
	raw := utils.OrPanic1(tx.MarshalBinary())
	decoded := utils.OrPanic1(env.EVMProxy.DecodeTx(raw))
	_, err := env.EVMProxy.CheckTx(decoded)
	require.NoError(t, err)
	require.Equal(t, utils.Some(sender), app.sender)
	env.ForgetEvmSender(tx.Hash())
	decoded, err = env.EVMProxy.DecodeTx(raw)
	require.NoError(t, err)
	_, err = env.EVMProxy.CheckTx(decoded)
	require.NoError(t, err)
	require.Equal(t, utils.Some(utils.OrPanic1(ethtypes.Sender(ethtypes.LatestSignerForChainID(tx.ChainId()), tx))), app.sender)
}

func TestEVMEnvironmentRejectsInvalidSignatureBeforeAdmission(t *testing.T) {
	app := &senderCacheApp{}
	env := &Environment{EVM: utils.Some(proxy.NewEVM(app))}
	raw := utils.OrPanic1(ethtypes.NewTx(&ethtypes.LegacyTx{}).MarshalBinary())
	response, err := env.CheckTx(t.Context(), &coretypes.RequestCheckTx{Tx: raw})
	require.NoError(t, err)
	require.Equal(t, uint32(1), response.Code)
	require.False(t, app.sender.IsPresent())
}

func TestEVMAdmissionResponse(t *testing.T) {
	app := proxy.NewEVM(evmStateApp{})
	decode := func(gas uint64) proxy.EvmTx {
		return utils.OrPanic1(app.DecodeTx(utils.OrPanic1(signedEVMStateTestTx(t, gas).MarshalBinary())))
	}
	tx := decode(30_000)
	response, err := evmAdmissionResponse(tx, proxy.AdmittedTx{GasEstimated: 21_000}, nil)
	require.NoError(t, err)
	require.Equal(t, uint32(0), response.Code)
	require.Equal(t, int64(30_000), response.GasWanted)
	require.Equal(t, int64(21_000), response.GasEstimated)
	response, err = evmAdmissionResponse(tx, proxy.AdmittedTx{}, fmt.Errorf("%w: invalid tx", proxy.ErrTxRejected))
	require.NoError(t, err)
	require.Equal(t, uint32(1), response.Code)
	_, err = evmAdmissionResponse(tx, proxy.AdmittedTx{}, context.Canceled)
	require.ErrorIs(t, err, context.Canceled)
	_, err = evmAdmissionResponse(decode(math.MaxUint64), proxy.AdmittedTx{}, nil)
	require.Error(t, err)
	_, err = evmAdmissionResponse(tx, proxy.AdmittedTx{GasEstimated: math.MaxUint64}, nil)
	require.Error(t, err)
}
