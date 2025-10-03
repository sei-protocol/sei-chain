package feegrant_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"

	"github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/simapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/feegrant"
)

func TestFilteredFeeValidAllow(t *testing.T) {
	app := simapp.Setup(false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{
		Time: time.Now(),
	})

	eth := sdk.NewCoins(sdk.NewInt64Coin("eth", 10))
	atom := sdk.NewCoins(sdk.NewInt64Coin("atom", 555))
	smallAtom := sdk.NewCoins(sdk.NewInt64Coin("atom", 43))
	bigAtom := sdk.NewCoins(sdk.NewInt64Coin("atom", 1000))
	leftAtom := bigAtom.Sub(smallAtom)
	now := ctx.BlockTime()
	oneHour := now.Add(1 * time.Hour)
	from := sdk.MustAccAddressFromBech32("cosmos18cgkqduwuh253twzmhedesw3l7v3fm37sppt58")
	to := sdk.MustAccAddressFromBech32("cosmos1yq8lgssgxlx9smjhes6ryjasmqmd3ts2559g0t")

	// small fee without expire
	msgType := "/cosmos.bank.v1beta1.MsgSend"
	any, _ := types.NewAnyWithValue(&feegrant.BasicAllowance{
		SpendLimit: bigAtom,
	})

	// all fee without expire
	any2, _ := types.NewAnyWithValue(&feegrant.BasicAllowance{
		SpendLimit: smallAtom,
	})

	// wrong fee
	any3, _ := types.NewAnyWithValue(&feegrant.BasicAllowance{
		SpendLimit: bigAtom,
	})

	// wrong fee
	any4, _ := types.NewAnyWithValue(&feegrant.BasicAllowance{
		SpendLimit: bigAtom,
	})

	// expired
	any5, _ := types.NewAnyWithValue(&feegrant.BasicAllowance{
		SpendLimit: bigAtom,
		Expiration: &now,
	})

	// few more than allowed
	any6, _ := types.NewAnyWithValue(&feegrant.BasicAllowance{
		SpendLimit: atom,
		Expiration: &now,
	})

	// with out spend limit
	any7, _ := types.NewAnyWithValue(&feegrant.BasicAllowance{
		Expiration: &oneHour,
	})

	// expired no spend limit
	any8, _ := types.NewAnyWithValue(&feegrant.BasicAllowance{
		Expiration: &now,
	})

	// msg type not allowed
	msgType2 := "/cosmos.ibc.applications.transfer.v1.MsgTransfer"
	any9, _ := types.NewAnyWithValue(&feegrant.BasicAllowance{
		Expiration: &now,
	})

	cases := map[string]struct {
		allowance *feegrant.AllowedMsgAllowance
		msgs      []sdk.Msg
		fee       sdk.Coins
		blockTime time.Time
		valid     bool
		accept    bool
		remove    bool
		remains   sdk.Coins
	}{
		"small fee without expire": {
			allowance: &feegrant.AllowedMsgAllowance{
				Allowance:       any,
				AllowedMessages: []string{msgType},
			},
			msgs: []sdk.Msg{&banktypes.MsgSend{
				FromAddress: from.String(),
				ToAddress:   to.String(),
				Amount:      bigAtom,
			}},
			fee:     smallAtom,
			accept:  true,
			remove:  false,
			remains: leftAtom,
		},
		"all fee without expire": {
			allowance: &feegrant.AllowedMsgAllowance{
				Allowance:       any2,
				AllowedMessages: []string{msgType},
			},
			fee:    smallAtom,
			accept: true,
			remove: true,
		},
		"wrong fee": {
			allowance: &feegrant.AllowedMsgAllowance{
				Allowance:       any3,
				AllowedMessages: []string{msgType},
			},
			fee:    eth,
			accept: false,
		},
		"non-expired": {
			allowance: &feegrant.AllowedMsgAllowance{
				Allowance:       any4,
				AllowedMessages: []string{msgType},
			},
			valid:     true,
			fee:       smallAtom,
			blockTime: now,
			accept:    true,
			remove:    false,
			remains:   leftAtom,
		},
		"expired": {
			allowance: &feegrant.AllowedMsgAllowance{
				Allowance:       any5,
				AllowedMessages: []string{msgType},
			},
			valid:     true,
			fee:       smallAtom,
			blockTime: oneHour,
			accept:    false,
			remove:    true,
		},
		"fee more than allowed": {
			allowance: &feegrant.AllowedMsgAllowance{
				Allowance:       any6,
				AllowedMessages: []string{msgType},
			},
			valid:     true,
			fee:       bigAtom,
			blockTime: now,
			accept:    false,
		},
		"with out spend limit": {
			allowance: &feegrant.AllowedMsgAllowance{
				Allowance:       any7,
				AllowedMessages: []string{msgType},
			},
			valid:     true,
			fee:       bigAtom,
			blockTime: now,
			accept:    true,
		},
		"expired no spend limit": {
			allowance: &feegrant.AllowedMsgAllowance{
				Allowance:       any8,
				AllowedMessages: []string{msgType},
			},
			valid:     true,
			fee:       bigAtom,
			blockTime: oneHour,
			accept:    false,
		},
		"msg type not allowed": {
			allowance: &feegrant.AllowedMsgAllowance{
				Allowance:       any9,
				AllowedMessages: []string{msgType2},
			},
			msgs: []sdk.Msg{&banktypes.MsgSend{
				FromAddress: from.String(),
				ToAddress:   to.String(),
				Amount:      bigAtom,
			}},
			valid:  true,
			fee:    bigAtom,
			accept: false,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := tc.allowance.ValidateBasic()
			require.NoError(t, err)

			ctx := app.BaseApp.NewContext(false, tmproto.Header{}).WithBlockTime(tc.blockTime)

			removed, err := tc.allowance.Accept(ctx, tc.fee, tc.msgs)
			if !tc.accept {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			require.Equal(t, tc.remove, removed)
			if !removed {
				allowance, _ := tc.allowance.GetAllowance()
				assert.Equal(t, tc.remains, allowance.(*feegrant.BasicAllowance).SpendLimit)
			}
		})
	}
}

func TestFilteredFeeValidAllowance(t *testing.T) {
	app := simapp.Setup(false)

	smallAtom := sdk.NewCoins(sdk.NewInt64Coin("atom", 488))
	bigAtom := sdk.NewCoins(sdk.NewInt64Coin("atom", 1000))
	leftAtom := sdk.NewCoins(sdk.NewInt64Coin("atom", 512))

	basicAllowance, _ := types.NewAnyWithValue(&feegrant.BasicAllowance{
		SpendLimit: bigAtom,
	})

	cases := map[string]struct {
		allowance *feegrant.AllowedMsgAllowance
		// all other checks are ignored if valid=false
		fee       sdk.Coins
		blockTime time.Time
		valid     bool
		accept    bool
		remove    bool
		remains   sdk.Coins
	}{
		"internal fee is updated": {
			allowance: &feegrant.AllowedMsgAllowance{
				Allowance:       basicAllowance,
				AllowedMessages: []string{"/cosmos.bank.v1beta1.MsgSend"},
			},
			fee:     smallAtom,
			accept:  true,
			remove:  false,
			remains: leftAtom,
		},
	}

	for name, stc := range cases {
		tc := stc // to make scopelint happy
		t.Run(name, func(t *testing.T) {
			err := tc.allowance.ValidateBasic()
			require.NoError(t, err)

			ctx := app.BaseApp.NewContext(false, tmproto.Header{}).WithBlockTime(tc.blockTime)

			// now try to deduct
			removed, err := tc.allowance.Accept(ctx, tc.fee, []sdk.Msg{
				&banktypes.MsgSend{
					FromAddress: "gm",
					ToAddress:   "gn",
					Amount:      tc.fee,
				},
			})
			if !tc.accept {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			require.Equal(t, tc.remove, removed)
			if !removed {
				var basicAllowanceLeft feegrant.BasicAllowance
				app.AppCodec().Unmarshal(tc.allowance.Allowance.Value, &basicAllowanceLeft)

				assert.Equal(t, tc.remains, basicAllowanceLeft.SpendLimit)
			}
		})
	}
}
