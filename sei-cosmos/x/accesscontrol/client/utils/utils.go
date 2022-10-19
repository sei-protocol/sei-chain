package utils

import (
	"io/ioutil"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
)

func ParseMsgUpdateResourceDependencyMappingProposalFile(cdc codec.JSONCodec, proposalFile string) (types.MsgUpdateResourceDependencyMappingProposalJsonFile, error) {
	proposal := types.MsgUpdateResourceDependencyMappingProposalJsonFile{}

	contents, err := ioutil.ReadFile(proposalFile)
	if err != nil {
		return proposal, err
	}

	cdc.MustUnmarshalJSON(contents, &proposal)

	return proposal, nil
}

func ParseUpdateWasmDependencyMappingProposalJSON(cdc codec.JSONCodec, proposalFile string) (types.MsgUpdateWasmDependencyMappingProposalJsonFile, error) {
	proposal := types.MsgUpdateWasmDependencyMappingProposalJsonFile{}

	contents, err := ioutil.ReadFile(proposalFile)
	if err != nil {
		return proposal, err
	}

	cdc.MustUnmarshalJSON(contents, &proposal)

	return proposal, nil
}
