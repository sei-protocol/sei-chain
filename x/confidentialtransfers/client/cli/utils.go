package cli

import (
	"context"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/types"
)

// GetAccount Uses the query client to get the account data for the given address and denomination.
func GetAccount(queryClient types.QueryClient, address, denom string) (*types.Account, error) {
	ctAccount, err := queryClient.GetCtAccount(context.Background(), &types.GetCtAccountRequest{
		Address: address,
		Denom:   denom,
	})
	if err != nil {
		return nil, err
	}

	account, err := ctAccount.GetAccount().FromProto()
	if err != nil {
		return nil, err
	}

	return account, nil
}
