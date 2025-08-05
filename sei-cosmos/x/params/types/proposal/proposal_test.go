package proposal

import (
	"fmt"
	"github.com/tendermint/tendermint/types"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParameterChangeProposal(t *testing.T) {
	pc1 := NewParamChange("sub", "foo", "baz")
	pc2 := NewParamChange("sub", "bar", "cat")
	pcp := NewParameterChangeProposal("test title", "test description", []ParamChange{pc1, pc2}, true)

	require.Equal(t, "test title", pcp.GetTitle())
	require.Equal(t, "test description", pcp.GetDescription())
	require.Equal(t, RouterKey, pcp.ProposalRoute())
	require.Equal(t, ProposalTypeChange, pcp.ProposalType())
	require.Nil(t, pcp.ValidateBasic())

	pc3 := NewParamChange("", "bar", "cat")
	pcp = NewParameterChangeProposal("test title", "test description", []ParamChange{pc3}, true)
	require.Error(t, pcp.ValidateBasic())

	pc4 := NewParamChange("sub", "", "cat")
	pcp = NewParameterChangeProposal("test title", "test description", []ParamChange{pc4}, true)
	require.Error(t, pcp.ValidateBasic())
}

func TestConsensusParameterChangeProposal(t *testing.T) {
	// Valid block max_bytes (
	pc1 := NewParamChange("baseapp", "BlockParams", fmt.Sprintf("{\"max_bytes\":\"%d\"}", types.MaxBlockSizeBytes))
	pcp := NewParameterChangeProposal("test title", "test description", []ParamChange{pc1}, true)
	require.Nil(t, pcp.ValidateBasic())

	pc1 = NewParamChange("baseapp", "BlockParams", fmt.Sprintf("{\"max_bytes\":\"%d\"}", types.MaxBlockSizeBytes+1))
	pcp = NewParameterChangeProposal("test title", "test description", []ParamChange{pc1}, true)
	require.Error(t, pcp.ValidateBasic())
}
