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

	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
)

type evmStateApp struct{ abevm.Application }

func (a evmStateApp) Info() *abevm.ResponseInfo {
	return &abevm.ResponseInfo{Data: "native-evm", LastBlockHeight: 17, LastBlockAppHash: []byte{42}}
}
func (a evmStateApp) EvmChainID() uint64 { return 1 }

func (a evmStateApp) LastBlockHeight() int64                       { return 17 }
func (a evmStateApp) EvmNonce(common.Address) uint64               { return 3 }
func (a evmStateApp) EvmCommittedNonce(addr common.Address) uint64 { return a.EvmNonce(addr) }
func (a evmStateApp) EvmBalance(common.Address) uint256.Int        { return *uint256.NewInt(99) }
func (a evmStateApp) CheckTx(abevm.EvmTx) (abevm.AdmittedTx, error) {
	return abevm.AdmittedTx{}, errors.New("rejected")
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
	app := abevm.NewProxy(evmStateApp{})
	env := &Environment{EVM: utils.Some(app),
		EVMSigner: utils.Some(newTestSigner(app.EvmChainID(), 0))}
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

func (a *senderCacheApp) CheckTx(tx abevm.EvmTx) (abevm.AdmittedTx, error) {
	a.sender = utils.Some(tx.Sender())
	return abevm.AdmittedTx{}, nil
}

func TestEVMEnvironmentCachesSendersThroughGigaRouter(t *testing.T) {
	for _, validator := range []bool{false, true} {
		t.Run(fmt.Sprintf("validator=%t", validator), func(t *testing.T) {
			autobahn := newAutobahnEnv(t, validator)
			app := &senderCacheApp{}
			proxy := abevm.NewProxy(app)
			env := &EVMEnvironment{
				Environment: &Environment{Router: autobahn.Router, EVM: utils.Some(proxy), EVMSigner: autobahn.EVMSigner},
				Proxy:       proxy,
			}
			tx := signedEVMStateTestTx(t, 0)
			sender := common.Address{2}
			env.RememberEvmSender(tx.Hash(), sender)
			raw := utils.OrPanic1(tx.MarshalBinary())
			_, err := env.Environment.CheckTx(t.Context(), &coretypes.RequestCheckTx{Tx: raw})
			require.NoError(t, err)
			require.Equal(t, utils.Some(sender), app.sender)
			env.ForgetEvmSender(tx.Hash())
			_, err = env.Environment.CheckTx(t.Context(), &coretypes.RequestCheckTx{Tx: raw})
			require.NoError(t, err)
			require.Equal(t, utils.Some(utils.OrPanic1(ethtypes.Sender(ethtypes.LatestSignerForChainID(tx.ChainId()), tx))), app.sender)
		})
	}
}

func TestEVMEnvironmentRejectsInvalidSignatureBeforeAdmission(t *testing.T) {
	app := &senderCacheApp{}
	env := &Environment{EVM: utils.Some(abevm.NewProxy(app)),
		EVMSigner: utils.Some(newTestSigner(app.EvmChainID(), 0))}
	raw := utils.OrPanic1(ethtypes.NewTx(&ethtypes.DynamicFeeTx{ChainID: big.NewInt(1)}).MarshalBinary())
	response, err := env.CheckTx(t.Context(), &coretypes.RequestCheckTx{Tx: raw})
	require.NoError(t, err)
	require.Equal(t, uint32(1), response.Code)
	require.False(t, app.sender.IsPresent())
}

func TestEVMAdmissionResponse(t *testing.T) {
	app := abevm.NewProxy(evmStateApp{})
	decode := func(gas uint64) abevm.EvmTx {
		return utils.OrPanic1(newTestSigner(app.EvmChainID(), 0).ParseTx(utils.OrPanic1(signedEVMStateTestTx(t, gas).MarshalBinary())))
	}
	tx := decode(30_000)
	response, err := evmAdmissionResponse(tx, abevm.AdmittedTx{GasEstimated: 21_000}, nil)
	require.NoError(t, err)
	require.Equal(t, uint32(0), response.Code)
	require.Equal(t, int64(30_000), response.GasWanted)
	require.Equal(t, int64(21_000), response.GasEstimated)
	response, err = evmAdmissionResponse(tx, abevm.AdmittedTx{}, fmt.Errorf("%w: invalid tx", abevm.ErrTxRejected))
	require.NoError(t, err)
	require.Equal(t, uint32(1), response.Code)
	_, err = evmAdmissionResponse(tx, abevm.AdmittedTx{}, context.Canceled)
	require.ErrorIs(t, err, context.Canceled)
	_, err = evmAdmissionResponse(decode(math.MaxUint64), abevm.AdmittedTx{}, nil)
	require.Error(t, err)
	_, err = evmAdmissionResponse(tx, abevm.AdmittedTx{GasEstimated: math.MaxUint64}, nil)
	require.Error(t, err)
}

func newTestSigner(chainID uint64, workers int) *abevm.Signer {
	return abevm.NewSigner(ethtypes.LatestSignerForChainID(new(big.Int).SetUint64(chainID)), 1024, workers)
}
