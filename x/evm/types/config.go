package types

import (
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/params"
	"github.com/sei-protocol/sei-chain/utils"
)

var CancunTime int64 = 0

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
		HomesteadBlock:      utils.Big0,
		DAOForkBlock:        utils.Big0,
		DAOForkSupport:      false, // fork of Sei is supported outside EVM
		EIP150Block:         utils.Big0,
		EIP155Block:         utils.Big0,
		EIP158Block:         utils.Big0,
		ByzantiumBlock:      utils.Big0,
		ConstantinopleBlock: utils.Big0,
		PetersburgBlock:     utils.Big0,
		IstanbulBlock:       utils.Big0,
		MuirGlacierBlock:    utils.Big0,
		BerlinBlock:         utils.Big0,
		LondonBlock:         utils.Big0,
		ArrowGlacierBlock:   utils.Big0,
		GrayGlacierBlock:    utils.Big0,
		MergeNetsplitBlock:  utils.Big0,
		ShanghaiTime:        getUpgradeTimestamp(0),
		CancunTime:          getUpgradeTimestamp(cc.CancunTime),
		PragueTime:          getUpgradeTimestamp(cc.PragueTime),
		VerkleTime:          getUpgradeTimestamp(cc.VerkleTime),
		BlobScheduleConfig:  params.DefaultBlobSchedule,
	}
}

func DefaultChainConfig() ChainConfig {
	return ChainConfig{
		CancunTime: CancunTime,
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
