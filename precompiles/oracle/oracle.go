package oracle

import (
	"embed"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/x/oracle/types"
)

const (
	GetExchangeRatesMethod = "getExchangeRates"
	GetOracleTwapsMethod   = "getOracleTwaps"
)

const (
	OracleAddress = "0x0000000000000000000000000000000000001008"
)

// Embed abi json file to the executable binary. Needed when importing as dependency.
//
//go:embed abi.json
var f embed.FS

type PrecompileExecutor struct {
	evmKeeper    pcommon.EVMKeeper
	oracleKeeper pcommon.OracleKeeper

	GetExchangeRatesId []byte
	GetOracleTwapsId   []byte
}

// Define types which deviate slightly from cosmos types (ExchangeRate string vs sdk.Dec)
type OracleExchangeRate struct {
	ExchangeRate        string `json:"exchangeRate"`
	LastUpdate          string `json:"lastUpdate"`
	LastUpdateTimestamp int64  `json:"lastUpdateTimestamp"`
}

type DenomOracleExchangeRatePair struct {
	Denom                 string             `json:"denom"`
	OracleExchangeRateVal OracleExchangeRate `json:"oracleExchangeRateVal"`
}

type OracleTwap struct {
	Denom           string `json:"denom"`
	Twap            string `json:"twap"`
	LookbackSeconds int64  `json:"lookbackSeconds"`
}

func NewPrecompile(oracleKeeper pcommon.OracleKeeper, evmKeeper pcommon.EVMKeeper) (*pcommon.Precompile, error) {
	newAbi := pcommon.MustGetABI(f, "abi.json")

	p := &PrecompileExecutor{
		evmKeeper:    evmKeeper,
		oracleKeeper: oracleKeeper,
	}

	for name, m := range newAbi.Methods {
		switch name {
		case GetExchangeRatesMethod:
			p.GetExchangeRatesId = m.ID
		case GetOracleTwapsMethod:
			p.GetOracleTwapsId = m.ID
		}
	}

	return pcommon.NewPrecompile(newAbi, p, common.HexToAddress(OracleAddress), "oracle"), nil
}

// RequiredGas returns the required bare minimum gas to execute the precompile.
func (p PrecompileExecutor) RequiredGas(input []byte, method *abi.Method) uint64 {
	return pcommon.DefaultGasCost(input, p.IsTransaction(method.Name))
}

func (p PrecompileExecutor) Execute(ctx sdk.Context, method *abi.Method, caller common.Address, callingContract common.Address, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM) (bz []byte, err error) {
	switch method.Name {
	case GetExchangeRatesMethod:
		return p.getExchangeRates(ctx, method, args, value)
	case GetOracleTwapsMethod:
		return p.getOracleTwaps(ctx, method, args, value)
	}
	return
}

func (p PrecompileExecutor) getExchangeRates(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}

	if err := pcommon.ValidateArgsLength(args, 0); err != nil {
		return nil, err
	}
	exchangeRates := []DenomOracleExchangeRatePair{}
	p.oracleKeeper.IterateBaseExchangeRates(ctx, func(denom string, rate types.OracleExchangeRate) (stop bool) {
		exchangeRates = append(exchangeRates, DenomOracleExchangeRatePair{Denom: denom, OracleExchangeRateVal: OracleExchangeRate{ExchangeRate: rate.ExchangeRate.String(), LastUpdate: rate.LastUpdate.String(), LastUpdateTimestamp: rate.LastUpdateTimestamp}})
		return false
	})

	return method.Outputs.Pack(exchangeRates)
}

func (p PrecompileExecutor) getOracleTwaps(ctx sdk.Context, method *abi.Method, args []interface{}, value *big.Int) ([]byte, error) {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}

	if err := pcommon.ValidateArgsLength(args, 1); err != nil {
		return nil, err
	}
	lookbackSeconds := args[0].(uint64)
	twaps, err := p.oracleKeeper.CalculateTwaps(ctx, lookbackSeconds)
	if err != nil {
		return nil, err
	}
	// Convert twap to string
	oracleTwaps := make([]OracleTwap, 0, len(twaps))
	for _, twap := range twaps {
		oracleTwaps = append(oracleTwaps, OracleTwap{Denom: twap.Denom, Twap: twap.Twap.String(), LookbackSeconds: twap.LookbackSeconds})
	}
	return method.Outputs.Pack(oracleTwaps)
}

func (PrecompileExecutor) IsTransaction(string) bool {
	return false
}
