package migrations

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

// RetiredPointerBoundary is the body of the consensus boundaries whose only work was
// creating pointers: uploading and migrating the CosmWasm wrapper code for ERC20/721/1155
// tokens, and redeploying the ERC20/721/1155 pointers for native denoms and CosmWasm
// contracts at a newer version. Neither can be created any more, so the boundaries store
// nothing. They stay registered because a module's migration sequence must be unbroken
// for a chain that upgrades across it.
func RetiredPointerBoundary(sdk.Context) error {
	return nil
}
