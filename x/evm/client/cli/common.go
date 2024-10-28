package cli

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

func getMethodPayload(newAbi abi.ABI, args []string) ([]byte, error) {
	method := newAbi.Methods[args[0]]
	abiArgs := []interface{}{}
	for i, input := range method.Inputs {
		idx := i + 1
		if idx >= len(args) {
			return nil, errors.New("not enough arguments")
		}
		var arg interface{}
		var err error
		switch input.Type.T {
		case abi.IntTy:
			if input.Type.Size > 64 {
				bi, success := new(big.Int).SetString(args[idx], 10)
				if !success {
					return nil, errors.New("invalid big.Int")
				} else {
					arg = bi
				}
			} else {
				val, e := strconv.ParseInt(args[idx], 10, 64)
				err = e
				switch input.Type.Size {
				case 8:
					if val > math.MaxInt8 {
						return nil, errors.New("int8 overflow")
					}
					arg = int8(val)
				case 16:
					if val > math.MaxInt16 {
						return nil, errors.New("int16 overflow")
					}
					arg = int16(val)
				case 32:
					if val > math.MaxInt32 {
						return nil, errors.New("int32 overflow")
					}
					arg = int32(val)
				case 64:
					arg = val
				}
			}
		case abi.UintTy:
			if input.Type.Size > 64 {
				bi, success := new(big.Int).SetString(args[idx], 10)
				if !success {
					return nil, errors.New("invalid big.Int")
				} else {
					arg = bi
				}
			} else {
				val, e := strconv.ParseUint(args[idx], 10, 64)
				err = e
				switch input.Type.Size {
				case 8:
					if val > math.MaxUint8 {
						return nil, errors.New("uint8 overflow")
					}
					arg = uint8(val)
				case 16:
					if val > math.MaxUint16 {
						return nil, errors.New("uint16 overflow")
					}
					arg = uint16(val)
				case 32:
					if val > math.MaxUint32 {
						return nil, errors.New("uint32 overflow")
					}
					arg = uint32(val)
				case 64:
					arg = val
				}
			}
		case abi.BoolTy:
			if args[idx] != TrueStr && args[idx] != FalseStr {
				return nil, fmt.Errorf("boolean argument has to be either \"%s\" or \"%s\"", TrueStr, FalseStr)
			} else {
				arg = args[idx] == TrueStr
			}
		case abi.StringTy:
			arg = args[idx]
		case abi.AddressTy:
			arg = common.HexToAddress(args[idx])
		default:
			return nil, errors.New("argument type not supported yet")
		}
		if err != nil {
			return nil, err
		}
		abiArgs = append(abiArgs, arg)
	}

	return newAbi.Pack(args[0], abiArgs...)
}
