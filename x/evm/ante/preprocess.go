package ante

import (
	"encoding/hex"
	"errors"
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
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/sei-protocol/sei-chain/x/evm/derived"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
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
	evmAddr := derived.SenderEVMAddr
	seiAddr := p.evmKeeper.GetSeiAddress(ctx, evmAddr)
	ctx.EventManager().EmitEvent(sdk.NewEvent(evmtypes.EventTypeSigner,
		sdk.NewAttribute(evmtypes.AttributeKeyEvmAddress, evmAddr.Hex()),
		sdk.NewAttribute(evmtypes.AttributeKeySeiAddress, seiAddr.String())))
	if !p.accountKeeper.HasAccount(ctx, seiAddr) {
		p.accountKeeper.SetAccount(ctx, p.accountKeeper.NewAccountWithAddress(ctx, seiAddr))
	}
	if p.evmKeeper.EthReplayConfig.Enabled {
		p.evmKeeper.PrepareReplayedAddr(ctx, evmAddr)
	}

	return next(ctx, tx, simulate)
}

// only used in simulation for historical txs
func (p *EVMPreprocessDecorator) AssociateAddresses(ctx sdk.Context, seiAddr sdk.AccAddress, evmAddr common.Address, pubkey cryptotypes.PubKey) error {
	p.evmKeeper.SetAddressMapping(ctx, seiAddr, evmAddr)
	if acc := p.accountKeeper.GetAccount(ctx, seiAddr); acc.GetPubKey() == nil {
		if err := acc.SetPubKey(pubkey); err != nil {
			return err
		}
		p.accountKeeper.SetAccount(ctx, acc)
	}
	return migrateBalance(ctx, p.evmKeeper, evmAddr, seiAddr)
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
	evmAddr, seiPubkey, err := getAddresses(V, R, S, txHash)
	if err != nil {
		return err
	}
	msgEVMTransaction.Derived = &derived.Derived{
		SenderEVMAddr: evmAddr,
		PubKey:        &secp256k1.PubKey{Key: seiPubkey.Bytes()},
		Version:       version,
	}
	return nil
}

func (p *EVMPreprocessDecorator) AnteDeps(txDeps []sdkacltypes.AccessOperation, tx sdk.Tx, txIndex int, next sdk.AnteDepGenerator) (newTxDeps []sdkacltypes.AccessOperation, err error) {
	msg := evmtypes.MustGetEVMTransactionMessage(tx)
	return next(append(txDeps, sdkacltypes.AccessOperation{
		AccessType:         sdkacltypes.AccessType_WRITE,
		ResourceType:       sdkacltypes.ResourceType_KV_EVM_E2S,
		IdentifierTemplate: hex.EncodeToString(evmtypes.EVMAddressToSeiAddressKey(msg.Derived.SenderEVMAddr)),
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

func getAddresses(V *big.Int, R *big.Int, S *big.Int, data common.Hash) (common.Address, cryptotypes.PubKey, error) {
	pubkey, err := recoverPubkey(data, R, S, V, true)
	if err != nil {
		return common.Address{}, nil, err
	}
	evmAddr, err := pubkeyToEVMAddress(pubkey)
	if err != nil {
		return common.Address{}, nil, err
	}
	seiPubkey := pubkeyBytesToSeiPubKey(pubkey)
	return evmAddr, &seiPubkey, nil
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

// only used in simulation for historical txs
func migrateBalance(ctx sdk.Context, evmKeeper *evmkeeper.Keeper, evmAddr common.Address, seiAddr sdk.AccAddress) error {
	castAddr := sdk.AccAddress(evmAddr[:])
	castAddrBalances := evmKeeper.BankKeeper().GetAllBalances(ctx, castAddr)
	if !castAddrBalances.IsZero() {
		if err := evmKeeper.BankKeeper().SendCoins(ctx, castAddr, seiAddr, castAddrBalances); err != nil {
			return err
		}
	}
	castAddrWei := evmKeeper.BankKeeper().GetWeiBalance(ctx, castAddr)
	if !castAddrWei.IsZero() {
		if err := evmKeeper.BankKeeper().SendCoinsAndWei(ctx, castAddr, seiAddr, sdk.ZeroInt(), castAddrWei); err != nil {
			return err
		}
	}
	evmKeeper.AccountKeeper().RemoveAccount(ctx, authtypes.NewBaseAccountWithAddress(castAddr))
	return nil
}

//nolint:revive
func (p *EVMAddressDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	sigTx, ok := tx.(authsigning.SigVerifiableTx)
	if !ok {
		return ctx, sdkerrors.Wrap(sdkerrors.ErrTxDecode, "invalid tx type")
	}
	signers := sigTx.GetSigners()
	for _, signer := range signers {
		ctx.EventManager().EmitEvent(sdk.NewEvent(evmtypes.EventTypeSigner,
			sdk.NewAttribute(evmtypes.AttributeKeyEvmAddress, p.evmKeeper.GetEVMAddress(ctx, signer).Hex()),
			sdk.NewAttribute(evmtypes.AttributeKeySeiAddress, signer.String())))
	}
	return next(ctx, tx, simulate)
}
