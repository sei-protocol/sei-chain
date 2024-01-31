// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package cw20

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

// Cw20MetaData contains all meta data concerning the Cw20 contract.
var Cw20MetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"string\",\"name\":\"Cw20Address_\",\"type\":\"string\"},{\"internalType\":\"string\",\"name\":\"name_\",\"type\":\"string\"},{\"internalType\":\"string\",\"name\":\"symbol_\",\"type\":\"string\"}],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"spender\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"allowance\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"needed\",\"type\":\"uint256\"}],\"name\":\"ERC20InsufficientAllowance\",\"type\":\"error\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"balance\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"needed\",\"type\":\"uint256\"}],\"name\":\"ERC20InsufficientBalance\",\"type\":\"error\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"approver\",\"type\":\"address\"}],\"name\":\"ERC20InvalidApprover\",\"type\":\"error\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"receiver\",\"type\":\"address\"}],\"name\":\"ERC20InvalidReceiver\",\"type\":\"error\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"}],\"name\":\"ERC20InvalidSender\",\"type\":\"error\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"spender\",\"type\":\"address\"}],\"name\":\"ERC20InvalidSpender\",\"type\":\"error\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"spender\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"Approval\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"from\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"to\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"Transfer\",\"type\":\"event\"},{\"inputs\":[],\"name\":\"AddrPrecompile\",\"outputs\":[{\"internalType\":\"contractIAddr\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"Cw20Address\",\"outputs\":[{\"internalType\":\"string\",\"name\":\"\",\"type\":\"string\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"JsonPrecompile\",\"outputs\":[{\"internalType\":\"contractIJson\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"WasmdPrecompile\",\"outputs\":[{\"internalType\":\"contractIWasmd\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"spender\",\"type\":\"address\"}],\"name\":\"allowance\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"spender\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"approve\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"}],\"name\":\"balanceOf\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"decimals\",\"outputs\":[{\"internalType\":\"uint8\",\"name\":\"\",\"type\":\"uint8\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"name\",\"outputs\":[{\"internalType\":\"string\",\"name\":\"\",\"type\":\"string\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"symbol\",\"outputs\":[{\"internalType\":\"string\",\"name\":\"\",\"type\":\"string\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"totalSupply\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"to\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"transfer\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"from\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"to\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"transferFrom\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
}

// Cw20ABI is the input ABI used to generate the binding from.
// Deprecated: Use Cw20MetaData.ABI instead.
var Cw20ABI = Cw20MetaData.ABI

// Cw20 is an auto generated Go binding around an Ethereum contract.
type Cw20 struct {
	Cw20Caller     // Read-only binding to the contract
	Cw20Transactor // Write-only binding to the contract
	Cw20Filterer   // Log filterer for contract events
}

// Cw20Caller is an auto generated read-only Go binding around an Ethereum contract.
type Cw20Caller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// Cw20Transactor is an auto generated write-only Go binding around an Ethereum contract.
type Cw20Transactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// Cw20Filterer is an auto generated log filtering Go binding around an Ethereum contract events.
type Cw20Filterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// Cw20Session is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type Cw20Session struct {
	Contract     *Cw20             // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// Cw20CallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type Cw20CallerSession struct {
	Contract *Cw20Caller   // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts // Call options to use throughout this session
}

// Cw20TransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type Cw20TransactorSession struct {
	Contract     *Cw20Transactor   // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// Cw20Raw is an auto generated low-level Go binding around an Ethereum contract.
type Cw20Raw struct {
	Contract *Cw20 // Generic contract binding to access the raw methods on
}

// Cw20CallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type Cw20CallerRaw struct {
	Contract *Cw20Caller // Generic read-only contract binding to access the raw methods on
}

// Cw20TransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type Cw20TransactorRaw struct {
	Contract *Cw20Transactor // Generic write-only contract binding to access the raw methods on
}

// NewCw20 creates a new instance of Cw20, bound to a specific deployed contract.
func NewCw20(address common.Address, backend bind.ContractBackend) (*Cw20, error) {
	contract, err := bindCw20(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &Cw20{Cw20Caller: Cw20Caller{contract: contract}, Cw20Transactor: Cw20Transactor{contract: contract}, Cw20Filterer: Cw20Filterer{contract: contract}}, nil
}

// NewCw20Caller creates a new read-only instance of Cw20, bound to a specific deployed contract.
func NewCw20Caller(address common.Address, caller bind.ContractCaller) (*Cw20Caller, error) {
	contract, err := bindCw20(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &Cw20Caller{contract: contract}, nil
}

// NewCw20Transactor creates a new write-only instance of Cw20, bound to a specific deployed contract.
func NewCw20Transactor(address common.Address, transactor bind.ContractTransactor) (*Cw20Transactor, error) {
	contract, err := bindCw20(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &Cw20Transactor{contract: contract}, nil
}

// NewCw20Filterer creates a new log filterer instance of Cw20, bound to a specific deployed contract.
func NewCw20Filterer(address common.Address, filterer bind.ContractFilterer) (*Cw20Filterer, error) {
	contract, err := bindCw20(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &Cw20Filterer{contract: contract}, nil
}

// bindCw20 binds a generic wrapper to an already deployed contract.
func bindCw20(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := Cw20MetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Cw20 *Cw20Raw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _Cw20.Contract.Cw20Caller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Cw20 *Cw20Raw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Cw20.Contract.Cw20Transactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Cw20 *Cw20Raw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Cw20.Contract.Cw20Transactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Cw20 *Cw20CallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _Cw20.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Cw20 *Cw20TransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Cw20.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Cw20 *Cw20TransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Cw20.Contract.contract.Transact(opts, method, params...)
}

// AddrPrecompile is a free data retrieval call binding the contract method 0xc2aed302.
//
// Solidity: function AddrPrecompile() view returns(address)
func (_Cw20 *Cw20Caller) AddrPrecompile(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _Cw20.contract.Call(opts, &out, "AddrPrecompile")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// AddrPrecompile is a free data retrieval call binding the contract method 0xc2aed302.
//
// Solidity: function AddrPrecompile() view returns(address)
func (_Cw20 *Cw20Session) AddrPrecompile() (common.Address, error) {
	return _Cw20.Contract.AddrPrecompile(&_Cw20.CallOpts)
}

// AddrPrecompile is a free data retrieval call binding the contract method 0xc2aed302.
//
// Solidity: function AddrPrecompile() view returns(address)
func (_Cw20 *Cw20CallerSession) AddrPrecompile() (common.Address, error) {
	return _Cw20.Contract.AddrPrecompile(&_Cw20.CallOpts)
}

// Cw20Address is a free data retrieval call binding the contract method 0xda73d16b.
//
// Solidity: function Cw20Address() view returns(string)
func (_Cw20 *Cw20Caller) Cw20Address(opts *bind.CallOpts) (string, error) {
	var out []interface{}
	err := _Cw20.contract.Call(opts, &out, "Cw20Address")

	if err != nil {
		return *new(string), err
	}

	out0 := *abi.ConvertType(out[0], new(string)).(*string)

	return out0, err

}

// Cw20Address is a free data retrieval call binding the contract method 0xda73d16b.
//
// Solidity: function Cw20Address() view returns(string)
func (_Cw20 *Cw20Session) Cw20Address() (string, error) {
	return _Cw20.Contract.Cw20Address(&_Cw20.CallOpts)
}

// Cw20Address is a free data retrieval call binding the contract method 0xda73d16b.
//
// Solidity: function Cw20Address() view returns(string)
func (_Cw20 *Cw20CallerSession) Cw20Address() (string, error) {
	return _Cw20.Contract.Cw20Address(&_Cw20.CallOpts)
}

// JsonPrecompile is a free data retrieval call binding the contract method 0xde4725cc.
//
// Solidity: function JsonPrecompile() view returns(address)
func (_Cw20 *Cw20Caller) JsonPrecompile(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _Cw20.contract.Call(opts, &out, "JsonPrecompile")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// JsonPrecompile is a free data retrieval call binding the contract method 0xde4725cc.
//
// Solidity: function JsonPrecompile() view returns(address)
func (_Cw20 *Cw20Session) JsonPrecompile() (common.Address, error) {
	return _Cw20.Contract.JsonPrecompile(&_Cw20.CallOpts)
}

// JsonPrecompile is a free data retrieval call binding the contract method 0xde4725cc.
//
// Solidity: function JsonPrecompile() view returns(address)
func (_Cw20 *Cw20CallerSession) JsonPrecompile() (common.Address, error) {
	return _Cw20.Contract.JsonPrecompile(&_Cw20.CallOpts)
}

// WasmdPrecompile is a free data retrieval call binding the contract method 0xf00b0255.
//
// Solidity: function WasmdPrecompile() view returns(address)
func (_Cw20 *Cw20Caller) WasmdPrecompile(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _Cw20.contract.Call(opts, &out, "WasmdPrecompile")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// WasmdPrecompile is a free data retrieval call binding the contract method 0xf00b0255.
//
// Solidity: function WasmdPrecompile() view returns(address)
func (_Cw20 *Cw20Session) WasmdPrecompile() (common.Address, error) {
	return _Cw20.Contract.WasmdPrecompile(&_Cw20.CallOpts)
}

// WasmdPrecompile is a free data retrieval call binding the contract method 0xf00b0255.
//
// Solidity: function WasmdPrecompile() view returns(address)
func (_Cw20 *Cw20CallerSession) WasmdPrecompile() (common.Address, error) {
	return _Cw20.Contract.WasmdPrecompile(&_Cw20.CallOpts)
}

// Allowance is a free data retrieval call binding the contract method 0xdd62ed3e.
//
// Solidity: function allowance(address owner, address spender) view returns(uint256)
func (_Cw20 *Cw20Caller) Allowance(opts *bind.CallOpts, owner common.Address, spender common.Address) (*big.Int, error) {
	var out []interface{}
	err := _Cw20.contract.Call(opts, &out, "allowance", owner, spender)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// Allowance is a free data retrieval call binding the contract method 0xdd62ed3e.
//
// Solidity: function allowance(address owner, address spender) view returns(uint256)
func (_Cw20 *Cw20Session) Allowance(owner common.Address, spender common.Address) (*big.Int, error) {
	return _Cw20.Contract.Allowance(&_Cw20.CallOpts, owner, spender)
}

// Allowance is a free data retrieval call binding the contract method 0xdd62ed3e.
//
// Solidity: function allowance(address owner, address spender) view returns(uint256)
func (_Cw20 *Cw20CallerSession) Allowance(owner common.Address, spender common.Address) (*big.Int, error) {
	return _Cw20.Contract.Allowance(&_Cw20.CallOpts, owner, spender)
}

// BalanceOf is a free data retrieval call binding the contract method 0x70a08231.
//
// Solidity: function balanceOf(address owner) view returns(uint256)
func (_Cw20 *Cw20Caller) BalanceOf(opts *bind.CallOpts, owner common.Address) (*big.Int, error) {
	var out []interface{}
	err := _Cw20.contract.Call(opts, &out, "balanceOf", owner)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// BalanceOf is a free data retrieval call binding the contract method 0x70a08231.
//
// Solidity: function balanceOf(address owner) view returns(uint256)
func (_Cw20 *Cw20Session) BalanceOf(owner common.Address) (*big.Int, error) {
	return _Cw20.Contract.BalanceOf(&_Cw20.CallOpts, owner)
}

// BalanceOf is a free data retrieval call binding the contract method 0x70a08231.
//
// Solidity: function balanceOf(address owner) view returns(uint256)
func (_Cw20 *Cw20CallerSession) BalanceOf(owner common.Address) (*big.Int, error) {
	return _Cw20.Contract.BalanceOf(&_Cw20.CallOpts, owner)
}

// Decimals is a free data retrieval call binding the contract method 0x313ce567.
//
// Solidity: function decimals() view returns(uint8)
func (_Cw20 *Cw20Caller) Decimals(opts *bind.CallOpts) (uint8, error) {
	var out []interface{}
	err := _Cw20.contract.Call(opts, &out, "decimals")

	if err != nil {
		return *new(uint8), err
	}

	out0 := *abi.ConvertType(out[0], new(uint8)).(*uint8)

	return out0, err

}

// Decimals is a free data retrieval call binding the contract method 0x313ce567.
//
// Solidity: function decimals() view returns(uint8)
func (_Cw20 *Cw20Session) Decimals() (uint8, error) {
	return _Cw20.Contract.Decimals(&_Cw20.CallOpts)
}

// Decimals is a free data retrieval call binding the contract method 0x313ce567.
//
// Solidity: function decimals() view returns(uint8)
func (_Cw20 *Cw20CallerSession) Decimals() (uint8, error) {
	return _Cw20.Contract.Decimals(&_Cw20.CallOpts)
}

// Name is a free data retrieval call binding the contract method 0x06fdde03.
//
// Solidity: function name() view returns(string)
func (_Cw20 *Cw20Caller) Name(opts *bind.CallOpts) (string, error) {
	var out []interface{}
	err := _Cw20.contract.Call(opts, &out, "name")

	if err != nil {
		return *new(string), err
	}

	out0 := *abi.ConvertType(out[0], new(string)).(*string)

	return out0, err

}

// Name is a free data retrieval call binding the contract method 0x06fdde03.
//
// Solidity: function name() view returns(string)
func (_Cw20 *Cw20Session) Name() (string, error) {
	return _Cw20.Contract.Name(&_Cw20.CallOpts)
}

// Name is a free data retrieval call binding the contract method 0x06fdde03.
//
// Solidity: function name() view returns(string)
func (_Cw20 *Cw20CallerSession) Name() (string, error) {
	return _Cw20.Contract.Name(&_Cw20.CallOpts)
}

// Symbol is a free data retrieval call binding the contract method 0x95d89b41.
//
// Solidity: function symbol() view returns(string)
func (_Cw20 *Cw20Caller) Symbol(opts *bind.CallOpts) (string, error) {
	var out []interface{}
	err := _Cw20.contract.Call(opts, &out, "symbol")

	if err != nil {
		return *new(string), err
	}

	out0 := *abi.ConvertType(out[0], new(string)).(*string)

	return out0, err

}

// Symbol is a free data retrieval call binding the contract method 0x95d89b41.
//
// Solidity: function symbol() view returns(string)
func (_Cw20 *Cw20Session) Symbol() (string, error) {
	return _Cw20.Contract.Symbol(&_Cw20.CallOpts)
}

// Symbol is a free data retrieval call binding the contract method 0x95d89b41.
//
// Solidity: function symbol() view returns(string)
func (_Cw20 *Cw20CallerSession) Symbol() (string, error) {
	return _Cw20.Contract.Symbol(&_Cw20.CallOpts)
}

// TotalSupply is a free data retrieval call binding the contract method 0x18160ddd.
//
// Solidity: function totalSupply() view returns(uint256)
func (_Cw20 *Cw20Caller) TotalSupply(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _Cw20.contract.Call(opts, &out, "totalSupply")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// TotalSupply is a free data retrieval call binding the contract method 0x18160ddd.
//
// Solidity: function totalSupply() view returns(uint256)
func (_Cw20 *Cw20Session) TotalSupply() (*big.Int, error) {
	return _Cw20.Contract.TotalSupply(&_Cw20.CallOpts)
}

// TotalSupply is a free data retrieval call binding the contract method 0x18160ddd.
//
// Solidity: function totalSupply() view returns(uint256)
func (_Cw20 *Cw20CallerSession) TotalSupply() (*big.Int, error) {
	return _Cw20.Contract.TotalSupply(&_Cw20.CallOpts)
}

// Approve is a paid mutator transaction binding the contract method 0x095ea7b3.
//
// Solidity: function approve(address spender, uint256 amount) returns(bool)
func (_Cw20 *Cw20Transactor) Approve(opts *bind.TransactOpts, spender common.Address, amount *big.Int) (*types.Transaction, error) {
	return _Cw20.contract.Transact(opts, "approve", spender, amount)
}

// Approve is a paid mutator transaction binding the contract method 0x095ea7b3.
//
// Solidity: function approve(address spender, uint256 amount) returns(bool)
func (_Cw20 *Cw20Session) Approve(spender common.Address, amount *big.Int) (*types.Transaction, error) {
	return _Cw20.Contract.Approve(&_Cw20.TransactOpts, spender, amount)
}

// Approve is a paid mutator transaction binding the contract method 0x095ea7b3.
//
// Solidity: function approve(address spender, uint256 amount) returns(bool)
func (_Cw20 *Cw20TransactorSession) Approve(spender common.Address, amount *big.Int) (*types.Transaction, error) {
	return _Cw20.Contract.Approve(&_Cw20.TransactOpts, spender, amount)
}

// Transfer is a paid mutator transaction binding the contract method 0xa9059cbb.
//
// Solidity: function transfer(address to, uint256 amount) returns(bool)
func (_Cw20 *Cw20Transactor) Transfer(opts *bind.TransactOpts, to common.Address, amount *big.Int) (*types.Transaction, error) {
	return _Cw20.contract.Transact(opts, "transfer", to, amount)
}

// Transfer is a paid mutator transaction binding the contract method 0xa9059cbb.
//
// Solidity: function transfer(address to, uint256 amount) returns(bool)
func (_Cw20 *Cw20Session) Transfer(to common.Address, amount *big.Int) (*types.Transaction, error) {
	return _Cw20.Contract.Transfer(&_Cw20.TransactOpts, to, amount)
}

// Transfer is a paid mutator transaction binding the contract method 0xa9059cbb.
//
// Solidity: function transfer(address to, uint256 amount) returns(bool)
func (_Cw20 *Cw20TransactorSession) Transfer(to common.Address, amount *big.Int) (*types.Transaction, error) {
	return _Cw20.Contract.Transfer(&_Cw20.TransactOpts, to, amount)
}

// TransferFrom is a paid mutator transaction binding the contract method 0x23b872dd.
//
// Solidity: function transferFrom(address from, address to, uint256 amount) returns(bool)
func (_Cw20 *Cw20Transactor) TransferFrom(opts *bind.TransactOpts, from common.Address, to common.Address, amount *big.Int) (*types.Transaction, error) {
	return _Cw20.contract.Transact(opts, "transferFrom", from, to, amount)
}

// TransferFrom is a paid mutator transaction binding the contract method 0x23b872dd.
//
// Solidity: function transferFrom(address from, address to, uint256 amount) returns(bool)
func (_Cw20 *Cw20Session) TransferFrom(from common.Address, to common.Address, amount *big.Int) (*types.Transaction, error) {
	return _Cw20.Contract.TransferFrom(&_Cw20.TransactOpts, from, to, amount)
}

// TransferFrom is a paid mutator transaction binding the contract method 0x23b872dd.
//
// Solidity: function transferFrom(address from, address to, uint256 amount) returns(bool)
func (_Cw20 *Cw20TransactorSession) TransferFrom(from common.Address, to common.Address, amount *big.Int) (*types.Transaction, error) {
	return _Cw20.Contract.TransferFrom(&_Cw20.TransactOpts, from, to, amount)
}

// Cw20ApprovalIterator is returned from FilterApproval and is used to iterate over the raw logs and unpacked data for Approval events raised by the Cw20 contract.
type Cw20ApprovalIterator struct {
	Event *Cw20Approval // Event containing the contract specifics and raw log

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
func (it *Cw20ApprovalIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(Cw20Approval)
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
		it.Event = new(Cw20Approval)
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
func (it *Cw20ApprovalIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *Cw20ApprovalIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// Cw20Approval represents a Approval event raised by the Cw20 contract.
type Cw20Approval struct {
	Owner   common.Address
	Spender common.Address
	Value   *big.Int
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterApproval is a free log retrieval operation binding the contract event 0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925.
//
// Solidity: event Approval(address indexed owner, address indexed spender, uint256 value)
func (_Cw20 *Cw20Filterer) FilterApproval(opts *bind.FilterOpts, owner []common.Address, spender []common.Address) (*Cw20ApprovalIterator, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}
	var spenderRule []interface{}
	for _, spenderItem := range spender {
		spenderRule = append(spenderRule, spenderItem)
	}

	logs, sub, err := _Cw20.contract.FilterLogs(opts, "Approval", ownerRule, spenderRule)
	if err != nil {
		return nil, err
	}
	return &Cw20ApprovalIterator{contract: _Cw20.contract, event: "Approval", logs: logs, sub: sub}, nil
}

// WatchApproval is a free log subscription operation binding the contract event 0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925.
//
// Solidity: event Approval(address indexed owner, address indexed spender, uint256 value)
func (_Cw20 *Cw20Filterer) WatchApproval(opts *bind.WatchOpts, sink chan<- *Cw20Approval, owner []common.Address, spender []common.Address) (event.Subscription, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}
	var spenderRule []interface{}
	for _, spenderItem := range spender {
		spenderRule = append(spenderRule, spenderItem)
	}

	logs, sub, err := _Cw20.contract.WatchLogs(opts, "Approval", ownerRule, spenderRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(Cw20Approval)
				if err := _Cw20.contract.UnpackLog(event, "Approval", log); err != nil {
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
// Solidity: event Approval(address indexed owner, address indexed spender, uint256 value)
func (_Cw20 *Cw20Filterer) ParseApproval(log types.Log) (*Cw20Approval, error) {
	event := new(Cw20Approval)
	if err := _Cw20.contract.UnpackLog(event, "Approval", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// Cw20TransferIterator is returned from FilterTransfer and is used to iterate over the raw logs and unpacked data for Transfer events raised by the Cw20 contract.
type Cw20TransferIterator struct {
	Event *Cw20Transfer // Event containing the contract specifics and raw log

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
func (it *Cw20TransferIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(Cw20Transfer)
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
		it.Event = new(Cw20Transfer)
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
func (it *Cw20TransferIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *Cw20TransferIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// Cw20Transfer represents a Transfer event raised by the Cw20 contract.
type Cw20Transfer struct {
	From  common.Address
	To    common.Address
	Value *big.Int
	Raw   types.Log // Blockchain specific contextual infos
}

// FilterTransfer is a free log retrieval operation binding the contract event 0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef.
//
// Solidity: event Transfer(address indexed from, address indexed to, uint256 value)
func (_Cw20 *Cw20Filterer) FilterTransfer(opts *bind.FilterOpts, from []common.Address, to []common.Address) (*Cw20TransferIterator, error) {

	var fromRule []interface{}
	for _, fromItem := range from {
		fromRule = append(fromRule, fromItem)
	}
	var toRule []interface{}
	for _, toItem := range to {
		toRule = append(toRule, toItem)
	}

	logs, sub, err := _Cw20.contract.FilterLogs(opts, "Transfer", fromRule, toRule)
	if err != nil {
		return nil, err
	}
	return &Cw20TransferIterator{contract: _Cw20.contract, event: "Transfer", logs: logs, sub: sub}, nil
}

// WatchTransfer is a free log subscription operation binding the contract event 0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef.
//
// Solidity: event Transfer(address indexed from, address indexed to, uint256 value)
func (_Cw20 *Cw20Filterer) WatchTransfer(opts *bind.WatchOpts, sink chan<- *Cw20Transfer, from []common.Address, to []common.Address) (event.Subscription, error) {

	var fromRule []interface{}
	for _, fromItem := range from {
		fromRule = append(fromRule, fromItem)
	}
	var toRule []interface{}
	for _, toItem := range to {
		toRule = append(toRule, toItem)
	}

	logs, sub, err := _Cw20.contract.WatchLogs(opts, "Transfer", fromRule, toRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(Cw20Transfer)
				if err := _Cw20.contract.UnpackLog(event, "Transfer", log); err != nil {
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
// Solidity: event Transfer(address indexed from, address indexed to, uint256 value)
func (_Cw20 *Cw20Filterer) ParseTransfer(log types.Log) (*Cw20Transfer, error) {
	event := new(Cw20Transfer)
	if err := _Cw20.contract.UnpackLog(event, "Transfer", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
