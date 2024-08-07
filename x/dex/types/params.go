package types

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	"gopkg.in/yaml.v2"
)

var (
	KeyPriceSnapshotRetention     = []byte("PriceSnapshotRetention") // number of epochs to retain price snapshots for
	KeySudoCallGasPrice           = []byte("KeySudoCallGasPrice")    // gas price for sudo calls from endblock
	KeyBeginBlockGasLimit         = []byte("KeyBeginBlockGasLimit")
	KeyEndBlockGasLimit           = []byte("KeyEndBlockGasLimit")
	KeyDefaultGasPerOrder         = []byte("KeyDefaultGasPerOrder")
	KeyDefaultGasPerCancel        = []byte("KeyDefaultGasPerCancel")
	KeyMinRentDeposit             = []byte("KeyMinRentDeposit")
	KeyGasAllowancePerSettlement  = []byte("KeyGasAllowancePerSettlement")
	KeyMinProcessableRent         = []byte("KeyMinProcessableRent")
	KeyOrderBookEntriesPerLoad    = []byte("KeyOrderBookEntriesPerLoad")
	KeyContractUnsuspendCost      = []byte("KeyContractUnsuspendCost")
	KeyMaxOrderPerPrice           = []byte("KeyMaxOrderPerPrice")
	KeyMaxPairsPerContract        = []byte("KeyMaxPairsPerContract")
	KeyDefaultGasPerOrderDataByte = []byte("KeyDefaultGasPerOrderDataByte")
)

const (
	DefaultPriceSnapshotRetention     = 24 * 3600  // default to one day
	DefaultBeginBlockGasLimit         = 200000000  // 200M
	DefaultEndBlockGasLimit           = 1000000000 // 1B
	DefaultDefaultGasPerOrder         = 55000
	DefaultDefaultGasPerCancel        = 53000
	DefaultMinRentDeposit             = 10000000 // 10 sei
	DefaultGasAllowancePerSettlement  = 10000
	DefaultMinProcessableRent         = 100000
	DefaultOrderBookEntriesPerLoad    = 10
	DefaultContractUnsuspendCost      = 1000000
	DefaultMaxOrderPerPrice           = 10000
	DefaultMaxPairsPerContract        = 100
	DefaultDefaultGasPerOrderDataByte = 30
)

var DefaultSudoCallGasPrice = sdk.NewDecWithPrec(1, 1) // 0.1

var _ paramtypes.ParamSet = (*Params)(nil)

// ParamKeyTable the param key table for launch module
func ParamKeyTable() paramtypes.KeyTable {
	return paramtypes.NewKeyTable().RegisterParamSet(&Params{})
}

// DefaultParams returns a default set of parameters
func DefaultParams() Params {
	return Params{
		PriceSnapshotRetention:     DefaultPriceSnapshotRetention,
		SudoCallGasPrice:           DefaultSudoCallGasPrice,
		BeginBlockGasLimit:         DefaultBeginBlockGasLimit,
		EndBlockGasLimit:           DefaultEndBlockGasLimit,
		DefaultGasPerOrder:         DefaultDefaultGasPerOrder,
		DefaultGasPerCancel:        DefaultDefaultGasPerCancel,
		MinRentDeposit:             DefaultMinRentDeposit,
		GasAllowancePerSettlement:  DefaultGasAllowancePerSettlement,
		MinProcessableRent:         DefaultMinProcessableRent,
		OrderBookEntriesPerLoad:    DefaultOrderBookEntriesPerLoad,
		ContractUnsuspendCost:      DefaultContractUnsuspendCost,
		MaxOrderPerPrice:           DefaultMaxOrderPerPrice,
		MaxPairsPerContract:        DefaultMaxPairsPerContract,
		DefaultGasPerOrderDataByte: DefaultDefaultGasPerOrderDataByte,
	}
}

// ParamSetPairs get the params.ParamSet
func (p *Params) ParamSetPairs() paramtypes.ParamSetPairs {
	return paramtypes.ParamSetPairs{
		paramtypes.NewParamSetPair(KeyPriceSnapshotRetention, &p.PriceSnapshotRetention, validatePriceSnapshotRetention),
		paramtypes.NewParamSetPair(KeySudoCallGasPrice, &p.SudoCallGasPrice, validateSudoCallGasPrice),
		paramtypes.NewParamSetPair(KeyBeginBlockGasLimit, &p.BeginBlockGasLimit, validateUint64Param),
		paramtypes.NewParamSetPair(KeyEndBlockGasLimit, &p.EndBlockGasLimit, validateUint64Param),
		paramtypes.NewParamSetPair(KeyDefaultGasPerOrder, &p.DefaultGasPerOrder, validateUint64Param),
		paramtypes.NewParamSetPair(KeyDefaultGasPerCancel, &p.DefaultGasPerCancel, validateUint64Param),
		paramtypes.NewParamSetPair(KeyMinRentDeposit, &p.MinRentDeposit, validateUint64Param),
		paramtypes.NewParamSetPair(KeyGasAllowancePerSettlement, &p.GasAllowancePerSettlement, validateUint64Param),
		paramtypes.NewParamSetPair(KeyMinProcessableRent, &p.MinProcessableRent, validateUint64Param),
		paramtypes.NewParamSetPair(KeyOrderBookEntriesPerLoad, &p.OrderBookEntriesPerLoad, validateUint64Param),
		paramtypes.NewParamSetPair(KeyContractUnsuspendCost, &p.ContractUnsuspendCost, validateUint64Param),
		paramtypes.NewParamSetPair(KeyMaxOrderPerPrice, &p.MaxOrderPerPrice, validateUint64Param),
		paramtypes.NewParamSetPair(KeyMaxPairsPerContract, &p.MaxPairsPerContract, validateUint64Param),
		paramtypes.NewParamSetPair(KeyDefaultGasPerOrderDataByte, &p.DefaultGasPerOrderDataByte, validateUint64Param),
	}
}

// Validate validates the set of params
func (p Params) Validate() error {
	if err := validatePriceSnapshotRetention(p.PriceSnapshotRetention); err != nil {
		return err
	}
	if err := validateSudoCallGasPrice(p.SudoCallGasPrice); err != nil {
		return err
	}
	// it's not possible for other params to fail validation if they've already
	// made it into Params' fields.
	return nil
}

// String implements the Stringer interface.
func (p Params) String() string {
	out, _ := yaml.Marshal(p)
	return string(out)
}

func validatePriceSnapshotRetention(i interface{}) error {
	v, ok := i.(uint64)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	if v == 0 {
		return fmt.Errorf("price snapshot retention must be a positive integer: %d", v)
	}

	return nil
}

func validateSudoCallGasPrice(i interface{}) error {
	price, ok := i.(sdk.Dec)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	if !price.IsPositive() {
		return fmt.Errorf("nonpositive sudo call price")
	}
	return nil
}

func validateUint64Param(i interface{}) error {
	_, ok := i.(uint64)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	return nil
}
