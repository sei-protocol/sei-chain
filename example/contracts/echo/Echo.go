// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package echo

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

// EchoMetaData contains all meta data concerning the Echo contract.
var EchoMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"echo\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"pure\",\"type\":\"function\"}]",
}

// EchoABI is the input ABI used to generate the binding from.
// Deprecated: Use EchoMetaData.ABI instead.
var EchoABI = EchoMetaData.ABI

// Echo is an auto generated Go binding around an Ethereum contract.
type Echo struct {
	EchoCaller     // Read-only binding to the contract
	EchoTransactor // Write-only binding to the contract
	EchoFilterer   // Log filterer for contract events
}

// EchoCaller is an auto generated read-only Go binding around an Ethereum contract.
type EchoCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// EchoTransactor is an auto generated write-only Go binding around an Ethereum contract.
type EchoTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// EchoFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type EchoFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// EchoSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type EchoSession struct {
	Contract     *Echo             // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// EchoCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type EchoCallerSession struct {
	Contract *EchoCaller   // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts // Call options to use throughout this session
}

// EchoTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type EchoTransactorSession struct {
	Contract     *EchoTransactor   // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// EchoRaw is an auto generated low-level Go binding around an Ethereum contract.
type EchoRaw struct {
	Contract *Echo // Generic contract binding to access the raw methods on
}

// EchoCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type EchoCallerRaw struct {
	Contract *EchoCaller // Generic read-only contract binding to access the raw methods on
}

// EchoTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type EchoTransactorRaw struct {
	Contract *EchoTransactor // Generic write-only contract binding to access the raw methods on
}

// NewEcho creates a new instance of Echo, bound to a specific deployed contract.
func NewEcho(address common.Address, backend bind.ContractBackend) (*Echo, error) {
	contract, err := bindEcho(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &Echo{EchoCaller: EchoCaller{contract: contract}, EchoTransactor: EchoTransactor{contract: contract}, EchoFilterer: EchoFilterer{contract: contract}}, nil
}

// NewEchoCaller creates a new read-only instance of Echo, bound to a specific deployed contract.
func NewEchoCaller(address common.Address, caller bind.ContractCaller) (*EchoCaller, error) {
	contract, err := bindEcho(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &EchoCaller{contract: contract}, nil
}

// NewEchoTransactor creates a new write-only instance of Echo, bound to a specific deployed contract.
func NewEchoTransactor(address common.Address, transactor bind.ContractTransactor) (*EchoTransactor, error) {
	contract, err := bindEcho(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &EchoTransactor{contract: contract}, nil
}

// NewEchoFilterer creates a new log filterer instance of Echo, bound to a specific deployed contract.
func NewEchoFilterer(address common.Address, filterer bind.ContractFilterer) (*EchoFilterer, error) {
	contract, err := bindEcho(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &EchoFilterer{contract: contract}, nil
}

// bindEcho binds a generic wrapper to an already deployed contract.
func bindEcho(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := EchoMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Echo *EchoRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _Echo.Contract.EchoCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Echo *EchoRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Echo.Contract.EchoTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Echo *EchoRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Echo.Contract.EchoTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Echo *EchoCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _Echo.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Echo *EchoTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Echo.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Echo *EchoTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Echo.Contract.contract.Transact(opts, method, params...)
}

// Echo is a free data retrieval call binding the contract method 0x6279e43c.
//
// Solidity: function echo(uint256 value) pure returns(uint256)
func (_Echo *EchoCaller) Echo(opts *bind.CallOpts, value *big.Int) (*big.Int, error) {
	var out []interface{}
	err := _Echo.contract.Call(opts, &out, "echo", value)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// Echo is a free data retrieval call binding the contract method 0x6279e43c.
//
// Solidity: function echo(uint256 value) pure returns(uint256)
func (_Echo *EchoSession) Echo(value *big.Int) (*big.Int, error) {
	return _Echo.Contract.Echo(&_Echo.CallOpts, value)
}

// Echo is a free data retrieval call binding the contract method 0x6279e43c.
//
// Solidity: function echo(uint256 value) pure returns(uint256)
func (_Echo *EchoCallerSession) Echo(value *big.Int) (*big.Int, error) {
	return _Echo.Contract.Echo(&_Echo.CallOpts, value)
}
