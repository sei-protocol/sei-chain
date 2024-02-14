package types

import (
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/params"
)

/*
*
XXBlock/Time fields indicate upgrade heights/timestamps. For example, a BerlinBlock
of 123 means the chain upgraded to the Berlin version at height 123; a ShanghaiTime
of 42198537129 means the chain upgraded to the Shanghai version at timestamp 42198537129.
A value of 0 means the upgrade is included in the genesis of the EVM, which will be the
case on Sei for all versions up to Cancun. Still, we want to keep these fields in the
config for backward compatibility with the official EVM lib.
*/
func (cc ChainConfig) EthereumConfig(chainID *big.Int) *params.ChainConfig {
	return &params.ChainConfig{
		ChainID:             chainID,
		HomesteadBlock:      big.NewInt(0),
		DAOForkBlock:        big.NewInt(0),
		DAOForkSupport:      false, // fork of Sei is supported outside EVM
		EIP150Block:         big.NewInt(0),
		EIP155Block:         big.NewInt(0),
		EIP158Block:         big.NewInt(0),
		ByzantiumBlock:      big.NewInt(0),
		ConstantinopleBlock: big.NewInt(0),
		PetersburgBlock:     big.NewInt(0),
		IstanbulBlock:       big.NewInt(0),
		MuirGlacierBlock:    big.NewInt(0),
		BerlinBlock:         big.NewInt(0),
		LondonBlock:         big.NewInt(0),
		ArrowGlacierBlock:   big.NewInt(0),
		GrayGlacierBlock:    big.NewInt(0),
		MergeNetsplitBlock:  big.NewInt(0),
		ShanghaiTime:        getUpgradeTimestamp(0),
		CancunTime:          getUpgradeTimestamp(cc.CancunTime),
		PragueTime:          getUpgradeTimestamp(cc.PragueTime),
		VerkleTime:          getUpgradeTimestamp(cc.VerkleTime),
	}
}

func DefaultChainConfig() ChainConfig {
	return ChainConfig{
		CancunTime: 0,
		PragueTime: -1,
		VerkleTime: -1,
	}
}

func getUpgradeTimestamp(i int64) *uint64 {
	if i < 0 {
		return nil
	}
	res := uint64(i)
	return &res
}

func (cc ChainConfig) Validate() error {
	if err := cc.EthereumConfig(nil).CheckConfigForkOrder(); err != nil {
		return errors.New("invalid config fork order")
	}
	return nil
}
