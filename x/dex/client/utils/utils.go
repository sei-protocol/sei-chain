package utils

import (
	"errors"
	"io/ioutil"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	dextypes "github.com/sei-protocol/sei-chain/x/dex/types"
)

type (
	PairJSON struct {
		PriceDenom string `json:"price_denom" yaml:"price_denom"`
		AssetDenom string `json:"asset_denom" yaml:"asset_denom"`
		TickSize   string `json:"tick_size" yaml:"tick_size"`
	}

	TickSizeJSON struct {
		Pair         PairJSON `json:"pair" yaml:"pair"`
		TickSize     sdk.Dec  `json:"tick_size" yaml:"tick_size"`
		ContractAddr string   `json:"contract_addr" yaml:"contract_addr"`
	}

	PairsJSON     []PairJSON
	TickSizesJSON []TickSizeJSON
	AssetListJSON []dextypes.AssetMetadata

	// ParamChangeJSON defines a parameter change used in JSON input. This
	// allows values to be specified in raw JSON instead of being string encoded.
	BatchContractPairJSON struct {
		ContractAddr string    `json:"contract_addr" yaml:"contract_addr"`
		Pairs        PairsJSON `json:"pairs" yaml:"pairs"`
	}

	MultipleBatchContractPairJSON []BatchContractPairJSON

	// RegisterPairsProposalJSON defines a RegisterPairsProposal
	// to parse register pair proposals from a JSON file.
	RegisterPairsProposalJSON struct {
		Title             string                        `json:"title" yaml:"title"`
		Description       string                        `json:"description" yaml:"description"`
		BatchContractPair MultipleBatchContractPairJSON `json:"batch_contract_pair" yaml:"batch_contract_pair"`
		Deposit           string                        `json:"deposit" yaml:"deposit"`
	}

	UpdateTickSizeProposalJSON struct {
		Title       string        `json:"title" yaml:"title"`
		Description string        `json:"description" yaml:"description"`
		TickSizes   TickSizesJSON `json:"tick_size_list" yaml:"tick_size_list"`
		Deposit     string        `json:"deposit" yaml:"deposit"`
	}

	AddAssetMetadataProposalJSON struct {
		Title       string        `json:"title" yaml:"title"`
		Description string        `json:"description" yaml:"description"`
		AssetList   AssetListJSON `json:"tick_size_list" yaml:"tick_size_list"`
		Deposit     string        `json:"deposit" yaml:"deposit"`
	}
)

// TODO: ADD utils to convert Each type to dex/type (string to denom)
func NewPair(pair PairJSON) (dextypes.Pair, error) {
	PriceDenom := pair.PriceDenom
	AssetDenom := pair.AssetDenom
	ticksize, err := sdk.NewDecFromStr(pair.TickSize)
	if err != nil {
		return dextypes.Pair{}, errors.New("ticksize: str to decimal conversion err")
	}
	return dextypes.Pair{PriceDenom: PriceDenom, AssetDenom: AssetDenom, Ticksize: &ticksize}, nil
}

// ToParamChange converts a ParamChangeJSON object to ParamChange.
func (bcp BatchContractPairJSON) ToBatchContractPair() (dextypes.BatchContractPair, error) {
	pairs := make([]*dextypes.Pair, len(bcp.Pairs))
	for i, p := range bcp.Pairs {
		newPair, err := NewPair(p)
		if err != nil {
			return dextypes.BatchContractPair{}, nil
		}
		pairs[i] = &newPair
	}
	return dextypes.BatchContractPair{ContractAddr: bcp.ContractAddr, Pairs: pairs}, nil
}

func (ts TickSizeJSON) ToTickSize() (dextypes.TickSize, error) {
	return dextypes.TickSize{
		Pair: &dextypes.Pair{
			PriceDenom: ts.Pair.PriceDenom,
			AssetDenom: ts.Pair.AssetDenom,
		},
		Ticksize:     ts.TickSize,
		ContractAddr: ts.ContractAddr,
	}, nil
}

// ToParamChanges converts a slice of ParamChangeJSON objects to a slice of
// ParamChange.
func (mbcp MultipleBatchContractPairJSON) ToMultipleBatchContractPair() ([]dextypes.BatchContractPair, error) {
	res := make([]dextypes.BatchContractPair, len(mbcp))
	for i, bcp := range mbcp {
		newBatch, err := bcp.ToBatchContractPair()
		if err != nil {
			return res, nil
		}
		res[i] = newBatch
	}
	return res, nil
}

func (tss TickSizesJSON) ToTickSizes() ([]dextypes.TickSize, error) {
	res := make([]dextypes.TickSize, len(tss))
	for i, ts := range tss {
		ticksize, err := ts.ToTickSize()
		if err != nil {
			return res, nil
		}
		res[i] = ticksize
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

// ParseRegisterPairsProposalJSON reads and parses a RegisterPairsProposalJSON from
// a file.
func ParseUpdateTickSizeProposalJSON(cdc *codec.LegacyAmino, proposalFile string) (UpdateTickSizeProposalJSON, error) {
	proposal := UpdateTickSizeProposalJSON{}

	contents, err := ioutil.ReadFile(proposalFile)
	if err != nil {
		return proposal, err
	}

	if err := cdc.UnmarshalJSON(contents, &proposal); err != nil {
		return proposal, err
	}

	return proposal, nil
}

// ParseAddAssetMetadataProposalJSON reads and parses an AddAssetMetadataProposalJSON from
// a file.
func ParseAddAssetMetadataProposalJSON(cdc *codec.LegacyAmino, proposalFile string) (AddAssetMetadataProposalJSON, error) {
	proposal := AddAssetMetadataProposalJSON{}

	contents, err := ioutil.ReadFile(proposalFile)
	if err != nil {
		return proposal, err
	}

	if err := cdc.UnmarshalJSON(contents, &proposal); err != nil {
		return proposal, err
	}

	// Verify base denoms specified in proposal are well formed
	// Additionally verify that the asset "display" field is included in denom unit
	for _, asset := range proposal.AssetList {
		err := sdk.ValidateDenom(asset.Metadata.Base)
		if err != nil {
			return AddAssetMetadataProposalJSON{}, err
		}

		// The display denom must have an associated DenomUnit field
		display := asset.Metadata.Display
		found := false
		for _, denomUnit := range asset.Metadata.DenomUnits {
			if denomUnit.Denom == display {
				found = true
				break
			}
		}

		if !found {
			return AddAssetMetadataProposalJSON{}, errors.New("Display denom " + display + " has no associated DenomUnit in Metadata.")
		}

	}

	return proposal, nil
}
