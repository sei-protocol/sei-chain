package core

import (
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
	return signEVMStateTestTx(t, ethtypes.NewTx(&ethtypes.LegacyTx{Nonce: 3, Gas: gas}), ethtypes.LatestSignerForChainID(big.NewInt(1)))
}

func signEVMStateTestTx(t *testing.T, tx *ethtypes.Transaction, signer ethtypes.Signer) *ethtypes.Transaction {
	t.Helper()
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	signed, err := ethtypes.SignTx(tx, signer, key)
	require.NoError(t, err)
	return signed
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
	sender   utils.Option[common.Address]
	checkErr utils.Option[error]
}

func (a *senderCacheApp) CheckTx(tx abevm.EvmTx) (abevm.AdmittedTx, error) {
	a.sender = utils.Some(tx.Sender())
	if err, ok := a.checkErr.Get(); ok {
		return abevm.AdmittedTx{}, err
	}
	return abevm.AdmittedTx{}, nil
}

func newTestSigner(chainID uint64, workers int) *abevm.Signer {
	return abevm.NewSigner(ethtypes.LatestSignerForChainID(new(big.Int).SetUint64(chainID)), 1024, workers)
}
