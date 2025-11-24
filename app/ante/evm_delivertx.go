package ante

import (
	"math/big"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	upgradekeeper "github.com/cosmos/cosmos-sdk/x/upgrade/keeper"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/x/evm/derived"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
)

func EvmDeliverTxAnte(
	ctx sdk.Context,
	txConfig client.TxConfig,
	tx sdk.Tx,
	upgradeKeeper *upgradekeeper.Keeper,
	ek *evmkeeper.Keeper,
) (returnCtx sdk.Context, returnErr error) {
	chainID := ek.ChainID(ctx)
	if err := EvmStatelessChecks(ctx, tx, chainID); err != nil {
		return ctx, err
	}
	msg := tx.GetMsgs()[0].(*evmtypes.MsgEVMTransaction)
	txData, _ := evmtypes.UnpackTxData(msg.Data) // cached and validated
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	if atx, ok := txData.(*ethtx.AssociateTx); ok {
		return HandleAssociateTx(ctx, ek, atx, false)
	}
	etx := ethtypes.NewTx(txData.AsEthereumData())
	evmAddr, version, err := EvmDeliverHandleSignatures(ctx, ek, txData, chainID, msg)
	if err != nil {
		return ctx, err
	}
	ctx = DecorateNonceCallback(ctx, ek, evmAddr, etx.Nonce())
	if err := EvmDeliverChargeFees(ctx, ek, upgradeKeeper, txData, etx, msg, version, evmAddr); err != nil {
		return ctx, err
	}
	return DecorateContext(ctx, ek, tx, txData, etx, evmAddr), nil
}

func EvmDeliverHandleSignatures(ctx sdk.Context, ek *evmkeeper.Keeper, txData ethtx.TxData, chainID *big.Int, msg *evmtypes.MsgEVMTransaction) (common.Address, derived.SignerVersion, error) {
	evmAddr, seiAddr, seiPubkey, version, err := CheckAndDecodeSignature(ctx, txData, chainID)
	if err != nil {
		return evmAddr, version, err
	}
	if err := AssociateAddress(ctx, ek, evmAddr, seiAddr, seiPubkey); err != nil {
		return evmAddr, version, err
	}
	msg.Derived = &derived.Derived{
		SenderEVMAddr: evmAddr,
		SenderSeiAddr: seiAddr,
		PubKey:        &secp256k1.PubKey{Key: seiPubkey.Bytes()},
		Version:       version,
		IsAssociate:   false,
	}
	return evmAddr, version, nil
}

func EvmDeliverChargeFees(ctx sdk.Context, ek *evmkeeper.Keeper, upgradeKeeper *upgradekeeper.Keeper, txData ethtx.TxData, etx *ethtypes.Transaction, msg *evmtypes.MsgEVMTransaction, version derived.SignerVersion, evmAddr common.Address) error {
	stateDB, st, err := EvmCheckAndChargeFees(ctx, evmAddr, ek, upgradeKeeper, txData, etx, msg, version)
	if err != nil {
		return err
	}
	if err := st.StatelessChecks(); err != nil {
		return err
	}
	surplus, err := stateDB.Finalize()
	if err != nil {
		return err
	}
	return ek.AddAnteSurplus(ctx, etx.Hash(), surplus)
}

func DecorateNonceCallback(ctx sdk.Context, ek *evmkeeper.Keeper, evmAddr common.Address, txNonce uint64) sdk.Context {
	startingNonce := ek.GetNonce(ctx, evmAddr)
	return ctx.WithDeliverTxCallback(func(callCtx sdk.Context) {
		// bump nonce if it is for some reason not incremented (e.g. ante failure)
		if ek.GetNonce(callCtx, evmAddr) == startingNonce {
			ek.SetNonce(callCtx, evmAddr, startingNonce+1)
		}
	})
}
