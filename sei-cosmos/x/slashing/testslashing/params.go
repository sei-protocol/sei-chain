package testslashing

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/slashing/types"
	abcitypes "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/bytes"
)

// TestParams construct default slashing params for tests.
// Have to change these parameters for tests
// lest the tests take forever
func TestParams() types.Params {
	params := types.DefaultParams()
	params.SignedBlocksWindow = 1000
	params.DowntimeJailDuration = 60 * 60
	params.MinSignedPerWindow = sdk.NewDecWithPrec(5, 1)

	return params
}

func CreateBeginBlockReq(valAddr bytes.HexBytes, power int64, signed bool) abcitypes.RequestBeginBlock {
	return abcitypes.RequestBeginBlock{
		LastCommitInfo: abcitypes.LastCommitInfo{
			Votes: []abcitypes.VoteInfo{
				{
					Validator: abcitypes.Validator{
						Address: valAddr.Bytes(),
						Power:   power,
					},
					SignedLastBlock: signed,
				},
			},
		},
	}
}
