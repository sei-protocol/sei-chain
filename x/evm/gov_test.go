package evm_test

import (
	"testing"

	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

// TestProposalHandlerRefusesPointerProposals covers execution rather than submission:
// a proposal deposited before the retirement still reaches the handler when its
// voting period ends.
func TestProposalHandlerRefusesPointerProposals(t *testing.T) {
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)

	for _, content := range []govtypes.Content{
		&types.AddERCNativePointerProposal{Token: "test"},
		&types.AddERCCW20PointerProposal{Pointee: "test"},
		&types.AddERCCW721PointerProposal{Pointee: "test"},
		&types.AddERCCW1155PointerProposal{Pointee: "test"},
		&types.AddCWERC20PointerProposal{Pointee: "test"},
		&types.AddCWERC721PointerProposal{Pointee: "test"},
		&types.AddCWERC1155PointerProposal{Pointee: "test"},
		&types.AddERCNativePointerProposalV2{Token: "test", Name: "NAME", Symbol: "SYMBOL", Decimals: 6},
	} {
		t.Run(content.ProposalType(), func(t *testing.T) {
			require.ErrorIs(t, evm.ProposalHandler(ctx, content), types.ErrPointerProposalDeprecated)
		})
	}

	_, _, exists := testkeeper.EVMTestApp.EvmKeeper.GetERC20NativePointer(ctx, "test")
	require.False(t, exists)
}
