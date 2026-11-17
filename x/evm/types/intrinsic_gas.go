package types

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// IntrinsicGas returns the intrinsic gas of an EVM transaction under rules. An unsigned
// transaction is priced with a zero sender. It fails when the transaction's value does not fit in 256
// bits or the sender of a signed transaction cannot be recovered.
func IntrinsicGas(etx *ethtypes.Transaction, rules params.Rules) (uint64, error) {
	value, overflow := uint256.FromBig(etx.Value())
	if overflow {
		return 0, fmt.Errorf("value exceeds 256 bits: %d bits", etx.Value().BitLen())
	}
	from, err := txSender(etx)
	if err != nil {
		return 0, fmt.Errorf("recover sender: %w", err)
	}
	return core.IntrinsicGas(etx.Data(), etx.AccessList(), etx.SetCodeAuthorizations(), from, etx.To(), value, rules)
}

// txSender returns etx's sender, or the zero address when etx carries no signature.
func txSender(etx *ethtypes.Transaction) (common.Address, error) {
	if _, r, s := etx.RawSignatureValues(); r.Sign() == 0 && s.Sign() == 0 {
		return common.Address{}, nil
	}
	return ethtypes.Sender(txSigner(etx), etx)
}

// txSigner returns a signer for etx's own chain ID, or the Homestead signer when that chain ID is not
// positive.
func txSigner(etx *ethtypes.Transaction) ethtypes.Signer {
	// go-ethereum's chain-ID signers panic on a chain ID that is not positive.
	if chainID := etx.ChainId(); chainID.Sign() > 0 {
		return ethtypes.LatestSignerForChainID(chainID)
	}
	return ethtypes.HomesteadSigner{}
}
