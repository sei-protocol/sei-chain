package evmrpc

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSeiBlockAPIMethodSet(t *testing.T) {
	apiType := reflect.TypeOf(NewSeiBlockAPI(nil, nil, nil, nil, ConnectionTypeHTTP, nil, nil, nil))

	for _, method := range []string{
		"GetBlockByNumber",
		"GetBlockByNumberExcludeTraceFail",
		"GetBlockReceipts",
	} {
		_, exposed := apiType.MethodByName(method)
		require.False(t, exposed, method)
	}

	for _, method := range []string{
		"GetBlockByHash",
		"GetBlockByHashExcludeTraceFail",
		"GetBlockTransactionCountByHash",
		"GetBlockTransactionCountByNumber",
	} {
		_, exposed := apiType.MethodByName(method)
		require.True(t, exposed, method)
	}
}
