package occ

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"math/rand"
	"os"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/occ_tests/utils"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
)

var MintPayload = []byte{18, 73, 197, 139}
var BurnPayload = []byte{68, 223, 142, 112}

func TestMintBurn(t *testing.T) {
	blocksToTest := 100
	numAccounts := 10
	maxTxsPerBlock := 10
	burnToMintRatio := 1
	now := time.Now()
	val := utils.NewSigner()
	occWrapper := app.NewTestWrapper(t, now, val.PublicKey, false, func(ba *baseapp.BaseApp) {
		ba.SetOccEnabled(true)
		ba.SetConcurrencyWorkers(5)
	})
	occTestApp := occWrapper.App
	occCtx := occWrapper.Ctx
	seqWrapper := app.NewTestWrapper(t, now, val.PublicKey, false, func(ba *baseapp.BaseApp) {
		ba.SetOccEnabled(false)
		ba.SetConcurrencyWorkers(5)
	})
	seqTestApp := seqWrapper.App
	seqCtx := seqWrapper.Ctx
	accts := make([]utils.TestAcct, numAccounts)
	nonces := make([]uint64, numAccounts)
	funds := utils.Funds(100000000000000000)
	for i := 0; i < numAccounts; i++ {
		accts[i] = utils.NewSigner()
		panicIfErr(occTestApp.BankKeeper.MintCoins(occCtx, minttypes.ModuleName, funds))
		panicIfErr(seqTestApp.BankKeeper.MintCoins(seqCtx, minttypes.ModuleName, funds))
		panicIfErr(occTestApp.BankKeeper.SendCoinsFromModuleToAccount(occCtx, minttypes.ModuleName, accts[i].AccountAddress, funds))
		panicIfErr(seqTestApp.BankKeeper.SendCoinsFromModuleToAccount(seqCtx, minttypes.ModuleName, accts[i].AccountAddress, funds))
	}
	codeAddr := utils.NewSigner().EvmAddress
	code := readDeployedCode()
	occTestApp.EvmKeeper.SetCode(occCtx, codeAddr, code)
	seqTestApp.EvmKeeper.SetCode(seqCtx, codeAddr, code)
	chainID := occTestApp.EvmKeeper.ChainID(occCtx)
	require.Equal(t, chainID, seqTestApp.EvmKeeper.ChainID(seqCtx))
	chainCfg := evmtypes.DefaultChainConfig()
	ethCfg := chainCfg.EthereumConfig(chainID)
	for i := 0; i < blocksToTest; i++ {
		numTx := rand.Intn(maxTxsPerBlock) + 1
		fmt.Printf("number of transactions for block %d: %d\n", i, numTx)
		txsOcc := make([][]byte, numTx)
		txsSeq := make([][]byte, numTx)
		for j := 0; j < numTx; j++ {
			payload := BurnPayload
			if rand.Intn(burnToMintRatio) == 0 {
				payload = MintPayload
			}
			whichAcct := rand.Intn(numAccounts)
			acct := accts[whichAcct]
			txData := ethtypes.DynamicFeeTx{
				Nonce:     nonces[whichAcct],
				GasFeeCap: big.NewInt(10000000000),
				Gas:       50000,
				To:        &codeAddr,
				Value:     big.NewInt(0),
				Data:      payload,
				ChainID:   chainID,
			}
			nonces[whichAcct]++
			signer := ethtypes.MakeSigner(ethCfg, big.NewInt(0), uint64(now.Unix()))
			tx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, acct.EvmPrivateKey)
			panicIfErr(err)
			typedTx, err := ethtx.NewDynamicFeeTx(tx)
			panicIfErr(err)
			msg, err := evmtypes.NewMsgEVMTransaction(typedTx)
			panicIfErr(err)
			occTxBuilder := occTestApp.GetTxConfig().NewTxBuilder()
			occTxBuilder.SetMsgs(msg)
			txsOcc[j], err = occTestApp.GetTxConfig().TxEncoder()(occTxBuilder.GetTx())
			panicIfErr(err)
			seqTxBuilder := seqTestApp.GetTxConfig().NewTxBuilder()
			seqTxBuilder.SetMsgs(msg)
			txsSeq[j], err = seqTestApp.GetTxConfig().TxEncoder()(seqTxBuilder.GetTx())
			panicIfErr(err)
		}
		reqOcc := &abci.RequestFinalizeBlock{
			Txs:    txsOcc,
			Height: int64(i) + 1,
		}
		reqSeq := &abci.RequestFinalizeBlock{
			Txs:    txsSeq,
			Height: int64(i) + 1,
		}
		occCtx = occCtx.WithBlockHeight(reqOcc.Height)
		seqCtx = seqCtx.WithBlockHeight(reqSeq.Height)
		_, _, _, err := seqTestApp.ProcessBlock(seqCtx, txsSeq, reqSeq, reqSeq.DecidedLastCommit)
		panicIfErr(err)
		_, _, _, err = occTestApp.ProcessBlock(occCtx, txsOcc, reqOcc, reqOcc.DecidedLastCommit)
		panicIfErr(err)
		// verify account info
		for _, acct := range accts {
			require.Equal(t, occTestApp.BankKeeper.GetBalance(occCtx, acct.AccountAddress, "usei").Amount, seqTestApp.BankKeeper.GetBalance(seqCtx, acct.AccountAddress, "usei").Amount)
			require.Equal(t, occTestApp.EvmKeeper.GetNonce(occCtx, acct.EvmAddress), seqTestApp.EvmKeeper.GetNonce(seqCtx, acct.EvmAddress))
		}
		// verify contract state
		occTestApp.EvmKeeper.IterateState(occCtx, func(addr common.Address, key, val common.Hash) bool {
			seqState := seqTestApp.EvmKeeper.GetState(seqCtx, addr, key)
			require.Equal(t, seqState, val)
			return false
		})
		seqTestApp.SetDeliverStateToCommit()
		occTestApp.SetDeliverStateToCommit()
		seqTestApp.WriteState()
		seqTestApp.GetWorkingHash()
		seqTestApp.CommitMultiStore().Commit(true)
		occTestApp.WriteState()
		occTestApp.GetWorkingHash()
		occTestApp.CommitMultiStore().Commit(true)
		seqCtx = seqCtx.WithMultiStore(seqTestApp.CommitMultiStore().CacheMultiStore())
		occCtx = occCtx.WithMultiStore(occTestApp.CommitMultiStore().CacheMultiStore())
	}
}

func readDeployedCode() []byte {
	dat, err := os.ReadFile("../example/contracts/mintburn/deployed_bytes")
	panicIfErr(err)
	bz, err := hex.DecodeString(string(dat))
	panicIfErr(err)
	return bz
}

func panicIfErr(err error) {
	if err != nil {
		panic(err)
	}
}
