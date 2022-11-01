package antedecorators_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/app/antedecorators"
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
	chainedHandler(sdk.Context{}, FakeTx{}, false)
	require.Equal(t, "onetwothree", output)
}
