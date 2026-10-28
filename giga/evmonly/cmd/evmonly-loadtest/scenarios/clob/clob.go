// Package clob holds the CLOBv1 order book the load test's clob-place workload drives, with the
// calldata and storage-slot helpers that workload and its tests need.
//
// CLOBv1.sol is the source of truth for the contract; sei-load copies it with `make sync-clob`. After
// editing the contract, regenerate CLOBv1.abi and CLOBv1.bin here with sei-load's pinned compiler:
//
//	docker run --rm --platform linux/amd64 -v "$PWD":/w -w /w ethereum/solc:0.8.19 \
//	  --abi --bin --optimize --evm-version paris --metadata-hash none --overwrite -o . CLOBv1.sol
package clob

import (
	_ "embed"
	"encoding/binary"
	"fmt"
	"math/big"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm/runtime"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
)

// balancesSlot is the storage slot of CLOBv1's balances mapping.
const balancesSlot = 2

//go:embed CLOBv1.bin
var creationHex string

//go:embed CLOBv1.abi
var abiJSON string

var (
	placeNextSelector = crypto.Keccak256([]byte("placeNext(uint256)"))[:4]
	loadRuntime       = sync.OnceValues(buildRuntime)
)

// Credit returns the base and quote CLOBv1 credits an account with on its first call in a market.
func Credit() *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(30), nil)
}

// RuntimeCode returns a copy of the deployed bytecode the CLOBv1 constructor produces.
func RuntimeCode() ([]byte, error) {
	code, err := loadRuntime()
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), code...), nil
}

// ABI returns CLOBv1's JSON ABI.
func ABI() string {
	return abiJSON
}

// PlaceNextCalldata encodes placeNext(market). Market 0 is the sender's own market.
func PlaceNextCalldata(market uint64) []byte {
	data := make([]byte, 4+32)
	copy(data, placeNextSelector)
	binary.BigEndian.PutUint64(data[4+24:], market)
	return data
}

// BalanceSlots returns the storage slots of owner's base and quote balances in market. Each slot
// packs the available amount in its low 128 bits and the locked amount in its high 128 bits.
func BalanceSlots(market uint64, owner common.Address) (base, quote common.Hash) {
	inner := crypto.Keccak256Hash(common.BigToHash(new(big.Int).SetUint64(market)).Bytes(),
		common.BigToHash(big.NewInt(balancesSlot)).Bytes())
	base = crypto.Keccak256Hash(common.BytesToHash(owner.Bytes()).Bytes(), inner.Bytes())
	return base, common.BigToHash(new(big.Int).Add(base.Big(), big.NewInt(1)))
}

// UnpackBalance splits a balance slot value into its available and locked amounts.
func UnpackBalance(value common.Hash) (available, locked *big.Int) {
	return new(big.Int).SetBytes(value[16:32]), new(big.Int).SetBytes(value[0:16])
}

// buildRuntime runs the constructor in an empty EVM. The probe market it seeds is left behind, as
// only eth_estimateGas reads it.
func buildRuntime() ([]byte, error) {
	code, _, _, err := runtime.Create(common.FromHex(strings.TrimSpace(creationHex)), &runtime.Config{
		ChainConfig: params.AllDevChainProtocolChanges,
		Origin:      common.HexToAddress("0x1"),
		BlockNumber: big.NewInt(1),
		Time:        1_700_000_000,
		GasLimit:    10_000_000,
		GasPrice:    new(big.Int),
		Value:       new(big.Int),
		BaseFee:     new(big.Int),
	})
	if err != nil {
		return nil, fmt.Errorf("run CLOBv1 constructor: %w", err)
	}
	if len(code) == 0 {
		return nil, fmt.Errorf("CLOBv1 constructor returned no runtime")
	}
	return code, nil
}
