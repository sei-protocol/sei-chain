package ante

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"

	"github.com/btcsuite/btcd/btcec"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
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
	"github.com/sei-protocol/sei-chain/x/evm/derived"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
)

// Accounts need to have at least 1Sei to force association. Note that account won't be charged.
const BalanceThreshold uint64 = 1000000

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
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeter())

	derived := msg.Derived
	seiAddr := derived.SenderSeiAddr
	evmAddr := derived.SenderEVMAddr
	pubkey := derived.PubKey
	isAssociateTx := derived.IsAssociate
	_, isAssociated := p.evmKeeper.GetEVMAddress(ctx, seiAddr)
	if isAssociateTx && isAssociated {
		return ctx, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "account already has association set")
	} else if isAssociateTx {
		// check if the account has enough balance (without charging)
		baseDenom := p.evmKeeper.GetBaseDenom(ctx)
		seiBalance := p.evmKeeper.BankKeeper().GetBalance(ctx, seiAddr, baseDenom).Amount
		// no need to get wei balance here since the sei address is not used directly in EVM and thus does not
		// contain any wei, so any wei balance in `sdk.AccAddress(evmAddr[:])` would not add up to 1usei anyway.
		castBalance := p.evmKeeper.BankKeeper().GetBalance(ctx, sdk.AccAddress(evmAddr[:]), baseDenom).Amount
		if new(big.Int).Add(seiBalance.BigInt(), castBalance.BigInt()).Cmp(new(big.Int).SetUint64(BalanceThreshold)) < 0 {
			return ctx, sdkerrors.Wrap(sdkerrors.ErrInsufficientFunds, "account needs to have at least 1Sei to force association")
		}
		if err := p.associateAddresses(ctx, seiAddr, evmAddr, pubkey); err != nil {
			return ctx, err
		}
		return ctx.WithPriority(antedecorators.EVMAssociatePriority), nil // short-circuit without calling next
	} else if isAssociated {
		// noop; for readability
	} else {
		// not associatedTx and not already associated
		if err := p.associateAddresses(ctx, seiAddr, evmAddr, pubkey); err != nil {
			return ctx, err
		}
		if p.evmKeeper.EthReplayConfig.Enabled {
			p.evmKeeper.PrepareReplayedAddr(ctx, evmAddr)
		}
	}

	return next(ctx, tx, simulate)
}

func (p *EVMPreprocessDecorator) associateAddresses(ctx sdk.Context, seiAddr sdk.AccAddress, evmAddr common.Address, pubkey cryptotypes.PubKey) error {
	p.evmKeeper.SetAddressMapping(ctx, seiAddr, evmAddr)
	if acc := p.accountKeeper.GetAccount(ctx, seiAddr); acc.GetPubKey() == nil {
		if err := acc.SetPubKey(pubkey); err != nil {
			return err
		}
		p.accountKeeper.SetAccount(ctx, acc)
	}
	castAddr := sdk.AccAddress(evmAddr[:])
	castAddrBalances := p.evmKeeper.BankKeeper().GetAllBalances(ctx, castAddr)
	if !castAddrBalances.IsZero() {
		if err := p.evmKeeper.BankKeeper().SendCoins(ctx, castAddr, seiAddr, castAddrBalances); err != nil {
			return err
		}
	}
	castAddrWei := p.evmKeeper.BankKeeper().GetWeiBalance(ctx, castAddr)
	if !castAddrWei.IsZero() {
		if err := p.evmKeeper.BankKeeper().SendCoinsAndWei(ctx, castAddr, seiAddr, sdk.ZeroInt(), castAddrWei); err != nil {
			return err
		}
	}
	p.evmKeeper.AccountKeeper().RemoveAccount(ctx, authtypes.NewBaseAccountWithAddress(castAddr))
	return nil
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
		evmAddr, seiAddr, pubkey, err := getAddresses(V, R, S, customMessageHash)
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
	evmAddr, seiAddr, seiPubkey, err := getAddresses(V, R, S, txHash)
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

func getAddresses(V *big.Int, R *big.Int, S *big.Int, data common.Hash) (common.Address, sdk.AccAddress, cryptotypes.PubKey, error) {
	pubkey, err := recoverPubkey(data, R, S, V, true)
	if err != nil {
		return common.Address{}, sdk.AccAddress{}, nil, err
	}
	evmAddr, err := pubkeyToEVMAddress(pubkey)
	if err != nil {
		return common.Address{}, sdk.AccAddress{}, nil, err
	}
	seiPubkey := pubkeyBytesToSeiPubKey(pubkey)
	seiAddr := sdk.AccAddress(seiPubkey.Address())
	return evmAddr, seiAddr, &seiPubkey, nil
}

// first half of go-ethereum/core/types/transaction_signing.go:recoverPlain
func recoverPubkey(sighash common.Hash, R, S, Vb *big.Int, homestead bool) ([]byte, error) {
	if Vb.BitLen() > 8 {
		return []byte{}, ethtypes.ErrInvalidSig
	}
	V := byte(Vb.Uint64() - 27)
	if !crypto.ValidateSignatureValues(V, R, S, homestead) {
		return []byte{}, ethtypes.ErrInvalidSig
	}
	// encode the signature in uncompressed format
	r, s := R.Bytes(), S.Bytes()
	sig := make([]byte, crypto.SignatureLength)
	copy(sig[32-len(r):32], r)
	copy(sig[64-len(s):64], s)
	sig[64] = V
	// recover the public key from the signature
	return crypto.Ecrecover(sighash[:], sig)
}

// second half of go-ethereum/core/types/transaction_signing.go:recoverPlain
func pubkeyToEVMAddress(pub []byte) (common.Address, error) {
	if len(pub) == 0 || pub[0] != 4 {
		return common.Address{}, errors.New("invalid public key")
	}
	var addr common.Address
	copy(addr[:], crypto.Keccak256(pub[1:])[12:])
	return addr, nil
}

func pubkeyBytesToSeiPubKey(pub []byte) secp256k1.PubKey {
	pubKey, _ := crypto.UnmarshalPubkey(pub)
	pubkeyObj := (*btcec.PublicKey)(pubKey)
	return secp256k1.PubKey{Key: pubkeyObj.SerializeCompressed()}
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
		if _, associated := p.evmKeeper.GetEVMAddress(ctx, signer); associated {
			continue
		}
		acc := p.accountKeeper.GetAccount(ctx, signer)
		if acc.GetPubKey() == nil {
			ctx.Logger().Error(fmt.Sprintf("missing pubkey for %s", signer.String()))
			continue
		}
		pk, err := btcec.ParsePubKey(acc.GetPubKey().Bytes(), btcec.S256())
		if err != nil {
			ctx.Logger().Error(fmt.Sprintf("failed to parse pubkey for %s", err))
			continue
		}
		evmAddr, err := pubkeyToEVMAddress(pk.SerializeUncompressed())
		if err != nil {
			ctx.Logger().Error(fmt.Sprintf("failed to get EVM address from pubkey due to %s", err))
			continue
		}
		p.evmKeeper.SetAddressMapping(ctx, signer, evmAddr)
	}
	return next(ctx, tx, simulate)
}
