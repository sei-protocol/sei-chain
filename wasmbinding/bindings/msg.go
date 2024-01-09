package bindings

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

// / CreateDenom creates a new factory denom, of denomination:
// / factory/{creating contract address}/{Subdenom}
// / Subdenom can be of length at most 44 characters, in [0-9a-zA-Z./]
// / The (creating contract address, subdenom) pair must be unique.
// / The created denom's admin is the creating contract address,
// / but this admin can be changed using the ChangeAdmin binding.
type CreateDenom struct {
	Subdenom string `json:"subdenom"`
}

// / ChangeAdmin changes the admin for a factory denom.
// / If the NewAdminAddress is empty, the denom has no admin.
type ChangeAdmin struct {
	Denom           string `json:"denom"`
	NewAdminAddress string `json:"new_admin_address"`
}

type SetMetadata struct {
	Metadata banktypes.Metadata `json:"metadata"`
}

type MintTokens struct {
	Amount sdk.Coin `json:"amount"`
}

type BurnTokens struct {
	Amount sdk.Coin `json:"amount"`
}

// Dex Module msgs
type PlaceOrders struct {
	Orders       []*types.Order `json:"orders"`
	Funds        sdk.Coins      `json:"funds"`
	ContractAddr string         `json:"contract_address"`
}

type CancelOrders struct {
	Cancellations []*types.Cancellation `json:"cancellations"`
	ContractAddr  string                `json:"contract_address"`
}

type CallEVM struct {
	Value *sdk.Int `json:"value"`
	To    string   `json:"to"`
	Data  string   `json:"data"` // base64
}
