package types

import (
	"errors"
	fmt "fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	"gopkg.in/yaml.v2"
)

var (
	KeyPriorityNormalizer        = []byte("KeyPriorityNormalizer")
	KeyBaseFeePerGas             = []byte("KeyBaseFeePerGas")
	KeyMinFeePerGas              = []byte("KeyMinFeePerGas")
	KeyDeliverTxHookWasmGasLimit = []byte("KeyDeliverTxHookWasmGasLimit")
	// deprecated
	KeyWhitelistedCwCodeHashesForDelegateCall = []byte("KeyWhitelistedCwCodeHashesForDelegateCall")
)

var DefaultPriorityNormalizer = sdk.NewDec(1)

// DefaultBaseFeePerGas determines how much usei per gas spent is
// burnt rather than go to validators (similar to base fee on
// Ethereum).
var DefaultBaseFeePerGas = sdk.NewDec(0)
var DefaultMinFeePerGas = sdk.NewDec(100000000000)
var DefaultDeliverTxHookWasmGasLimit = uint64(300000)

var DefaultWhitelistedCwCodeHashesForDelegateCall = generateDefaultWhitelistedCwCodeHashesForDelegateCall()

var _ paramtypes.ParamSet = (*Params)(nil)

func ParamKeyTable() paramtypes.KeyTable {
	return paramtypes.NewKeyTable().RegisterParamSet(&Params{})
}

func DefaultParams() Params {
	return Params{
		PriorityNormalizer:                     DefaultPriorityNormalizer,
		BaseFeePerGas:                          DefaultBaseFeePerGas,
		MinimumFeePerGas:                       DefaultMinFeePerGas,
		DeliverTxHookWasmGasLimit:              DefaultDeliverTxHookWasmGasLimit,
		WhitelistedCwCodeHashesForDelegateCall: DefaultWhitelistedCwCodeHashesForDelegateCall,
	}
}

func (p *Params) ParamSetPairs() paramtypes.ParamSetPairs {
	return paramtypes.ParamSetPairs{
		paramtypes.NewParamSetPair(KeyPriorityNormalizer, &p.PriorityNormalizer, validatePriorityNormalizer),
		paramtypes.NewParamSetPair(KeyBaseFeePerGas, &p.BaseFeePerGas, validateBaseFeePerGas),
		paramtypes.NewParamSetPair(KeyMinFeePerGas, &p.MinimumFeePerGas, validateMinFeePerGas),
		paramtypes.NewParamSetPair(KeyDeliverTxHookWasmGasLimit, &p.DeliverTxHookWasmGasLimit, validateDeliverTxHookWasmGasLimit),
		paramtypes.NewParamSetPair(KeyWhitelistedCwCodeHashesForDelegateCall, &p.WhitelistedCwCodeHashesForDelegateCall, validateWhitelistedCwHashesForDelegateCall),
	}
}

func (p Params) Validate() error {
	if err := validatePriorityNormalizer(p.PriorityNormalizer); err != nil {
		return err
	}
	if err := validateBaseFeePerGas(p.BaseFeePerGas); err != nil {
		return err
	}
	if err := validateMinFeePerGas(p.MinimumFeePerGas); err != nil {
		return err
	}
	if err := validateDeliverTxHookWasmGasLimit(p.DeliverTxHookWasmGasLimit); err != nil {
		return err
	}
	if p.MinimumFeePerGas.LT(p.BaseFeePerGas) {
		return errors.New("minimum fee cannot be lower than base fee")
	}
	return validateWhitelistedCwHashesForDelegateCall(p.WhitelistedCwCodeHashesForDelegateCall)
}

func (p Params) String() string {
	out, _ := yaml.Marshal(p)
	return string(out)
}

func validatePriorityNormalizer(i interface{}) error {
	v, ok := i.(sdk.Dec)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	if !v.IsPositive() {
		return fmt.Errorf("nonpositive priority normalizer: %d", v)
	}

	return nil
}

func validateBaseFeePerGas(i interface{}) error {
	v, ok := i.(sdk.Dec)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	if v.IsNegative() {
		return fmt.Errorf("negative base fee per gas: %d", v)
	}

	return nil
}

func validateMinFeePerGas(i interface{}) error {
	v, ok := i.(sdk.Dec)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	if v.IsNegative() {
		return fmt.Errorf("negative min fee per gas: %d", v)
	}

	return nil
}

func validateDeliverTxHookWasmGasLimit(i interface{}) error {
	v, ok := i.(uint64)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	if v == 0 {
		return fmt.Errorf("invalid deliver_tx_hook_wasm_gas_limit: must be greater than 0, got %d", v)
	}
	return nil
}

func validateWhitelistedCwHashesForDelegateCall(i interface{}) error {
	_, ok := i.([][]byte)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	return nil
}

func generateDefaultWhitelistedCwCodeHashesForDelegateCall() [][]byte {
	return [][]byte(nil)
}
