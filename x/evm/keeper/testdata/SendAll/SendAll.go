// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package sendall

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

// SendallMetaData contains all meta data concerning the Sendall contract.
var SendallMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"address\",\"name\":\"fromAddress\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"toAddress\",\"type\":\"address\"},{\"internalType\":\"string\",\"name\":\"denom\",\"type\":\"string\"}],\"name\":\"sendAll\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
}

// SendallABI is the input ABI used to generate the binding from.
// Deprecated: Use SendallMetaData.ABI instead.
var SendallABI = SendallMetaData.ABI

// Sendall is an auto generated Go binding around an Ethereum contract.
type Sendall struct {
	SendallCaller     // Read-only binding to the contract
	SendallTransactor // Write-only binding to the contract
	SendallFilterer   // Log filterer for contract events
}

// SendallCaller is an auto generated read-only Go binding around an Ethereum contract.
type SendallCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// SendallTransactor is an auto generated write-only Go binding around an Ethereum contract.
type SendallTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// SendallFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type SendallFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// SendallSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type SendallSession struct {
	Contract     *Sendall          // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// SendallCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type SendallCallerSession struct {
	Contract *SendallCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts  // Call options to use throughout this session
}

// SendallTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type SendallTransactorSession struct {
	Contract     *SendallTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts  // Transaction auth options to use throughout this session
}

// SendallRaw is an auto generated low-level Go binding around an Ethereum contract.
type SendallRaw struct {
	Contract *Sendall // Generic contract binding to access the raw methods on
}

// SendallCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type SendallCallerRaw struct {
	Contract *SendallCaller // Generic read-only contract binding to access the raw methods on
}

// SendallTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type SendallTransactorRaw struct {
	Contract *SendallTransactor // Generic write-only contract binding to access the raw methods on
}

// NewSendall creates a new instance of Sendall, bound to a specific deployed contract.
func NewSendall(address common.Address, backend bind.ContractBackend) (*Sendall, error) {
	contract, err := bindSendall(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &Sendall{SendallCaller: SendallCaller{contract: contract}, SendallTransactor: SendallTransactor{contract: contract}, SendallFilterer: SendallFilterer{contract: contract}}, nil
}

// NewSendallCaller creates a new read-only instance of Sendall, bound to a specific deployed contract.
func NewSendallCaller(address common.Address, caller bind.ContractCaller) (*SendallCaller, error) {
	contract, err := bindSendall(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &SendallCaller{contract: contract}, nil
}

// NewSendallTransactor creates a new write-only instance of Sendall, bound to a specific deployed contract.
func NewSendallTransactor(address common.Address, transactor bind.ContractTransactor) (*SendallTransactor, error) {
	contract, err := bindSendall(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &SendallTransactor{contract: contract}, nil
}

// NewSendallFilterer creates a new log filterer instance of Sendall, bound to a specific deployed contract.
func NewSendallFilterer(address common.Address, filterer bind.ContractFilterer) (*SendallFilterer, error) {
	contract, err := bindSendall(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &SendallFilterer{contract: contract}, nil
}

// bindSendall binds a generic wrapper to an already deployed contract.
func bindSendall(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := SendallMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Sendall *SendallRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _Sendall.Contract.SendallCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Sendall *SendallRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Sendall.Contract.SendallTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Sendall *SendallRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Sendall.Contract.SendallTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Sendall *SendallCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _Sendall.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Sendall *SendallTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Sendall.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Sendall *SendallTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Sendall.Contract.contract.Transact(opts, method, params...)
}

// SendAll is a paid mutator transaction binding the contract method 0x89e5af5f.
//
// Solidity: function sendAll(address fromAddress, address toAddress, string denom) returns()
func (_Sendall *SendallTransactor) SendAll(opts *bind.TransactOpts, fromAddress common.Address, toAddress common.Address, denom string) (*types.Transaction, error) {
	return _Sendall.contract.Transact(opts, "sendAll", fromAddress, toAddress, denom)
}

// SendAll is a paid mutator transaction binding the contract method 0x89e5af5f.
//
// Solidity: function sendAll(address fromAddress, address toAddress, string denom) returns()
func (_Sendall *SendallSession) SendAll(fromAddress common.Address, toAddress common.Address, denom string) (*types.Transaction, error) {
	return _Sendall.Contract.SendAll(&_Sendall.TransactOpts, fromAddress, toAddress, denom)
}

// SendAll is a paid mutator transaction binding the contract method 0x89e5af5f.
//
// Solidity: function sendAll(address fromAddress, address toAddress, string denom) returns()
func (_Sendall *SendallTransactorSession) SendAll(fromAddress common.Address, toAddress common.Address, denom string) (*types.Transaction, error) {
	return _Sendall.Contract.SendAll(&_Sendall.TransactOpts, fromAddress, toAddress, denom)
}
