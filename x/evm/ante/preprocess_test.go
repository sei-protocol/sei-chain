package ante_test

import (
	"encoding/hex"
	"math/big"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/ante"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
)

func TestPreprocessAnteHandler(t *testing.T) {
	k, _, ctx := testkeeper.MockEVMKeeper()
	handler := ante.NewEVMPreprocessDecorator(k, k.AccountKeeper())
	privKey := testkeeper.MockPrivateKey()
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	to := new(common.Address)
	copy(to[:], []byte("0x1234567890abcdef1234567890abcdef12345678"))
	txData := ethtypes.LegacyTx{
		Nonce:    1,
		GasPrice: big.NewInt(10),
		Gas:      1000,
		To:       to,
		Value:    big.NewInt(1000),
		Data:     []byte("abc"),
	}
	chainID := k.ChainID(ctx)
	evmParams := k.GetParams(ctx)
	chainCfg := evmParams.GetChainConfig()
	ethCfg := chainCfg.EthereumConfig(chainID)
	blockNum := big.NewInt(ctx.BlockHeight())
	signer := ethtypes.MakeSigner(ethCfg, blockNum, uint64(ctx.BlockTime().Unix()))
	tx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	typedTx, err := ethtx.NewLegacyTx(tx)
	require.Nil(t, err)
	msg, err := types.NewMsgEVMTransaction(typedTx)
	require.Nil(t, err)
	ctx, err = handler.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Nil(t, err)
	setAddr, found := types.GetContextSeiAddress(ctx)
	require.True(t, found)
	require.Equal(t, sdk.AccAddress(privKey.PubKey().Address()), setAddr)
}

func TestGetVersion(t *testing.T) {
	ethCfg := &params.ChainConfig{}
	ctx := sdk.Context{}.WithBlockHeight(10).WithBlockTime(time.Now())
	zero := uint64(0)

	ethCfg.LondonBlock = big.NewInt(0)
	ethCfg.CancunTime = &zero
	require.Equal(t, types.Cancun, ante.GetVersion(ctx, ethCfg))

	ethCfg.CancunTime = nil
	require.Equal(t, types.London, ante.GetVersion(ctx, ethCfg))
}
