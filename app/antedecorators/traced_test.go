package antedecorators_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/app/antedecorators"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/utils"
	"github.com/stretchr/testify/require"
)

func TestTracedDecorator(t *testing.T) {
	output = ""
	anteDecorators := []sdk.AnteDecorator{
		FakeAnteDecoratorOne{},
		FakeAnteDecoratorTwo{},
		FakeAnteDecoratorThree{},
	}
	tracedDecorators := utils.Map(anteDecorators, func(d sdk.AnteDecorator) sdk.AnteDecorator {
		return antedecorators.NewTracedAnteDecorator(d, nil)
	})
	chainedHandler := sdk.ChainAnteDecorators(tracedDecorators...)
	chainedHandler(sdk.NewContext(nil, tmproto.Header{}, false, nil), FakeTx{}, false)
	require.Equal(t, "onetwothree", output)
}
