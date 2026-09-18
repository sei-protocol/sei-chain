package migrations

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

// RetiredCWPointerBoundary is the body of the consensus boundaries that uploaded and
// migrated the CosmWasm wrapper code for ERC20/721/1155 tokens. The wrappers can no
// longer be created, so the boundaries store nothing. They stay registered because a
// module's migration sequence must be unbroken for a chain that upgrades across it.
func RetiredCWPointerBoundary(sdk.Context) error {
	return nil
}
