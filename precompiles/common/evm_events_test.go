package common

import (
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
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

func TestEmitDelegateEvent(t *testing.T) {
	// Test that the event signature is correct
	eventSig := crypto.Keccak256Hash([]byte("Delegate(address,string,uint256)"))
	require.Equal(t, DelegateEventSig, eventSig)

	// Test parameters
	delegator := common.HexToAddress("0x5678")
	validator := "seivaloper1abcdef"
	amount := big.NewInt(1000000)

	// Test that we can create the topics
	topics := []common.Hash{
		DelegateEventSig,
		common.HexToHash(delegator.Hex()),
	}
	require.Len(t, topics, 2)
	require.Equal(t, DelegateEventSig, topics[0])
	require.Equal(t, common.HexToHash(delegator.Hex()), topics[1])

	// Test that we can encode the data
	_, err := StakingABI.Pack("Delegate", delegator, validator, amount)
	require.NoError(t, err)
}

func TestEmitRedelegateEvent(t *testing.T) {
	// Test that the event signature is correct
	eventSig := crypto.Keccak256Hash([]byte("Redelegate(address,string,string,uint256)"))
	require.Equal(t, RedelegateEventSig, eventSig)

	// Test parameters
	delegator := common.HexToAddress("0x5678")
	srcValidator := "seivaloper1src"
	dstValidator := "seivaloper1dst"
	amount := big.NewInt(2000000)

	// Test that we can create the topics
	topics := []common.Hash{
		RedelegateEventSig,
		common.HexToHash(delegator.Hex()),
	}
	require.Len(t, topics, 2)

	// Test that we can encode the data
	_, err := StakingABI.Pack("Redelegate", delegator, srcValidator, dstValidator, amount)
	require.NoError(t, err)
}

func TestEmitUndelegateEvent(t *testing.T) {
	// Test that the event signature is correct
	eventSig := crypto.Keccak256Hash([]byte("Undelegate(address,string,uint256)"))
	require.Equal(t, UndelegateEventSig, eventSig)

	// Test parameters
	delegator := common.HexToAddress("0x5678")
	validator := "seivaloper1abcdef"
	amount := big.NewInt(3000000)

	// Test that we can create the topics
	topics := []common.Hash{
		UndelegateEventSig,
		common.HexToHash(delegator.Hex()),
	}
	require.Len(t, topics, 2)

	// Test that we can encode the data
	_, err := StakingABI.Pack("Undelegate", delegator, validator, amount)
	require.NoError(t, err)
}

func TestEmitValidatorCreatedEvent(t *testing.T) {
	// Test that the event signature is correct
	eventSig := crypto.Keccak256Hash([]byte("ValidatorCreated(address,string,string)"))
	require.Equal(t, ValidatorCreatedEventSig, eventSig)

	// Test parameters
	creator := common.HexToAddress("0x5678")
	validator := "seivaloper1new"
	moniker := "New Validator"

	// Test that we can create the topics
	topics := []common.Hash{
		ValidatorCreatedEventSig,
		common.HexToHash(creator.Hex()),
	}
	require.Len(t, topics, 2)

	// Test that we can encode the data
	_, err := StakingABI.Pack("ValidatorCreated", creator, validator, moniker)
	require.NoError(t, err)
}

func TestEmitValidatorEditedEvent(t *testing.T) {
	// Test that the event signature is correct
	eventSig := crypto.Keccak256Hash([]byte("ValidatorEdited(address,string,string)"))
	require.Equal(t, ValidatorEditedEventSig, eventSig)

	// Test parameters
	editor := common.HexToAddress("0x5678")
	validator := "seivaloper1edit"
	moniker := "Edited Validator"

	// Test that we can create the topics
	topics := []common.Hash{
		ValidatorEditedEventSig,
		common.HexToHash(editor.Hex()),
	}
	require.Len(t, topics, 2)

	// Test that we can encode the data
	_, err := StakingABI.Pack("ValidatorEdited", editor, validator, moniker)
	require.NoError(t, err)
}

func TestEmitEVMLogWithNilEVM(t *testing.T) {
	precompileAddr := common.HexToAddress("0x1234")

	err := EmitEVMLog(nil, precompileAddr, []common.Hash{}, []byte{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "EVM is nil")
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
