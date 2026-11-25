package producer

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

type checkTxCountingSigner struct {
	ethtypes.Signer
	calls int
}

func (s *checkTxCountingSigner) Sender(tx *ethtypes.Transaction) (common.Address, error) {
	s.calls++
	return s.Signer.Sender(tx)
}

func TestCheckTxSenderCache(t *testing.T) {
	for _, rejected := range []bool{false, true} {
		t.Run(fmt.Sprintf("rejected=%t", rejected), func(t *testing.T) {
			rng := utils.TestRng()
			app := newTestApp()
			env := newTestEnv(t, rng, app.Cfg(), app)
			counted := &checkTxCountingSigner{Signer: ethtypes.LatestSignerForChainID(big.NewInt(1))}
			signer := abevm.NewSigner(counted, 1024, 0)
			state := NewState(app.Cfg(), env.consensus, env.app, signer)
			address, nonce := app.NewAccount(rng)
			tx := env.genTx(rng, address, nonce).transaction()
			if rejected {
				// Sign an envelope with an invalid app payload.
				keyBytes := app.SigningKey(address)
				key := utils.OrPanic1(crypto.ToECDSA(keyBytes[:]))
				tx = utils.OrPanic1(ethtypes.SignTx(ethtypes.NewTx(&ethtypes.LegacyTx{Nonce: nonce, Data: []byte{1}}), counted.Signer, key))
			}
			raw := utils.OrPanic1(tx.MarshalBinary())
			decoded, _, err := state.checkTx(t.Context(), raw)
			if rejected {
				require.ErrorIs(t, err, abevm.ErrTxRejected)
			} else {
				require.NoError(t, err)
				require.Equal(t, address, decoded.Sender())
			}
			require.Equal(t, 1, counted.calls)
			parsed, err := signer.ParseTxs([][]byte{raw})
			require.NoError(t, err)
			expected := 1
			if rejected {
				expected = 2
			}
			require.Equal(t, expected, counted.calls)
			require.Equal(t, decoded.Sender(), parsed[0].Sender())
			signer.ForgetTxs(parsed)
			if !rejected {
				_, err := env.app.FinalizeBlock(t.Context(), abevm.FinalizeBlockReq{Txs: parsed})
				require.NoError(t, err)
				require.Equal(t, nonce+1, app.EvmNonce(address))
			}
			_, err = signer.ParseTx(raw)
			require.NoError(t, err)
			require.Equal(t, expected+1, counted.calls)
		})
	}
}

func TestCheckTxRejectsMalformedInput(t *testing.T) {
	rng := utils.TestRng()
	app := newTestApp()
	env := newTestEnv(t, rng, app.Cfg(), app)
	_, _, err := env.state.checkTx(t.Context(), []byte{1})
	require.ErrorIs(t, err, abevm.ErrTxRejected)
	require.Zero(t, app.checked.Load())
}
