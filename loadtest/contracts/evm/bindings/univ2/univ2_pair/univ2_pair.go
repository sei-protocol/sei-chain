// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package univ2_pair

import (
	"errors"
	"math/big"
	"strings"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
)

// Reference imports to suppress errors if they are not otherwise used.
var (
	_ = errors.New
	_ = big.NewInt
	_ = strings.NewReader
	_ = ethereum.NotFound
	_ = bind.Bind
	_ = common.Big1
	_ = types.BloomLookup
	_ = event.NewSubscription
	_ = abi.ConvertType
)

// Univ2PairMetaData contains all meta data concerning the Univ2Pair contract.
var Univ2PairMetaData = &bind.MetaData{
	ABI: "[{\"type\":\"constructor\",\"inputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"DOMAIN_SEPARATOR\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"MINIMUM_LIQUIDITY\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"SELECTOR\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"bytes4\",\"internalType\":\"bytes4\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"allowance\",\"inputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"approve\",\"inputs\":[{\"name\":\"spender\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"amount\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"balanceOf\",\"inputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"blockTimestampLast\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint32\",\"internalType\":\"uint32\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"burn\",\"inputs\":[{\"name\":\"to\",\"type\":\"address\",\"internalType\":\"address\"}],\"outputs\":[{\"name\":\"amount0\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"amount1\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"decimals\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint8\",\"internalType\":\"uint8\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getReserves\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint112\",\"internalType\":\"uint112\"},{\"name\":\"\",\"type\":\"uint112\",\"internalType\":\"uint112\"},{\"name\":\"\",\"type\":\"uint32\",\"internalType\":\"uint32\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"initialize\",\"inputs\":[{\"name\":\"_token0\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"_token1\",\"type\":\"address\",\"internalType\":\"address\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"mint\",\"inputs\":[{\"name\":\"to\",\"type\":\"address\",\"internalType\":\"address\"}],\"outputs\":[{\"name\":\"liquidity\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"name\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"string\",\"internalType\":\"string\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"nonces\",\"inputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"permit\",\"inputs\":[{\"name\":\"owner\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"spender\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"value\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"deadline\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"v\",\"type\":\"uint8\",\"internalType\":\"uint8\"},{\"name\":\"r\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"s\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"price0CumulativeLast\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"price1CumulativeLast\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"reserve0\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint112\",\"internalType\":\"uint112\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"reserve1\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint112\",\"internalType\":\"uint112\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"swap\",\"inputs\":[{\"name\":\"amount0Out\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"amount1Out\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"to\",\"type\":\"address\",\"internalType\":\"address\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"symbol\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"string\",\"internalType\":\"string\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"sync\",\"inputs\":[],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"token0\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"token1\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"totalSupply\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"transfer\",\"inputs\":[{\"name\":\"to\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"amount\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"transferFrom\",\"inputs\":[{\"name\":\"from\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"to\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"amount\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"nonpayable\"},{\"type\":\"event\",\"name\":\"Approval\",\"inputs\":[{\"name\":\"owner\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"spender\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"amount\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"Burn\",\"inputs\":[{\"name\":\"_operator\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"_value\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"Initialized\",\"inputs\":[{\"name\":\"version\",\"type\":\"uint64\",\"indexed\":false,\"internalType\":\"uint64\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"Mint\",\"inputs\":[{\"name\":\"_operator\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"_value\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"Swap\",\"inputs\":[{\"name\":\"sender\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"amount0Out\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"amount1Out\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"to\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"Sync\",\"inputs\":[{\"name\":\"reserve0\",\"type\":\"uint112\",\"indexed\":false,\"internalType\":\"uint112\"},{\"name\":\"reserve1\",\"type\":\"uint112\",\"indexed\":false,\"internalType\":\"uint112\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"Transfer\",\"inputs\":[{\"name\":\"from\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"to\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"amount\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"error\",\"name\":\"BalanceOverflow\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"InsufficientLiquidity\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"InsufficientLiquidityBurned\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"InsufficientLiquidityMinted\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"InvalidAmount\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"InvalidConstantProductFormula\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"InvalidInitialization\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"NotInitializing\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"SafeTransferFailed\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"SwapToSelf\",\"inputs\":[]}]",
}

// Univ2PairABI is the input ABI used to generate the binding from.
// Deprecated: Use Univ2PairMetaData.ABI instead.
var Univ2PairABI = Univ2PairMetaData.ABI

// Univ2Pair is an auto generated Go binding around an Ethereum contract.
type Univ2Pair struct {
	Univ2PairCaller     // Read-only binding to the contract
	Univ2PairTransactor // Write-only binding to the contract
	Univ2PairFilterer   // Log filterer for contract events
}

// Univ2PairCaller is an auto generated read-only Go binding around an Ethereum contract.
type Univ2PairCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// Univ2PairTransactor is an auto generated write-only Go binding around an Ethereum contract.
type Univ2PairTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// Univ2PairFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type Univ2PairFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// Univ2PairSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type Univ2PairSession struct {
	Contract     *Univ2Pair        // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// Univ2PairCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type Univ2PairCallerSession struct {
	Contract *Univ2PairCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts    // Call options to use throughout this session
}

// Univ2PairTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type Univ2PairTransactorSession struct {
	Contract     *Univ2PairTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts    // Transaction auth options to use throughout this session
}

// Univ2PairRaw is an auto generated low-level Go binding around an Ethereum contract.
type Univ2PairRaw struct {
	Contract *Univ2Pair // Generic contract binding to access the raw methods on
}

// Univ2PairCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type Univ2PairCallerRaw struct {
	Contract *Univ2PairCaller // Generic read-only contract binding to access the raw methods on
}

// Univ2PairTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type Univ2PairTransactorRaw struct {
	Contract *Univ2PairTransactor // Generic write-only contract binding to access the raw methods on
}

// NewUniv2Pair creates a new instance of Univ2Pair, bound to a specific deployed contract.
func NewUniv2Pair(address common.Address, backend bind.ContractBackend) (*Univ2Pair, error) {
	contract, err := bindUniv2Pair(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &Univ2Pair{Univ2PairCaller: Univ2PairCaller{contract: contract}, Univ2PairTransactor: Univ2PairTransactor{contract: contract}, Univ2PairFilterer: Univ2PairFilterer{contract: contract}}, nil
}

// NewUniv2PairCaller creates a new read-only instance of Univ2Pair, bound to a specific deployed contract.
func NewUniv2PairCaller(address common.Address, caller bind.ContractCaller) (*Univ2PairCaller, error) {
	contract, err := bindUniv2Pair(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &Univ2PairCaller{contract: contract}, nil
}

// NewUniv2PairTransactor creates a new write-only instance of Univ2Pair, bound to a specific deployed contract.
func NewUniv2PairTransactor(address common.Address, transactor bind.ContractTransactor) (*Univ2PairTransactor, error) {
	contract, err := bindUniv2Pair(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &Univ2PairTransactor{contract: contract}, nil
}

// NewUniv2PairFilterer creates a new log filterer instance of Univ2Pair, bound to a specific deployed contract.
func NewUniv2PairFilterer(address common.Address, filterer bind.ContractFilterer) (*Univ2PairFilterer, error) {
	contract, err := bindUniv2Pair(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &Univ2PairFilterer{contract: contract}, nil
}

// bindUniv2Pair binds a generic wrapper to an already deployed contract.
func bindUniv2Pair(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := Univ2PairMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Univ2Pair *Univ2PairRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _Univ2Pair.Contract.Univ2PairCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Univ2Pair *Univ2PairRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Univ2Pair.Contract.Univ2PairTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Univ2Pair *Univ2PairRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Univ2Pair.Contract.Univ2PairTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Univ2Pair *Univ2PairCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _Univ2Pair.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Univ2Pair *Univ2PairTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Univ2Pair.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Univ2Pair *Univ2PairTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Univ2Pair.Contract.contract.Transact(opts, method, params...)
}

// DOMAINSEPARATOR is a free data retrieval call binding the contract method 0x3644e515.
//
// Solidity: function DOMAIN_SEPARATOR() view returns(bytes32)
func (_Univ2Pair *Univ2PairCaller) DOMAINSEPARATOR(opts *bind.CallOpts) ([32]byte, error) {
	var out []interface{}
	err := _Univ2Pair.contract.Call(opts, &out, "DOMAIN_SEPARATOR")

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// DOMAINSEPARATOR is a free data retrieval call binding the contract method 0x3644e515.
//
// Solidity: function DOMAIN_SEPARATOR() view returns(bytes32)
func (_Univ2Pair *Univ2PairSession) DOMAINSEPARATOR() ([32]byte, error) {
	return _Univ2Pair.Contract.DOMAINSEPARATOR(&_Univ2Pair.CallOpts)
}

// DOMAINSEPARATOR is a free data retrieval call binding the contract method 0x3644e515.
//
// Solidity: function DOMAIN_SEPARATOR() view returns(bytes32)
func (_Univ2Pair *Univ2PairCallerSession) DOMAINSEPARATOR() ([32]byte, error) {
	return _Univ2Pair.Contract.DOMAINSEPARATOR(&_Univ2Pair.CallOpts)
}

// MINIMUMLIQUIDITY is a free data retrieval call binding the contract method 0xba9a7a56.
//
// Solidity: function MINIMUM_LIQUIDITY() view returns(uint256)
func (_Univ2Pair *Univ2PairCaller) MINIMUMLIQUIDITY(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _Univ2Pair.contract.Call(opts, &out, "MINIMUM_LIQUIDITY")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// MINIMUMLIQUIDITY is a free data retrieval call binding the contract method 0xba9a7a56.
//
// Solidity: function MINIMUM_LIQUIDITY() view returns(uint256)
func (_Univ2Pair *Univ2PairSession) MINIMUMLIQUIDITY() (*big.Int, error) {
	return _Univ2Pair.Contract.MINIMUMLIQUIDITY(&_Univ2Pair.CallOpts)
}

// MINIMUMLIQUIDITY is a free data retrieval call binding the contract method 0xba9a7a56.
//
// Solidity: function MINIMUM_LIQUIDITY() view returns(uint256)
func (_Univ2Pair *Univ2PairCallerSession) MINIMUMLIQUIDITY() (*big.Int, error) {
	return _Univ2Pair.Contract.MINIMUMLIQUIDITY(&_Univ2Pair.CallOpts)
}

// SELECTOR is a free data retrieval call binding the contract method 0x053c238d.
//
// Solidity: function SELECTOR() view returns(bytes4)
func (_Univ2Pair *Univ2PairCaller) SELECTOR(opts *bind.CallOpts) ([4]byte, error) {
	var out []interface{}
	err := _Univ2Pair.contract.Call(opts, &out, "SELECTOR")

	if err != nil {
		return *new([4]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([4]byte)).(*[4]byte)

	return out0, err

}

// SELECTOR is a free data retrieval call binding the contract method 0x053c238d.
//
// Solidity: function SELECTOR() view returns(bytes4)
func (_Univ2Pair *Univ2PairSession) SELECTOR() ([4]byte, error) {
	return _Univ2Pair.Contract.SELECTOR(&_Univ2Pair.CallOpts)
}

// SELECTOR is a free data retrieval call binding the contract method 0x053c238d.
//
// Solidity: function SELECTOR() view returns(bytes4)
func (_Univ2Pair *Univ2PairCallerSession) SELECTOR() ([4]byte, error) {
	return _Univ2Pair.Contract.SELECTOR(&_Univ2Pair.CallOpts)
}

// Allowance is a free data retrieval call binding the contract method 0xdd62ed3e.
//
// Solidity: function allowance(address , address ) view returns(uint256)
func (_Univ2Pair *Univ2PairCaller) Allowance(opts *bind.CallOpts, arg0 common.Address, arg1 common.Address) (*big.Int, error) {
	var out []interface{}
	err := _Univ2Pair.contract.Call(opts, &out, "allowance", arg0, arg1)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// Allowance is a free data retrieval call binding the contract method 0xdd62ed3e.
//
// Solidity: function allowance(address , address ) view returns(uint256)
func (_Univ2Pair *Univ2PairSession) Allowance(arg0 common.Address, arg1 common.Address) (*big.Int, error) {
	return _Univ2Pair.Contract.Allowance(&_Univ2Pair.CallOpts, arg0, arg1)
}

// Allowance is a free data retrieval call binding the contract method 0xdd62ed3e.
//
// Solidity: function allowance(address , address ) view returns(uint256)
func (_Univ2Pair *Univ2PairCallerSession) Allowance(arg0 common.Address, arg1 common.Address) (*big.Int, error) {
	return _Univ2Pair.Contract.Allowance(&_Univ2Pair.CallOpts, arg0, arg1)
}

// BalanceOf is a free data retrieval call binding the contract method 0x70a08231.
//
// Solidity: function balanceOf(address ) view returns(uint256)
func (_Univ2Pair *Univ2PairCaller) BalanceOf(opts *bind.CallOpts, arg0 common.Address) (*big.Int, error) {
	var out []interface{}
	err := _Univ2Pair.contract.Call(opts, &out, "balanceOf", arg0)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// BalanceOf is a free data retrieval call binding the contract method 0x70a08231.
//
// Solidity: function balanceOf(address ) view returns(uint256)
func (_Univ2Pair *Univ2PairSession) BalanceOf(arg0 common.Address) (*big.Int, error) {
	return _Univ2Pair.Contract.BalanceOf(&_Univ2Pair.CallOpts, arg0)
}

// BalanceOf is a free data retrieval call binding the contract method 0x70a08231.
//
// Solidity: function balanceOf(address ) view returns(uint256)
func (_Univ2Pair *Univ2PairCallerSession) BalanceOf(arg0 common.Address) (*big.Int, error) {
	return _Univ2Pair.Contract.BalanceOf(&_Univ2Pair.CallOpts, arg0)
}

// BlockTimestampLast is a free data retrieval call binding the contract method 0xc5700a02.
//
// Solidity: function blockTimestampLast() view returns(uint32)
func (_Univ2Pair *Univ2PairCaller) BlockTimestampLast(opts *bind.CallOpts) (uint32, error) {
	var out []interface{}
	err := _Univ2Pair.contract.Call(opts, &out, "blockTimestampLast")

	if err != nil {
		return *new(uint32), err
	}

	out0 := *abi.ConvertType(out[0], new(uint32)).(*uint32)

	return out0, err

}

// BlockTimestampLast is a free data retrieval call binding the contract method 0xc5700a02.
//
// Solidity: function blockTimestampLast() view returns(uint32)
func (_Univ2Pair *Univ2PairSession) BlockTimestampLast() (uint32, error) {
	return _Univ2Pair.Contract.BlockTimestampLast(&_Univ2Pair.CallOpts)
}

// BlockTimestampLast is a free data retrieval call binding the contract method 0xc5700a02.
//
// Solidity: function blockTimestampLast() view returns(uint32)
func (_Univ2Pair *Univ2PairCallerSession) BlockTimestampLast() (uint32, error) {
	return _Univ2Pair.Contract.BlockTimestampLast(&_Univ2Pair.CallOpts)
}

// Decimals is a free data retrieval call binding the contract method 0x313ce567.
//
// Solidity: function decimals() view returns(uint8)
func (_Univ2Pair *Univ2PairCaller) Decimals(opts *bind.CallOpts) (uint8, error) {
	var out []interface{}
	err := _Univ2Pair.contract.Call(opts, &out, "decimals")

	if err != nil {
		return *new(uint8), err
	}

	out0 := *abi.ConvertType(out[0], new(uint8)).(*uint8)

	return out0, err

}

// Decimals is a free data retrieval call binding the contract method 0x313ce567.
//
// Solidity: function decimals() view returns(uint8)
func (_Univ2Pair *Univ2PairSession) Decimals() (uint8, error) {
	return _Univ2Pair.Contract.Decimals(&_Univ2Pair.CallOpts)
}

// Decimals is a free data retrieval call binding the contract method 0x313ce567.
//
// Solidity: function decimals() view returns(uint8)
func (_Univ2Pair *Univ2PairCallerSession) Decimals() (uint8, error) {
	return _Univ2Pair.Contract.Decimals(&_Univ2Pair.CallOpts)
}

// GetReserves is a free data retrieval call binding the contract method 0x0902f1ac.
//
// Solidity: function getReserves() view returns(uint112, uint112, uint32)
func (_Univ2Pair *Univ2PairCaller) GetReserves(opts *bind.CallOpts) (*big.Int, *big.Int, uint32, error) {
	var out []interface{}
	err := _Univ2Pair.contract.Call(opts, &out, "getReserves")

	if err != nil {
		return *new(*big.Int), *new(*big.Int), *new(uint32), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)
	out1 := *abi.ConvertType(out[1], new(*big.Int)).(**big.Int)
	out2 := *abi.ConvertType(out[2], new(uint32)).(*uint32)

	return out0, out1, out2, err

}

// GetReserves is a free data retrieval call binding the contract method 0x0902f1ac.
//
// Solidity: function getReserves() view returns(uint112, uint112, uint32)
func (_Univ2Pair *Univ2PairSession) GetReserves() (*big.Int, *big.Int, uint32, error) {
	return _Univ2Pair.Contract.GetReserves(&_Univ2Pair.CallOpts)
}

// GetReserves is a free data retrieval call binding the contract method 0x0902f1ac.
//
// Solidity: function getReserves() view returns(uint112, uint112, uint32)
func (_Univ2Pair *Univ2PairCallerSession) GetReserves() (*big.Int, *big.Int, uint32, error) {
	return _Univ2Pair.Contract.GetReserves(&_Univ2Pair.CallOpts)
}

// Name is a free data retrieval call binding the contract method 0x06fdde03.
//
// Solidity: function name() view returns(string)
func (_Univ2Pair *Univ2PairCaller) Name(opts *bind.CallOpts) (string, error) {
	var out []interface{}
	err := _Univ2Pair.contract.Call(opts, &out, "name")

	if err != nil {
		return *new(string), err
	}

	out0 := *abi.ConvertType(out[0], new(string)).(*string)

	return out0, err

}

// Name is a free data retrieval call binding the contract method 0x06fdde03.
//
// Solidity: function name() view returns(string)
func (_Univ2Pair *Univ2PairSession) Name() (string, error) {
	return _Univ2Pair.Contract.Name(&_Univ2Pair.CallOpts)
}

// Name is a free data retrieval call binding the contract method 0x06fdde03.
//
// Solidity: function name() view returns(string)
func (_Univ2Pair *Univ2PairCallerSession) Name() (string, error) {
	return _Univ2Pair.Contract.Name(&_Univ2Pair.CallOpts)
}

// Nonces is a free data retrieval call binding the contract method 0x7ecebe00.
//
// Solidity: function nonces(address ) view returns(uint256)
func (_Univ2Pair *Univ2PairCaller) Nonces(opts *bind.CallOpts, arg0 common.Address) (*big.Int, error) {
	var out []interface{}
	err := _Univ2Pair.contract.Call(opts, &out, "nonces", arg0)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// Nonces is a free data retrieval call binding the contract method 0x7ecebe00.
//
// Solidity: function nonces(address ) view returns(uint256)
func (_Univ2Pair *Univ2PairSession) Nonces(arg0 common.Address) (*big.Int, error) {
	return _Univ2Pair.Contract.Nonces(&_Univ2Pair.CallOpts, arg0)
}

// Nonces is a free data retrieval call binding the contract method 0x7ecebe00.
//
// Solidity: function nonces(address ) view returns(uint256)
func (_Univ2Pair *Univ2PairCallerSession) Nonces(arg0 common.Address) (*big.Int, error) {
	return _Univ2Pair.Contract.Nonces(&_Univ2Pair.CallOpts, arg0)
}

// Price0CumulativeLast is a free data retrieval call binding the contract method 0x5909c0d5.
//
// Solidity: function price0CumulativeLast() view returns(uint256)
func (_Univ2Pair *Univ2PairCaller) Price0CumulativeLast(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _Univ2Pair.contract.Call(opts, &out, "price0CumulativeLast")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// Price0CumulativeLast is a free data retrieval call binding the contract method 0x5909c0d5.
//
// Solidity: function price0CumulativeLast() view returns(uint256)
func (_Univ2Pair *Univ2PairSession) Price0CumulativeLast() (*big.Int, error) {
	return _Univ2Pair.Contract.Price0CumulativeLast(&_Univ2Pair.CallOpts)
}

// Price0CumulativeLast is a free data retrieval call binding the contract method 0x5909c0d5.
//
// Solidity: function price0CumulativeLast() view returns(uint256)
func (_Univ2Pair *Univ2PairCallerSession) Price0CumulativeLast() (*big.Int, error) {
	return _Univ2Pair.Contract.Price0CumulativeLast(&_Univ2Pair.CallOpts)
}

// Price1CumulativeLast is a free data retrieval call binding the contract method 0x5a3d5493.
//
// Solidity: function price1CumulativeLast() view returns(uint256)
func (_Univ2Pair *Univ2PairCaller) Price1CumulativeLast(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _Univ2Pair.contract.Call(opts, &out, "price1CumulativeLast")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// Price1CumulativeLast is a free data retrieval call binding the contract method 0x5a3d5493.
//
// Solidity: function price1CumulativeLast() view returns(uint256)
func (_Univ2Pair *Univ2PairSession) Price1CumulativeLast() (*big.Int, error) {
	return _Univ2Pair.Contract.Price1CumulativeLast(&_Univ2Pair.CallOpts)
}

// Price1CumulativeLast is a free data retrieval call binding the contract method 0x5a3d5493.
//
// Solidity: function price1CumulativeLast() view returns(uint256)
func (_Univ2Pair *Univ2PairCallerSession) Price1CumulativeLast() (*big.Int, error) {
	return _Univ2Pair.Contract.Price1CumulativeLast(&_Univ2Pair.CallOpts)
}

// Reserve0 is a free data retrieval call binding the contract method 0x443cb4bc.
//
// Solidity: function reserve0() view returns(uint112)
func (_Univ2Pair *Univ2PairCaller) Reserve0(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _Univ2Pair.contract.Call(opts, &out, "reserve0")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// Reserve0 is a free data retrieval call binding the contract method 0x443cb4bc.
//
// Solidity: function reserve0() view returns(uint112)
func (_Univ2Pair *Univ2PairSession) Reserve0() (*big.Int, error) {
	return _Univ2Pair.Contract.Reserve0(&_Univ2Pair.CallOpts)
}

// Reserve0 is a free data retrieval call binding the contract method 0x443cb4bc.
//
// Solidity: function reserve0() view returns(uint112)
func (_Univ2Pair *Univ2PairCallerSession) Reserve0() (*big.Int, error) {
	return _Univ2Pair.Contract.Reserve0(&_Univ2Pair.CallOpts)
}

// Reserve1 is a free data retrieval call binding the contract method 0x5a76f25e.
//
// Solidity: function reserve1() view returns(uint112)
func (_Univ2Pair *Univ2PairCaller) Reserve1(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _Univ2Pair.contract.Call(opts, &out, "reserve1")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// Reserve1 is a free data retrieval call binding the contract method 0x5a76f25e.
//
// Solidity: function reserve1() view returns(uint112)
func (_Univ2Pair *Univ2PairSession) Reserve1() (*big.Int, error) {
	return _Univ2Pair.Contract.Reserve1(&_Univ2Pair.CallOpts)
}

// Reserve1 is a free data retrieval call binding the contract method 0x5a76f25e.
//
// Solidity: function reserve1() view returns(uint112)
func (_Univ2Pair *Univ2PairCallerSession) Reserve1() (*big.Int, error) {
	return _Univ2Pair.Contract.Reserve1(&_Univ2Pair.CallOpts)
}

// Symbol is a free data retrieval call binding the contract method 0x95d89b41.
//
// Solidity: function symbol() view returns(string)
func (_Univ2Pair *Univ2PairCaller) Symbol(opts *bind.CallOpts) (string, error) {
	var out []interface{}
	err := _Univ2Pair.contract.Call(opts, &out, "symbol")

	if err != nil {
		return *new(string), err
	}

	out0 := *abi.ConvertType(out[0], new(string)).(*string)

	return out0, err

}

// Symbol is a free data retrieval call binding the contract method 0x95d89b41.
//
// Solidity: function symbol() view returns(string)
func (_Univ2Pair *Univ2PairSession) Symbol() (string, error) {
	return _Univ2Pair.Contract.Symbol(&_Univ2Pair.CallOpts)
}

// Symbol is a free data retrieval call binding the contract method 0x95d89b41.
//
// Solidity: function symbol() view returns(string)
func (_Univ2Pair *Univ2PairCallerSession) Symbol() (string, error) {
	return _Univ2Pair.Contract.Symbol(&_Univ2Pair.CallOpts)
}

// Token0 is a free data retrieval call binding the contract method 0x0dfe1681.
//
// Solidity: function token0() view returns(address)
func (_Univ2Pair *Univ2PairCaller) Token0(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _Univ2Pair.contract.Call(opts, &out, "token0")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Token0 is a free data retrieval call binding the contract method 0x0dfe1681.
//
// Solidity: function token0() view returns(address)
func (_Univ2Pair *Univ2PairSession) Token0() (common.Address, error) {
	return _Univ2Pair.Contract.Token0(&_Univ2Pair.CallOpts)
}

// Token0 is a free data retrieval call binding the contract method 0x0dfe1681.
//
// Solidity: function token0() view returns(address)
func (_Univ2Pair *Univ2PairCallerSession) Token0() (common.Address, error) {
	return _Univ2Pair.Contract.Token0(&_Univ2Pair.CallOpts)
}

// Token1 is a free data retrieval call binding the contract method 0xd21220a7.
//
// Solidity: function token1() view returns(address)
func (_Univ2Pair *Univ2PairCaller) Token1(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _Univ2Pair.contract.Call(opts, &out, "token1")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Token1 is a free data retrieval call binding the contract method 0xd21220a7.
//
// Solidity: function token1() view returns(address)
func (_Univ2Pair *Univ2PairSession) Token1() (common.Address, error) {
	return _Univ2Pair.Contract.Token1(&_Univ2Pair.CallOpts)
}

// Token1 is a free data retrieval call binding the contract method 0xd21220a7.
//
// Solidity: function token1() view returns(address)
func (_Univ2Pair *Univ2PairCallerSession) Token1() (common.Address, error) {
	return _Univ2Pair.Contract.Token1(&_Univ2Pair.CallOpts)
}

// TotalSupply is a free data retrieval call binding the contract method 0x18160ddd.
//
// Solidity: function totalSupply() view returns(uint256)
func (_Univ2Pair *Univ2PairCaller) TotalSupply(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _Univ2Pair.contract.Call(opts, &out, "totalSupply")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// TotalSupply is a free data retrieval call binding the contract method 0x18160ddd.
//
// Solidity: function totalSupply() view returns(uint256)
func (_Univ2Pair *Univ2PairSession) TotalSupply() (*big.Int, error) {
	return _Univ2Pair.Contract.TotalSupply(&_Univ2Pair.CallOpts)
}

// TotalSupply is a free data retrieval call binding the contract method 0x18160ddd.
//
// Solidity: function totalSupply() view returns(uint256)
func (_Univ2Pair *Univ2PairCallerSession) TotalSupply() (*big.Int, error) {
	return _Univ2Pair.Contract.TotalSupply(&_Univ2Pair.CallOpts)
}

// Approve is a paid mutator transaction binding the contract method 0x095ea7b3.
//
// Solidity: function approve(address spender, uint256 amount) returns(bool)
func (_Univ2Pair *Univ2PairTransactor) Approve(opts *bind.TransactOpts, spender common.Address, amount *big.Int) (*types.Transaction, error) {
	return _Univ2Pair.contract.Transact(opts, "approve", spender, amount)
}

// Approve is a paid mutator transaction binding the contract method 0x095ea7b3.
//
// Solidity: function approve(address spender, uint256 amount) returns(bool)
func (_Univ2Pair *Univ2PairSession) Approve(spender common.Address, amount *big.Int) (*types.Transaction, error) {
	return _Univ2Pair.Contract.Approve(&_Univ2Pair.TransactOpts, spender, amount)
}

// Approve is a paid mutator transaction binding the contract method 0x095ea7b3.
//
// Solidity: function approve(address spender, uint256 amount) returns(bool)
func (_Univ2Pair *Univ2PairTransactorSession) Approve(spender common.Address, amount *big.Int) (*types.Transaction, error) {
	return _Univ2Pair.Contract.Approve(&_Univ2Pair.TransactOpts, spender, amount)
}

// Burn is a paid mutator transaction binding the contract method 0x89afcb44.
//
// Solidity: function burn(address to) returns(uint256 amount0, uint256 amount1)
func (_Univ2Pair *Univ2PairTransactor) Burn(opts *bind.TransactOpts, to common.Address) (*types.Transaction, error) {
	return _Univ2Pair.contract.Transact(opts, "burn", to)
}

// Burn is a paid mutator transaction binding the contract method 0x89afcb44.
//
// Solidity: function burn(address to) returns(uint256 amount0, uint256 amount1)
func (_Univ2Pair *Univ2PairSession) Burn(to common.Address) (*types.Transaction, error) {
	return _Univ2Pair.Contract.Burn(&_Univ2Pair.TransactOpts, to)
}

// Burn is a paid mutator transaction binding the contract method 0x89afcb44.
//
// Solidity: function burn(address to) returns(uint256 amount0, uint256 amount1)
func (_Univ2Pair *Univ2PairTransactorSession) Burn(to common.Address) (*types.Transaction, error) {
	return _Univ2Pair.Contract.Burn(&_Univ2Pair.TransactOpts, to)
}

// Initialize is a paid mutator transaction binding the contract method 0x485cc955.
//
// Solidity: function initialize(address _token0, address _token1) returns()
func (_Univ2Pair *Univ2PairTransactor) Initialize(opts *bind.TransactOpts, _token0 common.Address, _token1 common.Address) (*types.Transaction, error) {
	return _Univ2Pair.contract.Transact(opts, "initialize", _token0, _token1)
}

// Initialize is a paid mutator transaction binding the contract method 0x485cc955.
//
// Solidity: function initialize(address _token0, address _token1) returns()
func (_Univ2Pair *Univ2PairSession) Initialize(_token0 common.Address, _token1 common.Address) (*types.Transaction, error) {
	return _Univ2Pair.Contract.Initialize(&_Univ2Pair.TransactOpts, _token0, _token1)
}

// Initialize is a paid mutator transaction binding the contract method 0x485cc955.
//
// Solidity: function initialize(address _token0, address _token1) returns()
func (_Univ2Pair *Univ2PairTransactorSession) Initialize(_token0 common.Address, _token1 common.Address) (*types.Transaction, error) {
	return _Univ2Pair.Contract.Initialize(&_Univ2Pair.TransactOpts, _token0, _token1)
}

// Mint is a paid mutator transaction binding the contract method 0x6a627842.
//
// Solidity: function mint(address to) returns(uint256 liquidity)
func (_Univ2Pair *Univ2PairTransactor) Mint(opts *bind.TransactOpts, to common.Address) (*types.Transaction, error) {
	return _Univ2Pair.contract.Transact(opts, "mint", to)
}

// Mint is a paid mutator transaction binding the contract method 0x6a627842.
//
// Solidity: function mint(address to) returns(uint256 liquidity)
func (_Univ2Pair *Univ2PairSession) Mint(to common.Address) (*types.Transaction, error) {
	return _Univ2Pair.Contract.Mint(&_Univ2Pair.TransactOpts, to)
}

// Mint is a paid mutator transaction binding the contract method 0x6a627842.
//
// Solidity: function mint(address to) returns(uint256 liquidity)
func (_Univ2Pair *Univ2PairTransactorSession) Mint(to common.Address) (*types.Transaction, error) {
	return _Univ2Pair.Contract.Mint(&_Univ2Pair.TransactOpts, to)
}

// Permit is a paid mutator transaction binding the contract method 0xd505accf.
//
// Solidity: function permit(address owner, address spender, uint256 value, uint256 deadline, uint8 v, bytes32 r, bytes32 s) returns()
func (_Univ2Pair *Univ2PairTransactor) Permit(opts *bind.TransactOpts, owner common.Address, spender common.Address, value *big.Int, deadline *big.Int, v uint8, r [32]byte, s [32]byte) (*types.Transaction, error) {
	return _Univ2Pair.contract.Transact(opts, "permit", owner, spender, value, deadline, v, r, s)
}

// Permit is a paid mutator transaction binding the contract method 0xd505accf.
//
// Solidity: function permit(address owner, address spender, uint256 value, uint256 deadline, uint8 v, bytes32 r, bytes32 s) returns()
func (_Univ2Pair *Univ2PairSession) Permit(owner common.Address, spender common.Address, value *big.Int, deadline *big.Int, v uint8, r [32]byte, s [32]byte) (*types.Transaction, error) {
	return _Univ2Pair.Contract.Permit(&_Univ2Pair.TransactOpts, owner, spender, value, deadline, v, r, s)
}

// Permit is a paid mutator transaction binding the contract method 0xd505accf.
//
// Solidity: function permit(address owner, address spender, uint256 value, uint256 deadline, uint8 v, bytes32 r, bytes32 s) returns()
func (_Univ2Pair *Univ2PairTransactorSession) Permit(owner common.Address, spender common.Address, value *big.Int, deadline *big.Int, v uint8, r [32]byte, s [32]byte) (*types.Transaction, error) {
	return _Univ2Pair.Contract.Permit(&_Univ2Pair.TransactOpts, owner, spender, value, deadline, v, r, s)
}

// Swap is a paid mutator transaction binding the contract method 0x6d9a640a.
//
// Solidity: function swap(uint256 amount0Out, uint256 amount1Out, address to) returns()
func (_Univ2Pair *Univ2PairTransactor) Swap(opts *bind.TransactOpts, amount0Out *big.Int, amount1Out *big.Int, to common.Address) (*types.Transaction, error) {
	return _Univ2Pair.contract.Transact(opts, "swap", amount0Out, amount1Out, to)
}

// Swap is a paid mutator transaction binding the contract method 0x6d9a640a.
//
// Solidity: function swap(uint256 amount0Out, uint256 amount1Out, address to) returns()
func (_Univ2Pair *Univ2PairSession) Swap(amount0Out *big.Int, amount1Out *big.Int, to common.Address) (*types.Transaction, error) {
	return _Univ2Pair.Contract.Swap(&_Univ2Pair.TransactOpts, amount0Out, amount1Out, to)
}

// Swap is a paid mutator transaction binding the contract method 0x6d9a640a.
//
// Solidity: function swap(uint256 amount0Out, uint256 amount1Out, address to) returns()
func (_Univ2Pair *Univ2PairTransactorSession) Swap(amount0Out *big.Int, amount1Out *big.Int, to common.Address) (*types.Transaction, error) {
	return _Univ2Pair.Contract.Swap(&_Univ2Pair.TransactOpts, amount0Out, amount1Out, to)
}

// Sync is a paid mutator transaction binding the contract method 0xfff6cae9.
//
// Solidity: function sync() returns()
func (_Univ2Pair *Univ2PairTransactor) Sync(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Univ2Pair.contract.Transact(opts, "sync")
}

// Sync is a paid mutator transaction binding the contract method 0xfff6cae9.
//
// Solidity: function sync() returns()
func (_Univ2Pair *Univ2PairSession) Sync() (*types.Transaction, error) {
	return _Univ2Pair.Contract.Sync(&_Univ2Pair.TransactOpts)
}

// Sync is a paid mutator transaction binding the contract method 0xfff6cae9.
//
// Solidity: function sync() returns()
func (_Univ2Pair *Univ2PairTransactorSession) Sync() (*types.Transaction, error) {
	return _Univ2Pair.Contract.Sync(&_Univ2Pair.TransactOpts)
}

// Transfer is a paid mutator transaction binding the contract method 0xa9059cbb.
//
// Solidity: function transfer(address to, uint256 amount) returns(bool)
func (_Univ2Pair *Univ2PairTransactor) Transfer(opts *bind.TransactOpts, to common.Address, amount *big.Int) (*types.Transaction, error) {
	return _Univ2Pair.contract.Transact(opts, "transfer", to, amount)
}

// Transfer is a paid mutator transaction binding the contract method 0xa9059cbb.
//
// Solidity: function transfer(address to, uint256 amount) returns(bool)
func (_Univ2Pair *Univ2PairSession) Transfer(to common.Address, amount *big.Int) (*types.Transaction, error) {
	return _Univ2Pair.Contract.Transfer(&_Univ2Pair.TransactOpts, to, amount)
}

// Transfer is a paid mutator transaction binding the contract method 0xa9059cbb.
//
// Solidity: function transfer(address to, uint256 amount) returns(bool)
func (_Univ2Pair *Univ2PairTransactorSession) Transfer(to common.Address, amount *big.Int) (*types.Transaction, error) {
	return _Univ2Pair.Contract.Transfer(&_Univ2Pair.TransactOpts, to, amount)
}

// TransferFrom is a paid mutator transaction binding the contract method 0x23b872dd.
//
// Solidity: function transferFrom(address from, address to, uint256 amount) returns(bool)
func (_Univ2Pair *Univ2PairTransactor) TransferFrom(opts *bind.TransactOpts, from common.Address, to common.Address, amount *big.Int) (*types.Transaction, error) {
	return _Univ2Pair.contract.Transact(opts, "transferFrom", from, to, amount)
}

// TransferFrom is a paid mutator transaction binding the contract method 0x23b872dd.
//
// Solidity: function transferFrom(address from, address to, uint256 amount) returns(bool)
func (_Univ2Pair *Univ2PairSession) TransferFrom(from common.Address, to common.Address, amount *big.Int) (*types.Transaction, error) {
	return _Univ2Pair.Contract.TransferFrom(&_Univ2Pair.TransactOpts, from, to, amount)
}

// TransferFrom is a paid mutator transaction binding the contract method 0x23b872dd.
//
// Solidity: function transferFrom(address from, address to, uint256 amount) returns(bool)
func (_Univ2Pair *Univ2PairTransactorSession) TransferFrom(from common.Address, to common.Address, amount *big.Int) (*types.Transaction, error) {
	return _Univ2Pair.Contract.TransferFrom(&_Univ2Pair.TransactOpts, from, to, amount)
}

// Univ2PairApprovalIterator is returned from FilterApproval and is used to iterate over the raw logs and unpacked data for Approval events raised by the Univ2Pair contract.
type Univ2PairApprovalIterator struct {
	Event *Univ2PairApproval // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *Univ2PairApprovalIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(Univ2PairApproval)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(Univ2PairApproval)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *Univ2PairApprovalIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *Univ2PairApprovalIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// Univ2PairApproval represents a Approval event raised by the Univ2Pair contract.
type Univ2PairApproval struct {
	Owner   common.Address
	Spender common.Address
	Amount  *big.Int
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterApproval is a free log retrieval operation binding the contract event 0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925.
//
// Solidity: event Approval(address indexed owner, address indexed spender, uint256 amount)
func (_Univ2Pair *Univ2PairFilterer) FilterApproval(opts *bind.FilterOpts, owner []common.Address, spender []common.Address) (*Univ2PairApprovalIterator, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}
	var spenderRule []interface{}
	for _, spenderItem := range spender {
		spenderRule = append(spenderRule, spenderItem)
	}

	logs, sub, err := _Univ2Pair.contract.FilterLogs(opts, "Approval", ownerRule, spenderRule)
	if err != nil {
		return nil, err
	}
	return &Univ2PairApprovalIterator{contract: _Univ2Pair.contract, event: "Approval", logs: logs, sub: sub}, nil
}

// WatchApproval is a free log subscription operation binding the contract event 0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925.
//
// Solidity: event Approval(address indexed owner, address indexed spender, uint256 amount)
func (_Univ2Pair *Univ2PairFilterer) WatchApproval(opts *bind.WatchOpts, sink chan<- *Univ2PairApproval, owner []common.Address, spender []common.Address) (event.Subscription, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}
	var spenderRule []interface{}
	for _, spenderItem := range spender {
		spenderRule = append(spenderRule, spenderItem)
	}

	logs, sub, err := _Univ2Pair.contract.WatchLogs(opts, "Approval", ownerRule, spenderRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(Univ2PairApproval)
				if err := _Univ2Pair.contract.UnpackLog(event, "Approval", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseApproval is a log parse operation binding the contract event 0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925.
//
// Solidity: event Approval(address indexed owner, address indexed spender, uint256 amount)
func (_Univ2Pair *Univ2PairFilterer) ParseApproval(log types.Log) (*Univ2PairApproval, error) {
	event := new(Univ2PairApproval)
	if err := _Univ2Pair.contract.UnpackLog(event, "Approval", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// Univ2PairBurnIterator is returned from FilterBurn and is used to iterate over the raw logs and unpacked data for Burn events raised by the Univ2Pair contract.
type Univ2PairBurnIterator struct {
	Event *Univ2PairBurn // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *Univ2PairBurnIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(Univ2PairBurn)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(Univ2PairBurn)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *Univ2PairBurnIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *Univ2PairBurnIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// Univ2PairBurn represents a Burn event raised by the Univ2Pair contract.
type Univ2PairBurn struct {
	Operator common.Address
	Value    *big.Int
	Raw      types.Log // Blockchain specific contextual infos
}

// FilterBurn is a free log retrieval operation binding the contract event 0xcc16f5dbb4873280815c1ee09dbd06736cffcc184412cf7a71a0fdb75d397ca5.
//
// Solidity: event Burn(address indexed _operator, uint256 _value)
func (_Univ2Pair *Univ2PairFilterer) FilterBurn(opts *bind.FilterOpts, _operator []common.Address) (*Univ2PairBurnIterator, error) {

	var _operatorRule []interface{}
	for _, _operatorItem := range _operator {
		_operatorRule = append(_operatorRule, _operatorItem)
	}

	logs, sub, err := _Univ2Pair.contract.FilterLogs(opts, "Burn", _operatorRule)
	if err != nil {
		return nil, err
	}
	return &Univ2PairBurnIterator{contract: _Univ2Pair.contract, event: "Burn", logs: logs, sub: sub}, nil
}

// WatchBurn is a free log subscription operation binding the contract event 0xcc16f5dbb4873280815c1ee09dbd06736cffcc184412cf7a71a0fdb75d397ca5.
//
// Solidity: event Burn(address indexed _operator, uint256 _value)
func (_Univ2Pair *Univ2PairFilterer) WatchBurn(opts *bind.WatchOpts, sink chan<- *Univ2PairBurn, _operator []common.Address) (event.Subscription, error) {

	var _operatorRule []interface{}
	for _, _operatorItem := range _operator {
		_operatorRule = append(_operatorRule, _operatorItem)
	}

	logs, sub, err := _Univ2Pair.contract.WatchLogs(opts, "Burn", _operatorRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(Univ2PairBurn)
				if err := _Univ2Pair.contract.UnpackLog(event, "Burn", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseBurn is a log parse operation binding the contract event 0xcc16f5dbb4873280815c1ee09dbd06736cffcc184412cf7a71a0fdb75d397ca5.
//
// Solidity: event Burn(address indexed _operator, uint256 _value)
func (_Univ2Pair *Univ2PairFilterer) ParseBurn(log types.Log) (*Univ2PairBurn, error) {
	event := new(Univ2PairBurn)
	if err := _Univ2Pair.contract.UnpackLog(event, "Burn", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// Univ2PairInitializedIterator is returned from FilterInitialized and is used to iterate over the raw logs and unpacked data for Initialized events raised by the Univ2Pair contract.
type Univ2PairInitializedIterator struct {
	Event *Univ2PairInitialized // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *Univ2PairInitializedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(Univ2PairInitialized)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(Univ2PairInitialized)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *Univ2PairInitializedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *Univ2PairInitializedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// Univ2PairInitialized represents a Initialized event raised by the Univ2Pair contract.
type Univ2PairInitialized struct {
	Version uint64
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterInitialized is a free log retrieval operation binding the contract event 0xc7f505b2f371ae2175ee4913f4499e1f2633a7b5936321eed1cdaeb6115181d2.
//
// Solidity: event Initialized(uint64 version)
func (_Univ2Pair *Univ2PairFilterer) FilterInitialized(opts *bind.FilterOpts) (*Univ2PairInitializedIterator, error) {

	logs, sub, err := _Univ2Pair.contract.FilterLogs(opts, "Initialized")
	if err != nil {
		return nil, err
	}
	return &Univ2PairInitializedIterator{contract: _Univ2Pair.contract, event: "Initialized", logs: logs, sub: sub}, nil
}

// WatchInitialized is a free log subscription operation binding the contract event 0xc7f505b2f371ae2175ee4913f4499e1f2633a7b5936321eed1cdaeb6115181d2.
//
// Solidity: event Initialized(uint64 version)
func (_Univ2Pair *Univ2PairFilterer) WatchInitialized(opts *bind.WatchOpts, sink chan<- *Univ2PairInitialized) (event.Subscription, error) {

	logs, sub, err := _Univ2Pair.contract.WatchLogs(opts, "Initialized")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(Univ2PairInitialized)
				if err := _Univ2Pair.contract.UnpackLog(event, "Initialized", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseInitialized is a log parse operation binding the contract event 0xc7f505b2f371ae2175ee4913f4499e1f2633a7b5936321eed1cdaeb6115181d2.
//
// Solidity: event Initialized(uint64 version)
func (_Univ2Pair *Univ2PairFilterer) ParseInitialized(log types.Log) (*Univ2PairInitialized, error) {
	event := new(Univ2PairInitialized)
	if err := _Univ2Pair.contract.UnpackLog(event, "Initialized", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// Univ2PairMintIterator is returned from FilterMint and is used to iterate over the raw logs and unpacked data for Mint events raised by the Univ2Pair contract.
type Univ2PairMintIterator struct {
	Event *Univ2PairMint // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *Univ2PairMintIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(Univ2PairMint)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(Univ2PairMint)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *Univ2PairMintIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *Univ2PairMintIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// Univ2PairMint represents a Mint event raised by the Univ2Pair contract.
type Univ2PairMint struct {
	Operator common.Address
	Value    *big.Int
	Raw      types.Log // Blockchain specific contextual infos
}

// FilterMint is a free log retrieval operation binding the contract event 0x0f6798a560793a54c3bcfe86a93cde1e73087d944c0ea20544137d4121396885.
//
// Solidity: event Mint(address indexed _operator, uint256 _value)
func (_Univ2Pair *Univ2PairFilterer) FilterMint(opts *bind.FilterOpts, _operator []common.Address) (*Univ2PairMintIterator, error) {

	var _operatorRule []interface{}
	for _, _operatorItem := range _operator {
		_operatorRule = append(_operatorRule, _operatorItem)
	}

	logs, sub, err := _Univ2Pair.contract.FilterLogs(opts, "Mint", _operatorRule)
	if err != nil {
		return nil, err
	}
	return &Univ2PairMintIterator{contract: _Univ2Pair.contract, event: "Mint", logs: logs, sub: sub}, nil
}

// WatchMint is a free log subscription operation binding the contract event 0x0f6798a560793a54c3bcfe86a93cde1e73087d944c0ea20544137d4121396885.
//
// Solidity: event Mint(address indexed _operator, uint256 _value)
func (_Univ2Pair *Univ2PairFilterer) WatchMint(opts *bind.WatchOpts, sink chan<- *Univ2PairMint, _operator []common.Address) (event.Subscription, error) {

	var _operatorRule []interface{}
	for _, _operatorItem := range _operator {
		_operatorRule = append(_operatorRule, _operatorItem)
	}

	logs, sub, err := _Univ2Pair.contract.WatchLogs(opts, "Mint", _operatorRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(Univ2PairMint)
				if err := _Univ2Pair.contract.UnpackLog(event, "Mint", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseMint is a log parse operation binding the contract event 0x0f6798a560793a54c3bcfe86a93cde1e73087d944c0ea20544137d4121396885.
//
// Solidity: event Mint(address indexed _operator, uint256 _value)
func (_Univ2Pair *Univ2PairFilterer) ParseMint(log types.Log) (*Univ2PairMint, error) {
	event := new(Univ2PairMint)
	if err := _Univ2Pair.contract.UnpackLog(event, "Mint", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// Univ2PairSwapIterator is returned from FilterSwap and is used to iterate over the raw logs and unpacked data for Swap events raised by the Univ2Pair contract.
type Univ2PairSwapIterator struct {
	Event *Univ2PairSwap // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *Univ2PairSwapIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(Univ2PairSwap)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(Univ2PairSwap)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *Univ2PairSwapIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *Univ2PairSwapIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// Univ2PairSwap represents a Swap event raised by the Univ2Pair contract.
type Univ2PairSwap struct {
	Sender     common.Address
	Amount0Out *big.Int
	Amount1Out *big.Int
	To         common.Address
	Raw        types.Log // Blockchain specific contextual infos
}

// FilterSwap is a free log retrieval operation binding the contract event 0x2a9237ff5aa599ef4c5ee4b1142b53429d5755e2685fe6288b2e3320202115f5.
//
// Solidity: event Swap(address indexed sender, uint256 amount0Out, uint256 amount1Out, address indexed to)
func (_Univ2Pair *Univ2PairFilterer) FilterSwap(opts *bind.FilterOpts, sender []common.Address, to []common.Address) (*Univ2PairSwapIterator, error) {

	var senderRule []interface{}
	for _, senderItem := range sender {
		senderRule = append(senderRule, senderItem)
	}

	var toRule []interface{}
	for _, toItem := range to {
		toRule = append(toRule, toItem)
	}

	logs, sub, err := _Univ2Pair.contract.FilterLogs(opts, "Swap", senderRule, toRule)
	if err != nil {
		return nil, err
	}
	return &Univ2PairSwapIterator{contract: _Univ2Pair.contract, event: "Swap", logs: logs, sub: sub}, nil
}

// WatchSwap is a free log subscription operation binding the contract event 0x2a9237ff5aa599ef4c5ee4b1142b53429d5755e2685fe6288b2e3320202115f5.
//
// Solidity: event Swap(address indexed sender, uint256 amount0Out, uint256 amount1Out, address indexed to)
func (_Univ2Pair *Univ2PairFilterer) WatchSwap(opts *bind.WatchOpts, sink chan<- *Univ2PairSwap, sender []common.Address, to []common.Address) (event.Subscription, error) {

	var senderRule []interface{}
	for _, senderItem := range sender {
		senderRule = append(senderRule, senderItem)
	}

	var toRule []interface{}
	for _, toItem := range to {
		toRule = append(toRule, toItem)
	}

	logs, sub, err := _Univ2Pair.contract.WatchLogs(opts, "Swap", senderRule, toRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(Univ2PairSwap)
				if err := _Univ2Pair.contract.UnpackLog(event, "Swap", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseSwap is a log parse operation binding the contract event 0x2a9237ff5aa599ef4c5ee4b1142b53429d5755e2685fe6288b2e3320202115f5.
//
// Solidity: event Swap(address indexed sender, uint256 amount0Out, uint256 amount1Out, address indexed to)
func (_Univ2Pair *Univ2PairFilterer) ParseSwap(log types.Log) (*Univ2PairSwap, error) {
	event := new(Univ2PairSwap)
	if err := _Univ2Pair.contract.UnpackLog(event, "Swap", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// Univ2PairSyncIterator is returned from FilterSync and is used to iterate over the raw logs and unpacked data for Sync events raised by the Univ2Pair contract.
type Univ2PairSyncIterator struct {
	Event *Univ2PairSync // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *Univ2PairSyncIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(Univ2PairSync)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(Univ2PairSync)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *Univ2PairSyncIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *Univ2PairSyncIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// Univ2PairSync represents a Sync event raised by the Univ2Pair contract.
type Univ2PairSync struct {
	Reserve0 *big.Int
	Reserve1 *big.Int
	Raw      types.Log // Blockchain specific contextual infos
}

// FilterSync is a free log retrieval operation binding the contract event 0x1c411e9a96e071241c2f21f7726b17ae89e3cab4c78be50e062b03a9fffbbad1.
//
// Solidity: event Sync(uint112 reserve0, uint112 reserve1)
func (_Univ2Pair *Univ2PairFilterer) FilterSync(opts *bind.FilterOpts) (*Univ2PairSyncIterator, error) {

	logs, sub, err := _Univ2Pair.contract.FilterLogs(opts, "Sync")
	if err != nil {
		return nil, err
	}
	return &Univ2PairSyncIterator{contract: _Univ2Pair.contract, event: "Sync", logs: logs, sub: sub}, nil
}

// WatchSync is a free log subscription operation binding the contract event 0x1c411e9a96e071241c2f21f7726b17ae89e3cab4c78be50e062b03a9fffbbad1.
//
// Solidity: event Sync(uint112 reserve0, uint112 reserve1)
func (_Univ2Pair *Univ2PairFilterer) WatchSync(opts *bind.WatchOpts, sink chan<- *Univ2PairSync) (event.Subscription, error) {

	logs, sub, err := _Univ2Pair.contract.WatchLogs(opts, "Sync")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(Univ2PairSync)
				if err := _Univ2Pair.contract.UnpackLog(event, "Sync", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseSync is a log parse operation binding the contract event 0x1c411e9a96e071241c2f21f7726b17ae89e3cab4c78be50e062b03a9fffbbad1.
//
// Solidity: event Sync(uint112 reserve0, uint112 reserve1)
func (_Univ2Pair *Univ2PairFilterer) ParseSync(log types.Log) (*Univ2PairSync, error) {
	event := new(Univ2PairSync)
	if err := _Univ2Pair.contract.UnpackLog(event, "Sync", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// Univ2PairTransferIterator is returned from FilterTransfer and is used to iterate over the raw logs and unpacked data for Transfer events raised by the Univ2Pair contract.
type Univ2PairTransferIterator struct {
	Event *Univ2PairTransfer // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *Univ2PairTransferIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(Univ2PairTransfer)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(Univ2PairTransfer)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *Univ2PairTransferIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *Univ2PairTransferIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// Univ2PairTransfer represents a Transfer event raised by the Univ2Pair contract.
type Univ2PairTransfer struct {
	From   common.Address
	To     common.Address
	Amount *big.Int
	Raw    types.Log // Blockchain specific contextual infos
}

// FilterTransfer is a free log retrieval operation binding the contract event 0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef.
//
// Solidity: event Transfer(address indexed from, address indexed to, uint256 amount)
func (_Univ2Pair *Univ2PairFilterer) FilterTransfer(opts *bind.FilterOpts, from []common.Address, to []common.Address) (*Univ2PairTransferIterator, error) {

	var fromRule []interface{}
	for _, fromItem := range from {
		fromRule = append(fromRule, fromItem)
	}
	var toRule []interface{}
	for _, toItem := range to {
		toRule = append(toRule, toItem)
	}

	logs, sub, err := _Univ2Pair.contract.FilterLogs(opts, "Transfer", fromRule, toRule)
	if err != nil {
		return nil, err
	}
	return &Univ2PairTransferIterator{contract: _Univ2Pair.contract, event: "Transfer", logs: logs, sub: sub}, nil
}

// WatchTransfer is a free log subscription operation binding the contract event 0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef.
//
// Solidity: event Transfer(address indexed from, address indexed to, uint256 amount)
func (_Univ2Pair *Univ2PairFilterer) WatchTransfer(opts *bind.WatchOpts, sink chan<- *Univ2PairTransfer, from []common.Address, to []common.Address) (event.Subscription, error) {

	var fromRule []interface{}
	for _, fromItem := range from {
		fromRule = append(fromRule, fromItem)
	}
	var toRule []interface{}
	for _, toItem := range to {
		toRule = append(toRule, toItem)
	}

	logs, sub, err := _Univ2Pair.contract.WatchLogs(opts, "Transfer", fromRule, toRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(Univ2PairTransfer)
				if err := _Univ2Pair.contract.UnpackLog(event, "Transfer", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseTransfer is a log parse operation binding the contract event 0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef.
//
// Solidity: event Transfer(address indexed from, address indexed to, uint256 amount)
func (_Univ2Pair *Univ2PairFilterer) ParseTransfer(log types.Log) (*Univ2PairTransfer, error) {
	event := new(Univ2PairTransfer)
	if err := _Univ2Pair.contract.UnpackLog(event, "Transfer", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

