package keeper

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	clienttypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/02-client/types"
	connectiontypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/03-connection/types"
	channeltypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/04-channel/types"
	coretypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/types"
)

func TestDeprecatedMessages(t *testing.T) {
	server := reflect.ValueOf(Keeper{})
	serverTypes := []reflect.Type{
		reflect.TypeOf((*clienttypes.MsgServer)(nil)).Elem(),
		reflect.TypeOf((*connectiontypes.MsgServer)(nil)).Elem(),
		reflect.TypeOf((*channeltypes.MsgServer)(nil)).Elem(),
	}

	for _, serverType := range serverTypes {
		for i := 0; i < serverType.NumMethod(); i++ {
			method := serverType.Method(i)
			t.Run(method.Name, func(t *testing.T) {
				results := server.MethodByName(method.Name).Call([]reflect.Value{
					reflect.ValueOf(context.Background()),
					reflect.Zero(method.Type.In(1)),
				})

				require.Nil(t, results[0].Interface())
				require.ErrorIs(t, results[1].Interface().(error), coretypes.ErrIBCDeprecated)
			})
		}
	}
}
