package core

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"

	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

type evmStateApp struct{ abevm.Application }

func (a evmStateApp) EvmChainID() uint64 { return 1 }

func (a evmStateApp) LastBlockHeight() int64                       { return 17 }
func (a evmStateApp) EvmNonce(common.Address) uint64               { return 3 }
func (a evmStateApp) EvmCommittedNonce(addr common.Address) uint64 { return a.EvmNonce(addr) }
func (a evmStateApp) EvmBalance(common.Address) uint256.Int        { return *uint256.NewInt(99) }

func signedEVMStateTestTx(t *testing.T, gas uint64) *ethtypes.Transaction {
	t.Helper()
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	tx, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.LegacyTx{Gas: gas}), ethtypes.LatestSignerForChainID(big.NewInt(1)), key)
	require.NoError(t, err)
	return tx
}

func TestGigaEnvironmentReadsStateWithoutABCI(t *testing.T) {
	app := abevm.NewProxy(evmStateApp{})
	env := newAutobahnEnvWithApp(t, false, app)
	require.Equal(t, uint64(17), env.EvmBlockNumber())
	require.Equal(t, uint64(3), env.EvmTransactionCount(common.Address{1}))
	require.Equal(t, *uint256.NewInt(99), app.EvmBalance(common.Address{1}))
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

func TestGigaEnvironmentCachesSendersThroughGigaRouter(t *testing.T) {
	for _, validator := range []bool{false, true} {
		t.Run(fmt.Sprintf("validator=%t", validator), func(t *testing.T) {
			app := &senderCacheApp{}
			proxy := abevm.NewProxy(app)
			signer := newTestSigner(proxy.EvmChainID(), 0)
			env := newAutobahnEnvWithSigner(t, validator, proxy, signer)
			tx := signedEVMStateTestTx(t, 0)
			sender := common.Address{2}
			env.RememberEvmSender(tx.Hash(), sender)
			raw := utils.OrPanic1(tx.MarshalBinary())
			_, _, err := abevm.CheckTx(proxy, signer, raw)
			require.NoError(t, err)
			require.Equal(t, utils.Some(sender), app.sender)
			env.ForgetEvmSender(tx.Hash())
			_, _, err = abevm.CheckTx(proxy, signer, raw)
			require.NoError(t, err)
			require.Equal(t, utils.Some(utils.OrPanic1(ethtypes.Sender(ethtypes.LatestSignerForChainID(tx.ChainId()), tx))), app.sender)
		})
	}
}

func newTestSigner(chainID uint64, workers int) *abevm.Signer {
	return abevm.NewSigner(ethtypes.LatestSignerForChainID(new(big.Int).SetUint64(chainID)), 1024, workers)
}
