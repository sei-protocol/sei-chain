package ante

import (
	"errors"
	"math/big"

	"github.com/btcsuite/btcd/btcec"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	accountkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/sei-protocol/sei-chain/app/antedecorators"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
)

// Accounts need to have at least 1Sei to force association. Note that account won't be charged.
const BalanceThreshold uint64 = 1000000

var SignerMap = map[evmtypes.SignerVersion]func(*big.Int) ethtypes.Signer{
	evmtypes.London: ethtypes.NewLondonSigner,
	evmtypes.Cancun: ethtypes.NewCancunSigner,
}
var AllowedTxTypes = map[evmtypes.SignerVersion][]uint8{
	evmtypes.London: {ethtypes.LegacyTxType, ethtypes.AccessListTxType, ethtypes.DynamicFeeTxType},
	evmtypes.Cancun: {ethtypes.LegacyTxType, ethtypes.AccessListTxType, ethtypes.DynamicFeeTxType, ethtypes.BlobTxType},
}

type EVMPreprocessDecorator struct {
	evmKeeper     *evmkeeper.Keeper
	accountKeeper *accountkeeper.AccountKeeper
}

func NewEVMPreprocessDecorator(evmKeeper *evmkeeper.Keeper, accountKeeper *accountkeeper.AccountKeeper) *EVMPreprocessDecorator {
	return &EVMPreprocessDecorator{evmKeeper: evmKeeper, accountKeeper: accountKeeper}
}

func (p *EVMPreprocessDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	pctx, err := Preprocess(ctx, tx.GetMsgs()[0].(*evmtypes.MsgEVMTransaction), p.evmKeeper.GetParams(ctx))
	if err != nil {
		return ctx, err
	}
	ctx = pctx

	seiAddr, found := evmtypes.GetContextSeiAddress(ctx)
	if !found {
		return ctx, errors.New("missing sei address from context, which is supposed to be set during preprocess")
	}
	evmAddr, found := evmtypes.GetContextEVMAddress(ctx)
	if !found {
		return ctx, errors.New("missing evm address from context, which is supposed to be set during preprocess")
	}
	pubkey, found := evmtypes.GetContextPubkey(ctx)
	if !found {
		return ctx, errors.New("missing pubkey from context, which is supposed to be set during preprocess")
	}
	isAssociateTx := evmtypes.GetContextIsAssociateTx(ctx)
	_, isAssociated := p.evmKeeper.GetEVMAddress(ctx, seiAddr)
	if isAssociateTx && isAssociated {
		return ctx, sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "account already has association set")
	} else if isAssociateTx {
		// check if the account has enough balance (without charging)
		baseDenom := p.evmKeeper.GetBaseDenom(ctx)
		seiBalance := p.evmKeeper.BankKeeper().GetBalance(ctx, seiAddr, baseDenom).Amount
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
	}

	return next(ctx, tx, simulate)
}

func (p *EVMPreprocessDecorator) associateAddresses(ctx sdk.Context, seiAddr sdk.AccAddress, evmAddr common.Address, pubkey cryptotypes.PubKey) error {
	p.evmKeeper.SetAddressMapping(ctx, seiAddr, evmAddr)
	if !p.accountKeeper.HasAccount(ctx, seiAddr) {
		p.accountKeeper.SetAccount(ctx, p.accountKeeper.NewAccountWithAddress(ctx, seiAddr))
	}
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
	p.evmKeeper.AccountKeeper().RemoveAccount(ctx, authtypes.NewBaseAccountWithAddress(castAddr))
	return nil
}

// stateless
func Preprocess(ctx sdk.Context, msgEVMTransaction *evmtypes.MsgEVMTransaction, params evmtypes.Params) (sdk.Context, error) {
	txData, err := evmtypes.UnpackTxData(msgEVMTransaction.Data)
	if err != nil {
		return ctx, err
	}
	ctx = evmtypes.SetContextTxData(ctx, txData)
	// use infinite gas meter for EVM transaction because EVM handles gas checking from within
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeter())

	chainID := params.ChainId
	chainCfg := params.GetChainConfig()
	ethCfg := chainCfg.EthereumConfig(chainID.BigInt())
	ctx = evmtypes.SetContextEtCfg(ctx, ethCfg)
	version := GetVersion(ctx, ethCfg)
	signer := SignerMap[version](ethCfg.ChainID)
	if atx, ok := txData.(*ethtx.AssociateTx); ok {
		V, R, S := atx.GetRawSignatureValues()
		V = new(big.Int).Add(V, big.NewInt(27))
		evmAddr, seiAddr, pubkey, err := getAddresses(V, R, S, common.Hash{}) // associate tx should sign over an empty hash
		if err != nil {
			return ctx, err
		}
		ctx = evmtypes.SetContextEVMAddress(ctx, evmAddr)
		ctx = evmtypes.SetContextSeiAddress(ctx, seiAddr)
		ctx = evmtypes.SetContextPubkey(ctx, pubkey)
		ctx = evmtypes.SetContextIsAssociatedTx(ctx)
		return ctx, nil
	}
	ethTx := ethtypes.NewTx(txData.AsEthereumData())
	ctx = evmtypes.SetContextEthTx(ctx, ethTx)
	if !isTxTypeAllowed(version, ethTx.Type()) {
		return ctx, ethtypes.ErrInvalidChainId
	}
	ctx = evmtypes.SetContextEVMVersion(ctx, version)

	V, R, S := ethTx.RawSignatureValues()
	V = adjustV(V, ethTx.Type(), ethCfg.ChainID)
	evmAddr, seiAddr, seiPubkey, err := getAddresses(V, R, S, signer.Hash(ethTx))
	if err != nil {
		return ctx, err
	}
	ctx = evmtypes.SetContextEVMAddress(ctx, evmAddr)
	ctx = evmtypes.SetContextSeiAddress(ctx, seiAddr)
	ctx = evmtypes.SetContextPubkey(ctx, seiPubkey)
	return ctx, nil
}

func (p EVMPreprocessDecorator) AnteDeps(txDeps []sdkacltypes.AccessOperation, tx sdk.Tx, txIndex int, next sdk.AnteDepGenerator) (newTxDeps []sdkacltypes.AccessOperation, err error) {
	// TODO: define granular dependencies
	// Challenge is mainly the fact that at the time this function is evaluated, we haven't derived
	// the `from` key from signatures yet.
	return next(append(txDeps, sdkacltypes.AccessOperation{
		AccessType:         sdkacltypes.AccessType_READ,
		ResourceType:       sdkacltypes.ResourceType_KV_EVM,
		IdentifierTemplate: "*",
	}, sdkacltypes.AccessOperation{
		AccessType:         sdkacltypes.AccessType_WRITE,
		ResourceType:       sdkacltypes.ResourceType_KV_EVM,
		IdentifierTemplate: "*",
	}, sdkacltypes.AccessOperation{
		AccessType:         sdkacltypes.AccessType_READ,
		ResourceType:       sdkacltypes.ResourceType_KV_BANK,
		IdentifierTemplate: "*",
	}, sdkacltypes.AccessOperation{
		AccessType:         sdkacltypes.AccessType_WRITE,
		ResourceType:       sdkacltypes.ResourceType_KV_BANK,
		IdentifierTemplate: "*",
	}, sdkacltypes.AccessOperation{
		AccessType:         sdkacltypes.AccessType_READ,
		ResourceType:       sdkacltypes.ResourceType_KV_AUTH,
		IdentifierTemplate: "*",
	}, sdkacltypes.AccessOperation{
		AccessType:         sdkacltypes.AccessType_WRITE,
		ResourceType:       sdkacltypes.ResourceType_KV_AUTH,
		IdentifierTemplate: "*",
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

func isTxTypeAllowed(version evmtypes.SignerVersion, txType uint8) bool {
	for _, t := range AllowedTxTypes[version] {
		if t == txType {
			return true
		}
	}
	return false
}

func adjustV(V *big.Int, txType uint8, chainID *big.Int) *big.Int {
	// Non-legacy TX always needs to be bumped by 27
	if txType != ethtypes.LegacyTxType {
		return new(big.Int).Add(V, big.NewInt(27))
	}

	// legacy TX needs to be adjusted based on chainID
	V = new(big.Int).Sub(V, new(big.Int).Mul(chainID, big.NewInt(2)))
	return V.Sub(V, big.NewInt(8))
}

func GetVersion(ctx sdk.Context, ethCfg *params.ChainConfig) evmtypes.SignerVersion {
	blockNum := big.NewInt(ctx.BlockHeight())
	ts := uint64(ctx.BlockTime().Unix())
	switch {
	case ethCfg.IsCancun(blockNum, ts):
		return evmtypes.Cancun
	default:
		return evmtypes.London
	}
}
