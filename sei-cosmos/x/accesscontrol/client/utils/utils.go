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

func ParseRegisterWasmDependencyMappingJSON(cdc codec.JSONCodec, dependencyFile string) (types.RegisterWasmDependencyJSONFile, error) {
	wasmDependencyJson := types.RegisterWasmDependencyJSONFile{}

	contents, err := ioutil.ReadFile(dependencyFile)
	if err != nil {
		return wasmDependencyJson, err
	}

	cdc.MustUnmarshalJSON(contents, &wasmDependencyJson)

	return wasmDependencyJson, nil
}
