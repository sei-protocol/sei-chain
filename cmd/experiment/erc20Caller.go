package main

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"os"

	"github.com/sei-protocol/sei-chain/loadtest/contracts/evm/bindings/erc20"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
)

func main() {
	// setup
	k, ctx := testkeeper.MockEVMKeeper()
	sei1, eth1 := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, sei1, eth1)
	fmt.Println("done")

	// deploy the contract to the keeper
	code, err := os.ReadFile("example/contracts/erc20/ERC20.bin")
	if err != nil {
		panic(err)
	}
	bz, err := hex.DecodeString(string(code))
	if err != nil {
		panic(err)
	}
	k.SetCode(ctx, eth1, bz)

	// do a static call to the contract
	abi, err := erc20.Erc20MetaData.GetAbi()
	if err != nil {
		panic(err)
	}
	bz, err = abi.Pack("totalSupply")
	if err != nil {
		panic(err)
	}
	res, err := k.StaticCallEVM(ctx, sei1, &eth1, bz)
	if err != nil {
		panic(err)
	}
	unpacked, err := abi.Unpack("totalSupply", res)
	if err != nil {
		panic(err)
	}
	fmt.Println("totalSupply = ", unpacked[0].(*big.Int))
}
