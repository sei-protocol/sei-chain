package evmrpc_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetSeiAddress(t *testing.T) {
	body := sendRequestGoodWithNamespace(t, "sei", "getSeiAddress", "0x1df809C639027b465B931BD63Ce71c8E5834D9d6")
	require.Equal(t, "sei1mf0llhmqane5w2y8uynmghmk2w4mh0xll9seym", body["result"])
}

func TestGetEvmAddress(t *testing.T) {
	body := sendRequestGoodWithNamespace(t, "sei", "getEVMAddress", "sei1mf0llhmqane5w2y8uynmghmk2w4mh0xll9seym")
	require.Equal(t, "0x1df809C639027b465B931BD63Ce71c8E5834D9d6", body["result"])
}

func TestGetCosmosTx(t *testing.T) {
	body := sendRequestGoodWithNamespace(t, "sei", "getCosmosTx", "0xa16d8f7ea8741acd23f15fc19b0dd26512aff68c01c6260d7c3a51b297399d32")
	fmt.Println(body)
	require.Equal(t, "91C86E5C7C41EA955834E8485EF14C8876CB5D0AB0447E7D7A1A5555B3421FCE", body["result"])
}
