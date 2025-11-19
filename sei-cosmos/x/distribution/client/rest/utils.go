package rest

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/rest"
	seitypes "github.com/sei-protocol/sei-chain/types"
)

type (
	// CommunityPoolSpendProposalReq defines a community pool spend proposal request body.
	CommunityPoolSpendProposalReq struct {
		BaseReq rest.BaseReq `json:"base_req" yaml:"base_req"`

		Title       string              `json:"title" yaml:"title"`
		Description string              `json:"description" yaml:"description"`
		Recipient   seitypes.AccAddress `json:"recipient" yaml:"recipient"`
		Amount      sdk.Coins           `json:"amount" yaml:"amount"`
		Proposer    seitypes.AccAddress `json:"proposer" yaml:"proposer"`
		Deposit     sdk.Coins           `json:"deposit" yaml:"deposit"`
	}
)
