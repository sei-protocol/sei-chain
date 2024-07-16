package ante

import (
	"encoding/hex"
	"fmt"
	"github.com/sei-protocol/sei-chain/utils/helpers"
	"math/big"

	"github.com/btcsuite/btcd/btcec"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	accountkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/sei-protocol/sei-chain/app/antedecorators"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/utils/metrics"
	"github.com/sei-protocol/sei-chain/x/evm/derived"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
)

// Accounts need to have at least 1Sei to force association. Note that account won't be charged.
const BalanceThreshold uint64 = 1000000

var BigBalanceThreshold *big.Int = new(big.Int).SetUint64(BalanceThreshold)
var BigBalanceThresholdMinus1 *big.Int = new(big.Int).SetUint64(BalanceThreshold - 1)

var SignerMap = map[derived.SignerVersion]func(*big.Int) ethtypes.Signer{
	derived.London: ethtypes.NewLondonSigner,
	derived.Cancun: ethtypes.NewCancunSigner,
}
var AllowedTxTypes = map[derived.SignerVersion][]uint8{
	derived.London: {ethtypes.LegacyTxType, ethtypes.AccessListTxType, ethtypes.DynamicFeeTxType},
	derived.Cancun: {ethtypes.LegacyTxType, ethtypes.AccessListTxType, ethtypes.DynamicFeeTxType, ethtypes.BlobTxType},
}

type EVMPreprocessDecorator struct {
	evmKeeper     *evmkeeper.Keeper
	accountKeeper *accountkeeper.AccountKeeper
}

func NewEVMPreprocessDecorator(evmKeeper *evmkeeper.Keeper, accountKeeper *accountkeeper.AccountKeeper) *EVMPreprocessDecorator {
	return &EVMPreprocessDecorator{evmKeeper: evmKeeper, accountKeeper: accountKeeper}
}

//nolint:revive
func (p *EVMPreprocessDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	msg := evmtypes.MustGetEVMTransactionMessage(tx)
	if err := Preprocess(ctx, msg); err != nil {
		return ctx, err
	}

	// use infinite gas meter for EVM transaction because EVM handles gas checking from within
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))

	derived := msg.Derived
	seiAddr := derived.SenderSeiAddr
	evmAddr := derived.SenderEVMAddr
	ctx.EventManager().EmitEvent(sdk.NewEvent(evmtypes.EventTypeSigner,
		sdk.NewAttribute(evmtypes.AttributeKeyEvmAddress, evmAddr.Hex()),
		sdk.NewAttribute(evmtypes.AttributeKeySeiAddress, seiAddr.String())))
	pubkey := derived.PubKey
	isAssociateTx := derived.IsAssociate
	associateHelper := helpers.NewAssociationHelper(p.evmKeeper, p.evmKeeper.BankKeeper(), p.accountKeeper)
	_, isAssociated := p.evmKeeper.GetEVMAddress(ctx, seiAddr)
	if isAssociateTx && isAssociated {
		return ctx, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "account already has association set")
	} else if isAssociateTx {
		// check if the account has enough balance (without charging)
		if !p.IsAccountBalancePositive(ctx, seiAddr, evmAddr) {
			metrics.IncrementAssociationError("associate_tx_insufficient_funds", evmtypes.NewAssociationMissingErr(seiAddr.String()))
			return ctx, sdkerrors.Wrap(sdkerrors.ErrInsufficientFunds, "account needs to have at least 1 wei to force association")
		}
		if err := associateHelper.AssociateAddresses(ctx, seiAddr, evmAddr, pubkey); err != nil {
			return ctx, err
		}

		return ctx.WithPriority(antedecorators.EVMAssociatePriority), nil // short-circuit without calling next
	} else if isAssociated {
		// noop; for readability
	} else {
		// not associatedTx and not already associated
		if err := associateHelper.AssociateAddresses(ctx, seiAddr, evmAddr, pubkey); err != nil {
			return ctx, err
		}
		if p.evmKeeper.EthReplayConfig.Enabled {
			p.evmKeeper.PrepareReplayedAddr(ctx, evmAddr)
		}
	}

	return next(ctx, tx, simulate)
}

func (p *EVMPreprocessDecorator) IsAccountBalancePositive(ctx sdk.Context, seiAddr sdk.AccAddress, evmAddr common.Address) bool {
	baseDenom := p.evmKeeper.GetBaseDenom(ctx)
	if amt := p.evmKeeper.BankKeeper().GetBalance(ctx, seiAddr, baseDenom).Amount; amt.IsPositive() {
		return true
	}
	if amt := p.evmKeeper.BankKeeper().GetBalance(ctx, sdk.AccAddress(evmAddr[:]), baseDenom).Amount; amt.IsPositive() {
		return true
	}
	if amt := p.evmKeeper.BankKeeper().GetWeiBalance(ctx, seiAddr); amt.IsPositive() {
		return true
	}
	return p.evmKeeper.BankKeeper().GetWeiBalance(ctx, sdk.AccAddress(evmAddr[:])).IsPositive()
}

// stateless
func Preprocess(ctx sdk.Context, msgEVMTransaction *evmtypes.MsgEVMTransaction) error {
	if msgEVMTransaction.Derived != nil {
		if msgEVMTransaction.Derived.PubKey == nil {
			// this means the message has `Derived` set from the outside, in which case we should reject
			return sdkerrors.ErrInvalidPubKey
		}
		// already preprocessed
		return nil
	}
	txData, err := evmtypes.UnpackTxData(msgEVMTransaction.Data)
	if err != nil {
		return err
	}

	if atx, ok := txData.(*ethtx.AssociateTx); ok {
		V, R, S := atx.GetRawSignatureValues()
		V = new(big.Int).Add(V, utils.Big27)
		// Hash custom message passed in
		customMessageHash := crypto.Keccak256Hash([]byte(atx.CustomMessage))
		evmAddr, seiAddr, pubkey, err := helpers.GetAddresses(V, R, S, customMessageHash)
		if err != nil {
			return err
		}
		msgEVMTransaction.Derived = &derived.Derived{
			SenderEVMAddr: evmAddr,
			SenderSeiAddr: seiAddr,
			PubKey:        &secp256k1.PubKey{Key: pubkey.Bytes()},
			Version:       derived.Cancun,
			IsAssociate:   true,
		}
		return nil
	}

	ethTx := ethtypes.NewTx(txData.AsEthereumData())
	chainID := ethTx.ChainId()
	chainCfg := evmtypes.DefaultChainConfig()
	ethCfg := chainCfg.EthereumConfig(chainID)
	version := GetVersion(ctx, ethCfg)
	signer := SignerMap[version](chainID)
	if !isTxTypeAllowed(version, ethTx.Type()) {
		return ethtypes.ErrInvalidChainId
	}

	var txHash common.Hash
	V, R, S := ethTx.RawSignatureValues()
	if ethTx.Protected() {
		V = AdjustV(V, ethTx.Type(), ethCfg.ChainID)
		txHash = signer.Hash(ethTx)
	} else {
		txHash = ethtypes.FrontierSigner{}.Hash(ethTx)
	}
	evmAddr, seiAddr, seiPubkey, err := helpers.GetAddresses(V, R, S, txHash)
	if err != nil {
		return err
	}
	msgEVMTransaction.Derived = &derived.Derived{
		SenderEVMAddr: evmAddr,
		SenderSeiAddr: seiAddr,
		PubKey:        &secp256k1.PubKey{Key: seiPubkey.Bytes()},
		Version:       version,
		IsAssociate:   false,
	}
	return nil
}

func (p *EVMPreprocessDecorator) AnteDeps(txDeps []sdkacltypes.AccessOperation, tx sdk.Tx, txIndex int, next sdk.AnteDepGenerator) (newTxDeps []sdkacltypes.AccessOperation, err error) {
	msg := evmtypes.MustGetEVMTransactionMessage(tx)
	return next(append(txDeps, sdkacltypes.AccessOperation{
		AccessType:         sdkacltypes.AccessType_READ,
		ResourceType:       sdkacltypes.ResourceType_KV_EVM_S2E,
		IdentifierTemplate: hex.EncodeToString(evmtypes.SeiAddressToEVMAddressKey(msg.Derived.SenderSeiAddr)),
	}, sdkacltypes.AccessOperation{
		AccessType:         sdkacltypes.AccessType_WRITE,
		ResourceType:       sdkacltypes.ResourceType_KV_EVM_S2E,
		IdentifierTemplate: hex.EncodeToString(evmtypes.SeiAddressToEVMAddressKey(msg.Derived.SenderSeiAddr)),
	}, sdkacltypes.AccessOperation{
		AccessType:         sdkacltypes.AccessType_WRITE,
		ResourceType:       sdkacltypes.ResourceType_KV_EVM_E2S,
		IdentifierTemplate: hex.EncodeToString(evmtypes.EVMAddressToSeiAddressKey(msg.Derived.SenderEVMAddr)),
	}, sdkacltypes.AccessOperation{
		AccessType:         sdkacltypes.AccessType_READ,
		ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
		IdentifierTemplate: hex.EncodeToString(banktypes.CreateAccountBalancesPrefix(msg.Derived.SenderSeiAddr)),
	}, sdkacltypes.AccessOperation{
		AccessType:         sdkacltypes.AccessType_WRITE,
		ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
		IdentifierTemplate: hex.EncodeToString(banktypes.CreateAccountBalancesPrefix(msg.Derived.SenderSeiAddr)),
	}, sdkacltypes.AccessOperation{
		AccessType:         sdkacltypes.AccessType_READ,
		ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
		IdentifierTemplate: hex.EncodeToString(banktypes.CreateAccountBalancesPrefix(msg.Derived.SenderEVMAddr[:])),
	}, sdkacltypes.AccessOperation{
		AccessType:         sdkacltypes.AccessType_WRITE,
		ResourceType:       sdkacltypes.ResourceType_KV_BANK_BALANCES,
		IdentifierTemplate: hex.EncodeToString(banktypes.CreateAccountBalancesPrefix(msg.Derived.SenderEVMAddr[:])),
	}, sdkacltypes.AccessOperation{
		AccessType:         sdkacltypes.AccessType_READ,
		ResourceType:       sdkacltypes.ResourceType_KV_AUTH_ADDRESS_STORE,
		IdentifierTemplate: hex.EncodeToString(authtypes.AddressStoreKey(msg.Derived.SenderSeiAddr)),
	}, sdkacltypes.AccessOperation{
		AccessType:         sdkacltypes.AccessType_WRITE,
		ResourceType:       sdkacltypes.ResourceType_KV_AUTH_ADDRESS_STORE,
		IdentifierTemplate: hex.EncodeToString(authtypes.AddressStoreKey(msg.Derived.SenderSeiAddr)),
	}, sdkacltypes.AccessOperation{
		AccessType:         sdkacltypes.AccessType_READ,
		ResourceType:       sdkacltypes.ResourceType_KV_AUTH_ADDRESS_STORE,
		IdentifierTemplate: hex.EncodeToString(authtypes.AddressStoreKey(msg.Derived.SenderEVMAddr[:])),
	}, sdkacltypes.AccessOperation{
		AccessType:         sdkacltypes.AccessType_WRITE,
		ResourceType:       sdkacltypes.ResourceType_KV_AUTH_ADDRESS_STORE,
		IdentifierTemplate: hex.EncodeToString(authtypes.AddressStoreKey(msg.Derived.SenderEVMAddr[:])),
	}, sdkacltypes.AccessOperation{
		AccessType:         sdkacltypes.AccessType_READ,
		ResourceType:       sdkacltypes.ResourceType_KV_EVM_NONCE,
		IdentifierTemplate: hex.EncodeToString(append(evmtypes.NonceKeyPrefix, msg.Derived.SenderEVMAddr[:]...)),
	}), tx, txIndex)
}

func isTxTypeAllowed(version derived.SignerVersion, txType uint8) bool {
	for _, t := range AllowedTxTypes[version] {
		if t == txType {
			return true
		}
	}
	return false
}

func AdjustV(V *big.Int, txType uint8, chainID *big.Int) *big.Int {
	// Non-legacy TX always needs to be bumped by 27
	if txType != ethtypes.LegacyTxType {
		return new(big.Int).Add(V, utils.Big27)
	}

	// legacy TX needs to be adjusted based on chainID
	V = new(big.Int).Sub(V, new(big.Int).Mul(chainID, utils.Big2))
	return V.Sub(V, utils.Big8)
}

func GetVersion(ctx sdk.Context, ethCfg *params.ChainConfig) derived.SignerVersion {
	blockNum := big.NewInt(ctx.BlockHeight())
	ts := uint64(ctx.BlockTime().Unix())
	switch {
	case ethCfg.IsCancun(blockNum, ts):
		return derived.Cancun
	default:
		return derived.London
	}
}

type EVMAddressDecorator struct {
	evmKeeper     *evmkeeper.Keeper
	accountKeeper *accountkeeper.AccountKeeper
}

func NewEVMAddressDecorator(evmKeeper *evmkeeper.Keeper, accountKeeper *accountkeeper.AccountKeeper) *EVMAddressDecorator {
	return &EVMAddressDecorator{evmKeeper: evmKeeper, accountKeeper: accountKeeper}
}

//nolint:revive
func (p *EVMAddressDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	sigTx, ok := tx.(authsigning.SigVerifiableTx)
	if !ok {
		return ctx, sdkerrors.Wrap(sdkerrors.ErrTxDecode, "invalid tx type")
	}
	signers := sigTx.GetSigners()
	for _, signer := range signers {
		if evmAddr, associated := p.evmKeeper.GetEVMAddress(ctx, signer); associated {
			ctx.EventManager().EmitEvent(sdk.NewEvent(evmtypes.EventTypeSigner,
				sdk.NewAttribute(evmtypes.AttributeKeyEvmAddress, evmAddr.Hex()),
				sdk.NewAttribute(evmtypes.AttributeKeySeiAddress, signer.String())))
			continue
		}
		acc := p.accountKeeper.GetAccount(ctx, signer)
		if acc.GetPubKey() == nil {
			ctx.Logger().Error(fmt.Sprintf("missing pubkey for %s", signer.String()))
			ctx.EventManager().EmitEvent(sdk.NewEvent(evmtypes.EventTypeSigner,
				sdk.NewAttribute(evmtypes.AttributeKeySeiAddress, signer.String())))
			continue
		}
		pk, err := btcec.ParsePubKey(acc.GetPubKey().Bytes(), btcec.S256())
		if err != nil {
			ctx.Logger().Debug(fmt.Sprintf("failed to parse pubkey for %s, likely due to the fact that it isn't on secp256k1 curve", acc.GetPubKey()), "err", err)
			ctx.EventManager().EmitEvent(sdk.NewEvent(evmtypes.EventTypeSigner,
				sdk.NewAttribute(evmtypes.AttributeKeySeiAddress, signer.String())))
			continue
		}
		evmAddr, err := helpers.PubkeyToEVMAddress(pk.SerializeUncompressed())
		if err != nil {
			ctx.Logger().Error(fmt.Sprintf("failed to get EVM address from pubkey due to %s", err))
			ctx.EventManager().EmitEvent(sdk.NewEvent(evmtypes.EventTypeSigner,
				sdk.NewAttribute(evmtypes.AttributeKeySeiAddress, signer.String())))
			continue
		}
		ctx.EventManager().EmitEvent(sdk.NewEvent(evmtypes.EventTypeSigner,
			sdk.NewAttribute(evmtypes.AttributeKeyEvmAddress, evmAddr.Hex()),
			sdk.NewAttribute(evmtypes.AttributeKeySeiAddress, signer.String())))
		p.evmKeeper.SetAddressMapping(ctx, signer, evmAddr)
		associationHelper := helpers.NewAssociationHelper(p.evmKeeper, p.evmKeeper.BankKeeper(), p.accountKeeper)
		if err := associationHelper.MigrateBalance(ctx, evmAddr, signer); err != nil {
			ctx.Logger().Error(fmt.Sprintf("failed to migrate EVM address balance (%s) %s", evmAddr.Hex(), err))
			return ctx, err
		}
		if evmtypes.IsTxMsgAssociate(tx) {
			// check if there is non-zero balance
			if !p.evmKeeper.BankKeeper().GetBalance(ctx, signer, sdk.MustGetBaseDenom()).IsPositive() && !p.evmKeeper.BankKeeper().GetWeiBalance(ctx, signer).IsPositive() {
				return ctx, sdkerrors.Wrap(sdkerrors.ErrInsufficientFunds, "account needs to have at least 1 wei to force association")
			}
		}
	}
	return next(ctx, tx, simulate)
}
