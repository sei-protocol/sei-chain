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
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

type EVMPreprocessDecorator struct {
	evmKeeper     *evmkeeper.Keeper
	accountKeeper *accountkeeper.AccountKeeper
}

func NewEVMPreprocessDecorator(evmKeeper *evmkeeper.Keeper, accountKeeper *accountkeeper.AccountKeeper) *EVMPreprocessDecorator {
	return &EVMPreprocessDecorator{evmKeeper: evmKeeper, accountKeeper: accountKeeper}
}

func (p EVMPreprocessDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	ethTx, txData := tx.GetMsgs()[0].(*evmtypes.MsgEVMTransaction).AsTransaction()
	ctx = evmtypes.SetContextEthTx(ctx, ethTx)
	ctx = evmtypes.SetContextTxData(ctx, txData)

	V, R, S := ethTx.RawSignatureValues()
	chainID := p.evmKeeper.ChainID()
	evmParams := p.evmKeeper.GetParams(ctx)
	chainCfg := evmParams.GetChainConfig()
	ethCfg := chainCfg.EthereumConfig(chainID)
	ctx = evmtypes.SetContextEtCfg(ctx, ethCfg)
	blockNum := big.NewInt(ctx.BlockHeight())
	var signer ethtypes.Signer
	switch {
	case ethCfg.IsCancun(blockNum, uint64(ctx.BlockTime().Unix())):
		if ethTx.Type() == ethtypes.LegacyTxType {
			if ethTx.ChainId().Cmp(ethCfg.ChainID) != 0 {
				return ctx, ethtypes.ErrInvalidChainId
			}
			V = new(big.Int).Sub(V, new(big.Int).Mul(ethTx.ChainId(), big.NewInt(2)))
			V.Sub(V, big.NewInt(8))
		} else if ethTx.Type() == ethtypes.AccessListTxType {
			V = new(big.Int).Add(V, big.NewInt(27))
		} else {
			V = new(big.Int).Add(V, big.NewInt(27))
			if ethTx.ChainId().Cmp(ethCfg.ChainID) != 0 {
				return ctx, ethtypes.ErrInvalidChainId
			}
		}
		signer = ethtypes.NewCancunSigner(ethCfg.ChainID)
	case ethCfg.IsLondon(blockNum):
		if ethTx.Type() == ethtypes.LegacyTxType {
			if ethTx.ChainId().Cmp(ethCfg.ChainID) != 0 {
				return ctx, ethtypes.ErrInvalidChainId
			}
			V = new(big.Int).Sub(V, new(big.Int).Mul(ethTx.ChainId(), big.NewInt(2)))
			V.Sub(V, big.NewInt(8))
		} else if ethTx.Type() == ethtypes.AccessListTxType {
			V = new(big.Int).Add(V, big.NewInt(27))
		} else if ethTx.Type() == ethtypes.DynamicFeeTxType {
			V = new(big.Int).Add(V, big.NewInt(27))
			if ethTx.ChainId().Cmp(ethCfg.ChainID) != 0 {
				return ctx, ethtypes.ErrInvalidChainId
			}
		} else {
			return ctx, ethtypes.ErrInvalidTxType
		}
		signer = ethtypes.NewLondonSigner(ethCfg.ChainID)
	case ethCfg.IsBerlin(blockNum):
		if ethTx.Type() == ethtypes.LegacyTxType {
			if ethTx.ChainId().Cmp(ethCfg.ChainID) != 0 {
				return ctx, ethtypes.ErrInvalidChainId
			}
			V = new(big.Int).Sub(V, new(big.Int).Mul(ethTx.ChainId(), big.NewInt(2)))
			V.Sub(V, big.NewInt(8))
		} else if ethTx.Type() == ethtypes.AccessListTxType {
			V = new(big.Int).Add(V, big.NewInt(27))
		} else {
			return ctx, ethtypes.ErrInvalidTxType
		}
		signer = ethtypes.NewEIP2930Signer(ethCfg.ChainID)
	case ethCfg.IsEIP155(blockNum):
		if ethTx.Type() != ethtypes.LegacyTxType {
			return ctx, ethtypes.ErrTxTypeNotSupported
		}
		if ethTx.ChainId().Cmp(ethCfg.ChainID) != 0 {
			return ctx, ethtypes.ErrInvalidChainId
		}
		V = new(big.Int).Sub(V, new(big.Int).Mul(ethTx.ChainId(), big.NewInt(2)))
		V.Sub(V, big.NewInt(8))
		signer = ethtypes.NewEIP155Signer(ethCfg.ChainID)
	case ethCfg.IsHomestead(blockNum):
		if ethTx.Type() != ethtypes.LegacyTxType {
			return ctx, ethtypes.ErrTxTypeNotSupported
		}
		signer = ethtypes.HomesteadSigner{}
	default:
		if ethTx.Type() != ethtypes.LegacyTxType {
			return ctx, ethtypes.ErrTxTypeNotSupported
		}
		signer = ethtypes.FrontierSigner{}
	}
	pubkey, err := recoverPubkey(signer.Hash(ethTx), R, S, V, true)
	if err != nil {
		return ctx, err
	}
	evmAddr, err := pubkeyToEVMAddress(pubkey)
	if err != nil {
		return ctx, err
	}
	seiAddr := pubkeyToSeiAddress(pubkey)
	ctx = evmtypes.SetContextEVMAddress(ctx, evmAddr)
	ctx = evmtypes.SetContextSeiAddress(ctx, seiAddr)

	if _, found := p.evmKeeper.GetEVMAddress(ctx, seiAddr); !found {
		p.evmKeeper.SetAddressMapping(ctx, seiAddr, evmAddr)
	}

	if p.accountKeeper.HasAccount(ctx, seiAddr) {
		p.accountKeeper.SetAccount(ctx, p.accountKeeper.NewAccountWithAddress(ctx, seiAddr))
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

func pubkeyToSeiAddress(pub []byte) sdk.AccAddress {
	pubKey, _ := crypto.UnmarshalPubkey(pub)
	pubkeyObj := (*btcec.PublicKey)(pubKey)
	pk := secp256k1.PubKey{Key: pubkeyObj.SerializeCompressed()}
	return sdk.AccAddress(pk.Address())
}
