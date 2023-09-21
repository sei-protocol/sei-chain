package types

import (
	"errors"
	"fmt"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"

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
		HomesteadBlock:      getBlockValue(cc.HomesteadBlock),
		DAOForkBlock:        getBlockValue(cc.DAOForkBlock),
		DAOForkSupport:      cc.DAOForkSupport,
		EIP150Block:         getBlockValue(cc.EIP150Block),
		EIP155Block:         getBlockValue(cc.EIP155Block),
		EIP158Block:         getBlockValue(cc.EIP158Block),
		ByzantiumBlock:      getBlockValue(cc.ByzantiumBlock),
		ConstantinopleBlock: getBlockValue(cc.ConstantinopleBlock),
		PetersburgBlock:     getBlockValue(cc.PetersburgBlock),
		IstanbulBlock:       getBlockValue(cc.IstanbulBlock),
		MuirGlacierBlock:    getBlockValue(cc.MuirGlacierBlock),
		BerlinBlock:         getBlockValue(cc.BerlinBlock),
		LondonBlock:         getBlockValue(cc.LondonBlock),
		ArrowGlacierBlock:   getBlockValue(cc.ArrowGlacierBlock),
		GrayGlacierBlock:    getBlockValue(cc.GrayGlacierBlock),
		MergeNetsplitBlock:  getBlockValue(cc.MergeNetsplitBlock),
		ShanghaiTime:        &cc.ShanghaiTime,
		CancunTime:          &cc.CancunTime,
		PragueTime:          &cc.PragueTime,
		VerkleTime:          &cc.VerkleTime,
	}
}

func DefaultChainConfig() ChainConfig {
	homesteadBlock := sdk.ZeroInt()
	daoForkBlock := sdk.ZeroInt()
	eip150Block := sdk.ZeroInt()
	eip155Block := sdk.ZeroInt()
	eip158Block := sdk.ZeroInt()
	byzantiumBlock := sdk.ZeroInt()
	constantinopleBlock := sdk.ZeroInt()
	petersburgBlock := sdk.ZeroInt()
	istanbulBlock := sdk.ZeroInt()
	muirGlacierBlock := sdk.ZeroInt()
	berlinBlock := sdk.ZeroInt()
	londonBlock := sdk.ZeroInt()
	arrowGlacierBlock := sdk.ZeroInt()
	grayGlacierBlock := sdk.ZeroInt()
	mergeNetsplitBlock := sdk.ZeroInt()

	return ChainConfig{
		HomesteadBlock:      &homesteadBlock,
		DAOForkBlock:        &daoForkBlock,
		DAOForkSupport:      true,
		EIP150Block:         &eip150Block,
		EIP155Block:         &eip155Block,
		EIP158Block:         &eip158Block,
		ByzantiumBlock:      &byzantiumBlock,
		ConstantinopleBlock: &constantinopleBlock,
		PetersburgBlock:     &petersburgBlock,
		IstanbulBlock:       &istanbulBlock,
		MuirGlacierBlock:    &muirGlacierBlock,
		BerlinBlock:         &berlinBlock,
		LondonBlock:         &londonBlock,
		ArrowGlacierBlock:   &arrowGlacierBlock,
		GrayGlacierBlock:    &grayGlacierBlock,
		MergeNetsplitBlock:  &mergeNetsplitBlock,
		ShanghaiTime:        0,
		CancunTime:          0,
		PragueTime:          0,
		VerkleTime:          0,
	}
}

func getBlockValue(block *sdk.Int) *big.Int {
	if block == nil || block.IsNegative() {
		return nil
	}

	return block.BigInt()
}

func (cc ChainConfig) Validate() error {
	if err := validateBlock(cc.HomesteadBlock); err != nil {
		return errors.New("homesteadBlock")
	}
	if err := validateBlock(cc.DAOForkBlock); err != nil {
		return errors.New("daoForkBlock")
	}
	if err := validateBlock(cc.EIP150Block); err != nil {
		return errors.New("eip150Block")
	}
	if err := validateBlock(cc.EIP155Block); err != nil {
		return errors.New("eip155Block")
	}
	if err := validateBlock(cc.EIP158Block); err != nil {
		return errors.New("eip158Block")
	}
	if err := validateBlock(cc.ByzantiumBlock); err != nil {
		return errors.New("byzantiumBlock")
	}
	if err := validateBlock(cc.ConstantinopleBlock); err != nil {
		return errors.New("constantinopleBlock")
	}
	if err := validateBlock(cc.PetersburgBlock); err != nil {
		return errors.New("petersburgBlock")
	}
	if err := validateBlock(cc.IstanbulBlock); err != nil {
		return errors.New("istanbulBlock")
	}
	if err := validateBlock(cc.MuirGlacierBlock); err != nil {
		return errors.New("muirGlacierBlock")
	}
	if err := validateBlock(cc.BerlinBlock); err != nil {
		return errors.New("berlinBlock")
	}
	if err := validateBlock(cc.LondonBlock); err != nil {
		return errors.New("londonBlock")
	}
	if err := validateBlock(cc.ArrowGlacierBlock); err != nil {
		return errors.New("arrowGlacierBlock")
	}
	if err := validateBlock(cc.GrayGlacierBlock); err != nil {
		return errors.New("grayGlacierBlock")
	}
	if err := validateBlock(cc.MergeNetsplitBlock); err != nil {
		return errors.New("mergeNetsplitBlock")
	}
	if err := cc.EthereumConfig(nil).CheckConfigForkOrder(); err != nil {
		return errors.New("invalid config fork order")
	}
	return nil
}

func validateBlock(block *sdk.Int) error {
	if block == nil {
		return nil
	}

	if block.IsNegative() {
		return fmt.Errorf(
			"block value cannot be negative: %s", block,
		)
	}

	return nil
}
