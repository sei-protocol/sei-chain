package keeper

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/apps/transfer/types"
)

func TestDeprecatedQueries(t *testing.T) {
	var queryServer types.QueryServer = Keeper{}
	server := reflect.ValueOf(queryServer)
	queryServerType := reflect.TypeOf((*types.QueryServer)(nil)).Elem()

	for i := 0; i < queryServerType.NumMethod(); i++ {
		method := queryServerType.Method(i)
		t.Run(method.Name, func(t *testing.T) {
			results := server.MethodByName(method.Name).Call([]reflect.Value{
				reflect.ValueOf(context.Background()),
				reflect.Zero(method.Type.In(1)),
			})

			require.Nil(t, results[0].Interface())
			require.ErrorIs(t, results[1].Interface().(error), types.ErrTransferDeprecated)
		})
	}
}
