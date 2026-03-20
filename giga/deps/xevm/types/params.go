package types

import (
	"errors"
	fmt "fmt"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	paramtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types"
	"gopkg.in/yaml.v2"
)

var (
	KeyPriorityNormalizer                  = []byte("KeyPriorityNormalizer")
	KeyMinFeePerGas                        = []byte("KeyMinFeePerGas")
	KeyMaxFeePerGas                        = []byte("KeyMaximumFeePerGas")
	KeyDeliverTxHookWasmGasLimit           = []byte("KeyDeliverTxHookWasmGasLimit")
	KeyMaxDynamicBaseFeeUpwardAdjustment   = []byte("KeyMaxDynamicBaseFeeUpwardAdjustment")
	KeyMaxDynamicBaseFeeDownwardAdjustment = []byte("KeyMaxDynamicBaseFeeDownwardAdjustment")
	KeyTargetGasUsedPerBlock               = []byte("KeyTargetGasUsedPerBlock")
	KeySeiSstoreSetGasEIP2200              = []byte("KeySeiSstoreSetGasEIP2200")
	// deprecated
	KeyBaseFeePerGas                          = []byte("KeyBaseFeePerGas")
	KeyWhitelistedCwCodeHashesForDelegateCall = []byte("KeyWhitelistedCwCodeHashesForDelegateCall")
	KeyRegisterPointerDisabled                = []byte("KeyRegisterPointerDisabled")
)

var DefaultPriorityNormalizer = sdk.NewDec(1)

// DefaultBaseFeePerGas determines how much usei per gas spent is
// burnt rather than go to validators (similar to base fee on
// Ethereum).
var DefaultBaseFeePerGas = sdk.NewDec(0)         // used for static base fee, deprecated in favor of dynamic base fee
var DefaultMinFeePerGas = sdk.NewDec(1000000000) // 1gwei
var DefaultDeliverTxHookWasmGasLimit = uint64(300000)

var DefaultWhitelistedCwCodeHashesForDelegateCall = generateDefaultWhitelistedCwCodeHashesForDelegateCall()

var DefaultMaxDynamicBaseFeeUpwardAdjustment = sdk.NewDecWithPrec(189, 4)  // 1.89%
var DefaultMaxDynamicBaseFeeDownwardAdjustment = sdk.NewDecWithPrec(39, 4) // .39%
var DefaultTargetGasUsedPerBlock = uint64(250000)                          // 250k
var DefaultMaxFeePerGas = sdk.NewDec(1000000000000)                        // 1,000gwei
var DefaultRegisterPointerDisabled = false
var DefaultSeiSstoreSetGasEIP2200 = uint64(20000) // 20k

var _ paramtypes.ParamSet = (*Params)(nil)

func ParamKeyTable() paramtypes.KeyTable {
	return paramtypes.NewKeyTable().RegisterParamSet(&Params{})
}

func DefaultParams() Params {
	return Params{
		PriorityNormalizer:                     DefaultPriorityNormalizer,
		BaseFeePerGas:                          DefaultBaseFeePerGas,
		MaxDynamicBaseFeeUpwardAdjustment:      DefaultMaxDynamicBaseFeeUpwardAdjustment,
		MaxDynamicBaseFeeDownwardAdjustment:    DefaultMaxDynamicBaseFeeDownwardAdjustment,
		MinimumFeePerGas:                       DefaultMinFeePerGas,
		DeliverTxHookWasmGasLimit:              DefaultDeliverTxHookWasmGasLimit,
		WhitelistedCwCodeHashesForDelegateCall: DefaultWhitelistedCwCodeHashesForDelegateCall,
		TargetGasUsedPerBlock:                  DefaultTargetGasUsedPerBlock,
		MaximumFeePerGas:                       DefaultMaxFeePerGas,
		RegisterPointerDisabled:                DefaultRegisterPointerDisabled,
		SeiSstoreSetGasEip2200:                 DefaultSeiSstoreSetGasEIP2200,
	}
}

func (p *Params) ParamSetPairs() paramtypes.ParamSetPairs {
	return paramtypes.ParamSetPairs{
		paramtypes.NewParamSetPair(KeyPriorityNormalizer, &p.PriorityNormalizer, validatePriorityNormalizer),
		paramtypes.NewParamSetPair(KeyBaseFeePerGas, &p.BaseFeePerGas, validateBaseFeePerGas),
		paramtypes.NewParamSetPair(KeyMaxDynamicBaseFeeUpwardAdjustment, &p.MaxDynamicBaseFeeUpwardAdjustment, validateBaseFeeAdjustment),
		paramtypes.NewParamSetPair(KeyMaxDynamicBaseFeeDownwardAdjustment, &p.MaxDynamicBaseFeeDownwardAdjustment, validateBaseFeeAdjustment),
		paramtypes.NewParamSetPair(KeyMinFeePerGas, &p.MinimumFeePerGas, validateMinFeePerGas),
		paramtypes.NewParamSetPair(KeyWhitelistedCwCodeHashesForDelegateCall, &p.WhitelistedCwCodeHashesForDelegateCall, validateWhitelistedCwHashesForDelegateCall),
		paramtypes.NewParamSetPair(KeyDeliverTxHookWasmGasLimit, &p.DeliverTxHookWasmGasLimit, validateDeliverTxHookWasmGasLimit),
		paramtypes.NewParamSetPair(KeyTargetGasUsedPerBlock, &p.TargetGasUsedPerBlock, func(i interface{}) error { return nil }),
		paramtypes.NewParamSetPair(KeySeiSstoreSetGasEIP2200, &p.SeiSstoreSetGasEip2200, validateSeiSstoreSetGasEIP2200),
		paramtypes.NewParamSetPair(KeyMaxFeePerGas, &p.MaximumFeePerGas, validateMaxFeePerGas),
		paramtypes.NewParamSetPair(KeyRegisterPointerDisabled, &p.RegisterPointerDisabled, validateRegisterPointerDisabled),
	}
}

func (ppre580 *ParamsPreV580) ParamSetPairs() paramtypes.ParamSetPairs {
	return paramtypes.ParamSetPairs{
		paramtypes.NewParamSetPair(KeyPriorityNormalizer, &ppre580.PriorityNormalizer, validatePriorityNormalizer),
		paramtypes.NewParamSetPair(KeyBaseFeePerGas, &ppre580.BaseFeePerGas, validateBaseFeePerGas),
		paramtypes.NewParamSetPair(KeyMinFeePerGas, &ppre580.MinimumFeePerGas, validateMinFeePerGas),
		paramtypes.NewParamSetPair(KeyWhitelistedCwCodeHashesForDelegateCall, &ppre580.WhitelistedCwCodeHashesForDelegateCall, validateWhitelistedCwHashesForDelegateCall),
	}
}

func (ppre600 *ParamsPreV600) ParamSetPairs() paramtypes.ParamSetPairs {
	return paramtypes.ParamSetPairs{
		paramtypes.NewParamSetPair(KeyPriorityNormalizer, &ppre600.PriorityNormalizer, validatePriorityNormalizer),
		paramtypes.NewParamSetPair(KeyBaseFeePerGas, &ppre600.BaseFeePerGas, validateBaseFeePerGas),
		paramtypes.NewParamSetPair(KeyMinFeePerGas, &ppre600.MinimumFeePerGas, validateMinFeePerGas),
		paramtypes.NewParamSetPair(KeyWhitelistedCwCodeHashesForDelegateCall, &ppre600.WhitelistedCwCodeHashesForDelegateCall, validateWhitelistedCwHashesForDelegateCall),
		paramtypes.NewParamSetPair(KeyDeliverTxHookWasmGasLimit, &ppre600.DeliverTxHookWasmGasLimit, validateDeliverTxHookWasmGasLimit),
	}
}

func (ppre601 *ParamsPreV601) ParamSetPairs() paramtypes.ParamSetPairs {
	return paramtypes.ParamSetPairs{
		paramtypes.NewParamSetPair(KeyPriorityNormalizer, &ppre601.PriorityNormalizer, validatePriorityNormalizer),
		paramtypes.NewParamSetPair(KeyBaseFeePerGas, &ppre601.BaseFeePerGas, validateBaseFeePerGas),
		paramtypes.NewParamSetPair(KeyMaxDynamicBaseFeeUpwardAdjustment, &ppre601.MaxDynamicBaseFeeUpwardAdjustment, validateBaseFeeAdjustment),
		paramtypes.NewParamSetPair(KeyMaxDynamicBaseFeeDownwardAdjustment, &ppre601.MaxDynamicBaseFeeDownwardAdjustment, validateBaseFeeAdjustment),
		paramtypes.NewParamSetPair(KeyMinFeePerGas, &ppre601.MinimumFeePerGas, validateMinFeePerGas),
		paramtypes.NewParamSetPair(KeyWhitelistedCwCodeHashesForDelegateCall, &ppre601.WhitelistedCwCodeHashesForDelegateCall, validateWhitelistedCwHashesForDelegateCall),
		paramtypes.NewParamSetPair(KeyDeliverTxHookWasmGasLimit, &ppre601.DeliverTxHookWasmGasLimit, validateDeliverTxHookWasmGasLimit),
		paramtypes.NewParamSetPair(KeyTargetGasUsedPerBlock, &ppre601.TargetGasUsedPerBlock, func(i interface{}) error { return nil }),
	}
}

func (ppre606 *ParamsPreV606) ParamSetPairs() paramtypes.ParamSetPairs {
	return paramtypes.ParamSetPairs{
		paramtypes.NewParamSetPair(KeyPriorityNormalizer, &ppre606.PriorityNormalizer, validatePriorityNormalizer),
		paramtypes.NewParamSetPair(KeyBaseFeePerGas, &ppre606.BaseFeePerGas, validateBaseFeePerGas),
		paramtypes.NewParamSetPair(KeyMaxDynamicBaseFeeUpwardAdjustment, &ppre606.MaxDynamicBaseFeeUpwardAdjustment, validateBaseFeeAdjustment),
		paramtypes.NewParamSetPair(KeyMaxDynamicBaseFeeDownwardAdjustment, &ppre606.MaxDynamicBaseFeeDownwardAdjustment, validateBaseFeeAdjustment),
		paramtypes.NewParamSetPair(KeyMinFeePerGas, &ppre606.MinimumFeePerGas, validateMinFeePerGas),
		paramtypes.NewParamSetPair(KeyWhitelistedCwCodeHashesForDelegateCall, &ppre606.WhitelistedCwCodeHashesForDelegateCall, validateWhitelistedCwHashesForDelegateCall),
		paramtypes.NewParamSetPair(KeyDeliverTxHookWasmGasLimit, &ppre606.DeliverTxHookWasmGasLimit, validateDeliverTxHookWasmGasLimit),
		paramtypes.NewParamSetPair(KeyTargetGasUsedPerBlock, &ppre606.TargetGasUsedPerBlock, func(i interface{}) error { return nil }),
		paramtypes.NewParamSetPair(KeyMaxFeePerGas, &ppre606.MaximumFeePerGas, validateMaxFeePerGas),
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
	if err := validateMaxFeePerGas(p.MaximumFeePerGas); err != nil {
		return err
	}
	if err := validateDeliverTxHookWasmGasLimit(p.DeliverTxHookWasmGasLimit); err != nil {
		return err
	}
	if p.MinimumFeePerGas.LT(p.BaseFeePerGas) {
		return errors.New("minimum fee cannot be lower than base fee")
	}
	if err := validateBaseFeeAdjustment(p.MaxDynamicBaseFeeUpwardAdjustment); err != nil {
		return fmt.Errorf("invalid max dynamic base fee upward adjustment: %s, err: %s", p.MaxDynamicBaseFeeUpwardAdjustment, err)
	}
	if err := validateBaseFeeAdjustment(p.MaxDynamicBaseFeeDownwardAdjustment); err != nil {
		return fmt.Errorf("invalid max dynamic base fee downward adjustment: %s, err: %s", p.MaxDynamicBaseFeeDownwardAdjustment, err)
	}
	if err := validateWhitelistedCwHashesForDelegateCall(p.WhitelistedCwCodeHashesForDelegateCall); err != nil {
		return fmt.Errorf("invalid whitelisted cw hashes for delegate call: %s", err)
	}
	if err := validateSeiSstoreSetGasEIP2200(p.SeiSstoreSetGasEip2200); err != nil {
		return fmt.Errorf("invalid sei sstore set gas eip2200: %s", err)
	}
	return nil
}

func validateBaseFeeAdjustment(i interface{}) error {
	adjustment, ok := i.(sdk.Dec)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	if adjustment.IsNegative() {
		return fmt.Errorf("negative base fee adjustment: %s", adjustment)
	}
	if adjustment.GT(sdk.OneDec()) {
		return fmt.Errorf("base fee adjustment must be less than or equal to 1: %s", adjustment)
	}
	return nil
}

func (p Params) String() string {
	out, _ := yaml.Marshal(p)
	return string(out)
}

func (ppre580 ParamsPreV580) String() string {
	out, _ := yaml.Marshal(ppre580)
	return string(out)
}

func (ppre600 ParamsPreV600) String() string {
	out, _ := yaml.Marshal(ppre600)
	return string(out)
}

func (ppre601 ParamsPreV601) String() string {
	out, _ := yaml.Marshal(ppre601)
	return string(out)
}

func (ppre606 ParamsPreV606) String() string {
	out, _ := yaml.Marshal(ppre606)
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

func validateMaxFeePerGas(i interface{}) error {
	v, ok := i.(sdk.Dec)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	if v.IsNegative() {
		return fmt.Errorf("negative max fee per gas: %d", v)
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

func validateRegisterPointerDisabled(i interface{}) error {
	_, ok := i.(bool)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	return nil
}

func validateSeiSstoreSetGasEIP2200(i interface{}) error {
	v, ok := i.(uint64)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	if v == 0 {
		return fmt.Errorf("invalid sei sstore set gas eip2200: must be greater than 0, got %d", v)
	}
	return nil
}

func generateDefaultWhitelistedCwCodeHashesForDelegateCall() [][]byte {
	return [][]byte(nil)
}
