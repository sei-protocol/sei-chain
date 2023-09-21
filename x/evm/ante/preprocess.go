package ante

import (
	"errors"
	"math/big"

	"github.com/btcsuite/btcd/btcec"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	accountkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

type Version int

const (
	Frontier Version = iota
	Homestead
	EIP155
	Berlin
	London
	Cancun
)

var SignerMap = map[Version]func(*big.Int) ethtypes.Signer{
	Frontier:  func(_ *big.Int) ethtypes.Signer { return ethtypes.FrontierSigner{} },
	Homestead: func(_ *big.Int) ethtypes.Signer { return ethtypes.HomesteadSigner{} },
	EIP155:    func(i *big.Int) ethtypes.Signer { return ethtypes.NewEIP155Signer(i) },
	Berlin:    ethtypes.NewEIP2930Signer,
	London:    ethtypes.NewLondonSigner,
	Cancun:    ethtypes.NewCancunSigner,
}
var AllowedTxTypes = map[Version][]uint8{
	Frontier:  {ethtypes.LegacyTxType},
	Homestead: {ethtypes.LegacyTxType},
	EIP155:    {ethtypes.LegacyTxType},
	Berlin:    {ethtypes.LegacyTxType, ethtypes.AccessListTxType},
	London:    {ethtypes.LegacyTxType, ethtypes.AccessListTxType, ethtypes.DynamicFeeTxType},
	Cancun:    {ethtypes.LegacyTxType, ethtypes.AccessListTxType, ethtypes.DynamicFeeTxType, ethtypes.BlobTxType},
}

type EVMPreprocessDecorator struct {
	evmKeeper     *evmkeeper.Keeper
	accountKeeper *accountkeeper.AccountKeeper
}

func NewEVMPreprocessDecorator(evmKeeper *evmkeeper.Keeper, accountKeeper *accountkeeper.AccountKeeper) *EVMPreprocessDecorator {
	return &EVMPreprocessDecorator{evmKeeper: evmKeeper, accountKeeper: accountKeeper}
}

func (p EVMPreprocessDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if len(tx.GetMsgs()) == 0 {
		// this would never happen if this handler call is routed by the router
		return ctx, errors.New("no message exists in EVM tx")
	}
	ethTx, txData := tx.GetMsgs()[0].(*evmtypes.MsgEVMTransaction).AsTransaction()
	ctx = evmtypes.SetContextEthTx(ctx, ethTx)
	ctx = evmtypes.SetContextTxData(ctx, txData)

	V, R, S := ethTx.RawSignatureValues()
	chainID := p.evmKeeper.ChainID()
	evmParams := p.evmKeeper.GetParams(ctx)
	chainCfg := evmParams.GetChainConfig()
	ethCfg := chainCfg.EthereumConfig(chainID)
	ctx = evmtypes.SetContextEtCfg(ctx, ethCfg)
	version := getVersion(ctx, ethCfg)
	if !isTxTypeAllowed(version, ethTx.Type()) {
		return ctx, ethtypes.ErrInvalidChainId
	}
	V = adjustV(V, version, ethTx.Type(), ethCfg.ChainID)
	signer := SignerMap[version](ethCfg.ChainID)
	pubkey, err := recoverPubkey(signer.Hash(ethTx), R, S, V, true)
	if err != nil {
		return ctx, err
	}
	evmAddr, err := pubkeyToEVMAddress(pubkey)
	if err != nil {
		return ctx, err
	}
	seiPubkey := pubkeyBytesToSeiPubKey(pubkey)
	seiAddr := sdk.AccAddress(seiPubkey.Address())
	ctx = evmtypes.SetContextEVMAddress(ctx, evmAddr)
	ctx = evmtypes.SetContextSeiAddress(ctx, seiAddr)

	if _, found := p.evmKeeper.GetEVMAddress(ctx, seiAddr); !found {
		p.evmKeeper.SetAddressMapping(ctx, seiAddr, evmAddr)
	}

	if !p.accountKeeper.HasAccount(ctx, seiAddr) {
		p.accountKeeper.SetAccount(ctx, p.accountKeeper.NewAccountWithAddress(ctx, seiAddr))
	}
	// set pubkey in acc object if not exist. Not doing it in the above block in case an account is created
	// as a recipient of a send
	if acc := p.accountKeeper.GetAccount(ctx, seiAddr); acc.GetPubKey() == nil {
		acc.SetPubKey(&seiPubkey)
		p.accountKeeper.SetAccount(ctx, acc)
	}

	if balance := p.evmKeeper.GetBalance(ctx, evmAddr); balance > 0 {
		if err := p.evmKeeper.EVMToBankSend(ctx, evmAddr, seiAddr, balance); err != nil {
			return ctx, err
		}
	}
	return ctx, nil
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

func isTxTypeAllowed(version Version, txType uint8) bool {
	for _, t := range AllowedTxTypes[version] {
		if t == txType {
			return true
		}
	}
	return false
}

func adjustV(V *big.Int, version Version, txType uint8, chainID *big.Int) *big.Int {
	// no need to adjust for Frontier or Homestead
	if version == Frontier || version == Homestead {
		return V
	}

	// Non-legacy TX always needs to be bumped by 27
	if txType != ethtypes.LegacyTxType {
		return new(big.Int).Add(V, big.NewInt(27))
	}

	// legacy TX needs to be adjusted based on chainID
	V = new(big.Int).Sub(V, new(big.Int).Mul(chainID, big.NewInt(2)))
	return V.Sub(V, big.NewInt(8))
}

func getVersion(ctx sdk.Context, ethCfg *params.ChainConfig) Version {
	blockNum := big.NewInt(ctx.BlockHeight())
	ts := uint64(ctx.BlockTime().Unix())
	switch {
	case ethCfg.IsCancun(blockNum, ts):
		return Cancun
	case ethCfg.IsLondon(blockNum):
		return London
	case ethCfg.IsBerlin(blockNum):
		return Berlin
	case ethCfg.IsEIP155(blockNum):
		return EIP155
	case ethCfg.IsHomestead(blockNum):
		return Homestead
	default:
		return Frontier
	}
}
