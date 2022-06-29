package utils

import (
	"errors"
	"io/ioutil"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

type (
	PairJSON struct {
		PriceDenom string          `json:"price_denom" yaml:"price_denom"`
		AssetDenom string          `json:"asset_denom" yaml:"asset_denom"`
	}

	PairsJSON []PairJSON

	// ParamChangeJSON defines a parameter change used in JSON input. This
	// allows values to be specified in raw JSON instead of being string encoded.
	BatchContractPairJSON struct {
		ContractAddr string          `json:"contract_addr" yaml:"contract_addr"`
		Pairs PairsJSON          `json:"pairs" yaml:"pairs"`
	}

	MultipleBatchContractPairJSON []BatchContractPairJSON

	// RegisterPairsProposalJSON defines a RegisterPairsProposal
	// to parse register pair proposals from a JSON file.
	RegisterPairsProposalJSON struct {
		Title       string           `json:"title" yaml:"title"`
		Description string           `json:"description" yaml:"description"`
		BatchContractPair MultipleBatchContractPairJSON           `json:"batch_contract_pair" yaml:"batch_contract_pair"`
		Deposit     string           `json:"deposit" yaml:"deposit"`
	}
)

// TODO: ADD utils to convert Each type to dex/type (string to denom)
func NewPair(pair PairJSON) (types.Pair, error) {

	PriceDenom, unit, err := types.GetDenomFromStr(pair.PriceDenom)
	if err != nil {
		return types.Pair{}, err
	}
	if unit != types.Unit_STANDARD {
		return types.Pair{}, errors.New("Denom must be in standard/whole unit (e.g. sei instead of usei)")
	}
	AssetDenom, unit, err := types.GetDenomFromStr(pair.AssetDenom)
	if err != nil {
		return types.Pair{}, err
	}
	if unit != types.Unit_STANDARD {
		return types.Pair{}, errors.New("Denom must be in standard/whole unit (e.g. sei instead of usei)")
	}
	return types.Pair{PriceDenom, AssetDenom}, nil
}

// ToParamChange converts a ParamChangeJSON object to ParamChange.
func (bcp BatchContractPairJSON) ToBatchContractPair() (types.BatchContractPair, error) {
	pairs := make([]*types.Pair, len(bcp.Pairs))
	for i, p := range bcp.Pairs {
		new_pair, err := NewPair(p)
		if err != nil {
			return types.BatchContractPair{}, nil
		}
		pairs[i] = &new_pair
	}
	return types.BatchContractPair{bcp.ContractAddr, pairs}, nil
}

// ToParamChanges converts a slice of ParamChangeJSON objects to a slice of
// ParamChange.
func (mbcp MultipleBatchContractPairJSON) ToMultipleBatchContractPair() ([]types.BatchContractPair, error) {
	res := make([]types.BatchContractPair, len(mbcp))
	for i, bcp := range mbcp {
		new_batch, err := bcp.ToBatchContractPair()
		if err != nil {
			return res, nil
		}
		res[i] = new_batch
	}
	return res, nil
}

// ParseRegisterPairsProposalJSON reads and parses a RegisterPairsProposalJSON from
// a file.
func ParseRegisterPairsProposalJSON(cdc *codec.LegacyAmino, proposalFile string) (RegisterPairsProposalJSON, error) {
	proposal := RegisterPairsProposalJSON{}

	contents, err := ioutil.ReadFile(proposalFile)
	if err != nil {
		return proposal, err
	}

	if err := cdc.UnmarshalJSON(contents, &proposal); err != nil {
		return proposal, err
	}

	return proposal, nil
}
