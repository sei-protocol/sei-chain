package types_test

import (
	"testing"

	"github.com/gogo/protobuf/proto"
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

type retiredProposal interface {
	proto.Message
	govtypes.Content
	ValidateProposalSubmission() error
}

func retiredProposals() map[string]retiredProposal {
	return map[string]retiredProposal{
		types.ProposalTypeAddERCNativePointer: &types.AddERCNativePointerProposal{
			Title: "title", Description: "desc", Token: "test",
		},
		types.ProposalTypeAddERCCW20Pointer: &types.AddERCCW20PointerProposal{
			Title: "title", Description: "desc", Pointee: "test",
		},
		types.ProposalTypeAddERCCW721Pointer: &types.AddERCCW721PointerProposal{
			Title: "title", Description: "desc", Pointee: "test",
		},
		types.ProposalTypeAddERCCW1155Pointer: &types.AddERCCW1155PointerProposal{
			Title: "title", Description: "desc", Pointee: "test",
		},
		types.ProposalTypeAddCWERC20Pointer: &types.AddCWERC20PointerProposal{
			Title: "title", Description: "desc", Pointee: "test",
		},
		types.ProposalTypeAddCWERC721Pointer: &types.AddCWERC721PointerProposal{
			Title: "title", Description: "desc", Pointee: "test",
		},
		types.ProposalTypeAddCWERC1155Pointer: &types.AddCWERC1155PointerProposal{
			Title: "title", Description: "desc", Pointee: "test",
		},
		types.ProposalTypeAddERCNativePointerV2: &types.AddERCNativePointerProposalV2{
			Title: "title", Description: "desc", Token: "test", Name: "TEST", Symbol: "Test", Decimals: 6,
		},
	}
}

func TestPointerProposalsAreRetired(t *testing.T) {
	for proposalType, content := range retiredProposals() {
		t.Run(proposalType, func(t *testing.T) {
			require.Equal(t, "title", content.GetTitle())
			require.Equal(t, "desc", content.GetDescription())
			require.Equal(t, "evm", content.ProposalRoute())
			require.Equal(t, proposalType, content.ProposalType())
			require.NotEmpty(t, content.String())
			require.ErrorIs(t, content.ValidateBasic(), types.ErrPointerProposalDeprecated)
			require.ErrorIs(t, content.ValidateProposalSubmission(), types.ErrPointerProposalDeprecated)
		})
	}
}

// TestPointerProposalsStillDecode pins the reason the retired types stay registered:
// gov keeps a proposal in state once its vote has ended, and reading one back unpacks
// its content through the registry.
func TestPointerProposalsStillDecode(t *testing.T) {
	registry := codectypes.NewInterfaceRegistry()
	govtypes.RegisterInterfaces(registry)
	types.RegisterInterfaces(registry)

	for proposalType, content := range retiredProposals() {
		t.Run(proposalType, func(t *testing.T) {
			packed, err := codectypes.NewAnyWithValue(content)
			require.NoError(t, err)

			var decoded govtypes.Content
			require.NoError(t, registry.UnpackAny(packed, &decoded))
			require.Equal(t, proposalType, decoded.ProposalType())
		})
	}
}
