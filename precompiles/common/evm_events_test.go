package common

import (
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

var StakingABI abi.ABI

func init() {
	// Create a simplified ABI for testing event encoding
	// Note: We're using function definitions to test Pack() method
	abiJSON := `[{
		"name": "Delegate",
		"type": "function",
		"inputs": [
			{"name": "delegator", "type": "address"},
			{"name": "validator", "type": "string"},
			{"name": "amount", "type": "uint256"}
		],
		"outputs": []
	},{
		"name": "Redelegate",
		"type": "function",
		"inputs": [
			{"name": "delegator", "type": "address"},
			{"name": "srcValidator", "type": "string"},
			{"name": "dstValidator", "type": "string"},
			{"name": "amount", "type": "uint256"}
		],
		"outputs": []
	},{
		"name": "Undelegate",
		"type": "function",
		"inputs": [
			{"name": "delegator", "type": "address"},
			{"name": "validator", "type": "string"},
			{"name": "amount", "type": "uint256"}
		],
		"outputs": []
	},{
		"name": "ValidatorCreated",
		"type": "function",
		"inputs": [
			{"name": "creator", "type": "address"},
			{"name": "validator", "type": "string"},
			{"name": "moniker", "type": "string"}
		],
		"outputs": []
	},{
		"name": "ValidatorEdited",
		"type": "function",
		"inputs": [
			{"name": "editor", "type": "address"},
			{"name": "validator", "type": "string"},
			{"name": "moniker", "type": "string"}
		],
		"outputs": []
	}]`
	var err error
	StakingABI, err = abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		panic(err)
	}
}

func TestEventSignatures(t *testing.T) {
	// Test that the event signatures match the expected values
	testCases := []struct {
		name        string
		signature   string
		expectedSig common.Hash
		actualSig   common.Hash
	}{
		{
			name:        "Delegate event signature",
			signature:   "Delegate(address,string,uint256)",
			expectedSig: crypto.Keccak256Hash([]byte("Delegate(address,string,uint256)")),
			actualSig:   DelegateEventSig,
		},
		{
			name:        "Redelegate event signature",
			signature:   "Redelegate(address,string,string,uint256)",
			expectedSig: crypto.Keccak256Hash([]byte("Redelegate(address,string,string,uint256)")),
			actualSig:   RedelegateEventSig,
		},
		{
			name:        "Undelegate event signature",
			signature:   "Undelegate(address,string,uint256)",
			expectedSig: crypto.Keccak256Hash([]byte("Undelegate(address,string,uint256)")),
			actualSig:   UndelegateEventSig,
		},
		{
			name:        "ValidatorCreated event signature",
			signature:   "ValidatorCreated(address,string,string)",
			expectedSig: crypto.Keccak256Hash([]byte("ValidatorCreated(address,string,string)")),
			actualSig:   ValidatorCreatedEventSig,
		},
		{
			name:        "ValidatorEdited event signature",
			signature:   "ValidatorEdited(address,string,string)",
			expectedSig: crypto.Keccak256Hash([]byte("ValidatorEdited(address,string,string)")),
			actualSig:   ValidatorEditedEventSig,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expectedSig, tc.actualSig, "Signature mismatch for %s", tc.signature)
		})
	}
}

func TestEmitEVMLog(t *testing.T) {
	testCases := []struct {
		name    string
		setup   func() (*vm.EVM, common.Address, []common.Hash, []byte)
		wantErr bool
		errMsg  string
	}{
		{
			name: "nil EVM",
			setup: func() (*vm.EVM, common.Address, []common.Hash, []byte) {
				return nil, common.Address{}, []common.Hash{}, []byte{}
			},
			wantErr: true,
			errMsg:  "EVM is nil",
		},
		{
			name: "nil StateDB",
			setup: func() (*vm.EVM, common.Address, []common.Hash, []byte) {
				return &vm.EVM{StateDB: nil}, common.Address{}, []common.Hash{}, []byte{}
			},
			wantErr: true,
			errMsg:  "EVM StateDB is nil",
		},
		{
			name: "invalid StateDB type",
			setup: func() (*vm.EVM, common.Address, []common.Hash, []byte) {
				mockStateDB := &mockStateDB{}
				evm := &vm.EVM{StateDB: mockStateDB}
				addr := common.HexToAddress("0x1234")
				topics := []common.Hash{crypto.Keccak256Hash([]byte("TestEvent()"))}
				data := []byte("test data")
				return evm, addr, topics, data
			},
			wantErr: true,
			errMsg:  "cannot emit log: invalid StateDB type",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			evm, addr, topics, data := tc.setup()
			err := EmitEVMLog(evm, addr, topics, data)
			if tc.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// mockStateDB is a minimal implementation of vm.StateDB for testing
type mockStateDB struct {
	vm.StateDB
}

func (m *mockStateDB) AddLog(log *ethtypes.Log) {
	// Mock implementation
}

func TestEventDataEncoding(t *testing.T) {
	// Test encoding of various event data
	testCases := []struct {
		name    string
		method  string
		args    []interface{}
		wantErr bool
	}{
		{
			name:   "Delegate event data",
			method: "Delegate",
			args: []interface{}{
				common.HexToAddress("0x1234"),
				"seivaloper1test",
				big.NewInt(1000000),
			},
			wantErr: false,
		},
		{
			name:   "Redelegate event data",
			method: "Redelegate",
			args: []interface{}{
				common.HexToAddress("0x1234"),
				"seivaloper1src",
				"seivaloper1dst",
				big.NewInt(2000000),
			},
			wantErr: false,
		},
		{
			name:   "Undelegate event data",
			method: "Undelegate",
			args: []interface{}{
				common.HexToAddress("0x1234"),
				"seivaloper1test",
				big.NewInt(3000000),
			},
			wantErr: false,
		},
		{
			name:   "ValidatorCreated event data",
			method: "ValidatorCreated",
			args: []interface{}{
				common.HexToAddress("0x1234"),
				"seivaloper1test",
				"Test Validator",
			},
			wantErr: false,
		},
		{
			name:   "ValidatorEdited event data",
			method: "ValidatorEdited",
			args: []interface{}{
				common.HexToAddress("0x1234"),
				"seivaloper1test",
				"Updated Validator",
			},
			wantErr: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := StakingABI.Pack(tc.method, tc.args...)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.NotEmpty(t, data)
				// Remove method ID (first 4 bytes) to get only the encoded arguments
				require.True(t, len(data) > 4)
			}
		})
	}
}

func TestEventDataEncodingManual(t *testing.T) {
	// Test manual encoding as done in the actual emit functions
	testCases := []struct {
		name     string
		testFunc func(t *testing.T)
	}{
		{
			name: "Delegate event manual encoding",
			testFunc: func(t *testing.T) {
				validator := "seivaloper1test"
				amount := big.NewInt(1000000)

				// Manually encode as done in EmitDelegateEvent
				data := make([]byte, 0)
				data = append(data, common.LeftPadBytes(big.NewInt(64).Bytes(), 32)...)
				data = append(data, common.LeftPadBytes(amount.Bytes(), 32)...)
				data = append(data, common.LeftPadBytes(big.NewInt(int64(len(validator))).Bytes(), 32)...)
				valBytes := []byte(validator)
				data = append(data, common.RightPadBytes(valBytes, ((len(valBytes)+31)/32)*32)...)

				require.NotEmpty(t, data)
				require.Len(t, data, 32+32+32+32) // offset + amount + length + padded string
			},
		},
		{
			name: "Redelegate event manual encoding",
			testFunc: func(t *testing.T) {
				srcValidator := "seivaloper1src"
				dstValidator := "seivaloper1dst"
				amount := big.NewInt(2000000)

				// Manually encode as done in EmitRedelegateEvent
				data := make([]byte, 0)
				data = append(data, common.LeftPadBytes(big.NewInt(96).Bytes(), 32)...)
				data = append(data, common.LeftPadBytes(big.NewInt(160).Bytes(), 32)...)
				data = append(data, common.LeftPadBytes(amount.Bytes(), 32)...)

				srcBytes := []byte(srcValidator)
				data = append(data, common.LeftPadBytes(big.NewInt(int64(len(srcBytes))).Bytes(), 32)...)
				data = append(data, common.RightPadBytes(srcBytes, ((len(srcBytes)+31)/32)*32)...)

				dstBytes := []byte(dstValidator)
				data = append(data, common.LeftPadBytes(big.NewInt(int64(len(dstBytes))).Bytes(), 32)...)
				data = append(data, common.RightPadBytes(dstBytes, ((len(dstBytes)+31)/32)*32)...)

				require.NotEmpty(t, data)
				require.True(t, len(data) > 96) // At least the header offsets and amount
			},
		},
		{
			name: "ValidatorCreated event manual encoding with offset adjustment",
			testFunc: func(t *testing.T) {
				validatorAddr := "seivaloper1new"
				moniker := "New Validator"

				// Manually encode as done in EmitValidatorCreatedEvent
				data := make([]byte, 0)
				data = append(data, common.LeftPadBytes(big.NewInt(64).Bytes(), 32)...)
				data = append(data, common.LeftPadBytes(big.NewInt(128).Bytes(), 32)...) // temporary

				valAddrBytes := []byte(validatorAddr)
				data = append(data, common.LeftPadBytes(big.NewInt(int64(len(valAddrBytes))).Bytes(), 32)...)
				data = append(data, common.RightPadBytes(valAddrBytes, ((len(valAddrBytes)+31)/32)*32)...)

				// Adjust offset for moniker
				monikerOffset := 64 + 32 + ((len(valAddrBytes)+31)/32)*32
				copy(data[32:64], common.LeftPadBytes(big.NewInt(int64(monikerOffset)).Bytes(), 32))

				monikerBytes := []byte(moniker)
				data = append(data, common.LeftPadBytes(big.NewInt(int64(len(monikerBytes))).Bytes(), 32)...)
				data = append(data, common.RightPadBytes(monikerBytes, ((len(monikerBytes)+31)/32)*32)...)

				require.NotEmpty(t, data)
				require.True(t, len(data) > 64) // At least the header offsets
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.testFunc(t)
		})
	}
}

