package json

import (
	"bytes"
	"embed"
	gjson "encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/utils"
)

const (
	ExtractAsBytesMethod     = "extractAsBytes"
	ExtractAsBytesListMethod = "extractAsBytesList"
	ExtractAsUint256Method   = "extractAsUint256"
)

const JSONAddress = "0x0000000000000000000000000000000000001003"
const GasCostPerByte = 100 // TODO: parameterize

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

type PrecompileExecutor struct {
	ExtractAsBytesID     []byte
	ExtractAsBytesListID []byte
	ExtractAsUint256ID   []byte
}

func ABI() (*abi.ABI, error) {
	abiBz, err := f.ReadFile("abi.json")
	if err != nil {
		return nil, fmt.Errorf("error loading the json ABI %s", err)
	}

	newAbi, err := abi.JSON(bytes.NewReader(abiBz))
	if err != nil {
		return nil, err
	}
	return &newAbi, nil
}

func NewPrecompile() (*pcommon.Precompile, error) {
	newAbi, err := ABI()
	if err != nil {
		return nil, err
	}

	p := &PrecompileExecutor{}

	for name, m := range newAbi.Methods {
		switch name {
		case ExtractAsBytesMethod:
			p.ExtractAsBytesID = m.ID
		case ExtractAsBytesListMethod:
			p.ExtractAsBytesListID = m.ID
		case ExtractAsUint256Method:
			p.ExtractAsUint256ID = m.ID
		}
	}

	return pcommon.NewPrecompile(*newAbi, p, common.HexToAddress(JSONAddress), "json"), nil
}

// RequiredGas returns the required bare minimum gas to execute the precompile.
func (p PrecompileExecutor) RequiredGas(input []byte, method *abi.Method) uint64 {
	return uint64(GasCostPerByte * len(input))
}

func (p PrecompileExecutor) Execute(ctx sdk.Context, method *abi.Method, caller common.Address, callingContract common.Address, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM) (bz []byte, err error) {
	switch method.Name {
	case ExtractAsBytesMethod:
		return p.extractAsBytes(ctx, method, args, value)
	case ExtractAsBytesListMethod:
		return p.extractAsBytesList(ctx, method, args, value)
	case ExtractAsUint256Method:
		byteArr := make([]byte, 32)
		uint_, err := p.ExtractAsUint256(ctx, method, args, value)
		if err != nil {
			return nil, err
		}

		if uint_.BitLen() > 256 {
			return nil, errors.New("value does not fit in 32 bytes")
		}

		uint_.FillBytes(byteArr)
		return byteArr, nil
	}
	return
}

func (p PrecompileExecutor) extractAsBytes(_ sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}

	if err := pcommon.ValidateArgsLength(args, 2); err != nil {
		return nil, err
	}

	// type assertion will always succeed because it's already validated in p.Prepare call in Run()
	bz := args[0].([]byte)
	decoded := map[string]gjson.RawMessage{}
	if err := gjson.Unmarshal(bz, &decoded); err != nil {
		return nil, err
	}
	key := args[1].(string)
	result, ok := decoded[key]
	if !ok {
		return nil, fmt.Errorf("input does not contain key %s", key)
	}
	// in the case of a string value, remove the quotes
	if len(result) >= 2 && result[0] == '"' && result[len(result)-1] == '"' {
		result = result[1 : len(result)-1]
	}

	return method.Outputs.Pack([]byte(result))
}

func (p PrecompileExecutor) extractAsBytesList(_ sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}

	if err := pcommon.ValidateArgsLength(args, 2); err != nil {
		return nil, err
	}

	// type assertion will always succeed because it's already validated in p.Prepare call in Run()
	bz := args[0].([]byte)
	decoded := map[string]gjson.RawMessage{}
	if err := gjson.Unmarshal(bz, &decoded); err != nil {
		return nil, err
	}
	key := args[1].(string)
	result, ok := decoded[key]
	if !ok {
		return nil, fmt.Errorf("input does not contain key %s", key)
	}
	decodedResult := []gjson.RawMessage{}
	if err := gjson.Unmarshal(result, &decodedResult); err != nil {
		return nil, err
	}

	return method.Outputs.Pack(utils.Map(decodedResult, func(r gjson.RawMessage) []byte { return []byte(r) }))
}

func (p PrecompileExecutor) ExtractAsUint256(_ sdk.Context, _ *abi.Method, args []interface{}, value *big.Int) (*big.Int, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}

	if err := pcommon.ValidateArgsLength(args, 2); err != nil {
		return nil, err
	}

	// type assertion will always succeed because it's already validated in p.Prepare call in Run()
	bz := args[0].([]byte)
	decoded := map[string]gjson.RawMessage{}
	if err := gjson.Unmarshal(bz, &decoded); err != nil {
		return nil, err
	}
	key := args[1].(string)
	result, ok := decoded[key]
	if !ok {
		return nil, fmt.Errorf("input does not contain key %s", key)
	}

	// Assuming result is your byte slice
	// Convert byte slice to string and trim quotation marks
	strValue := strings.Trim(string(result), "\"")

	// Convert the string to big.Int
	value, success := new(big.Int).SetString(strValue, 10)
	if !success {
		return nil, fmt.Errorf("failed to convert %s to big.Int", strValue)
	}

	return value, nil
}
