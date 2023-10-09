// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package testdata

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

// TestdataMetaData contains all meta data concerning the Testdata contract.
var TestdataMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[],\"name\":\"get\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"set\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
}

// TestdataABI is the input ABI used to generate the binding from.
// Deprecated: Use TestdataMetaData.ABI instead.
var TestdataABI = TestdataMetaData.ABI

// Testdata is an auto generated Go binding around an Ethereum contract.
type Testdata struct {
	TestdataCaller     // Read-only binding to the contract
	TestdataTransactor // Write-only binding to the contract
	TestdataFilterer   // Log filterer for contract events
}

// TestdataCaller is an auto generated read-only Go binding around an Ethereum contract.
type TestdataCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// TestdataTransactor is an auto generated write-only Go binding around an Ethereum contract.
type TestdataTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// TestdataFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type TestdataFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// TestdataSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type TestdataSession struct {
	Contract     *Testdata         // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// TestdataCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type TestdataCallerSession struct {
	Contract *TestdataCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts   // Call options to use throughout this session
}

// TestdataTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type TestdataTransactorSession struct {
	Contract     *TestdataTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts   // Transaction auth options to use throughout this session
}

// TestdataRaw is an auto generated low-level Go binding around an Ethereum contract.
type TestdataRaw struct {
	Contract *Testdata // Generic contract binding to access the raw methods on
}

// TestdataCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type TestdataCallerRaw struct {
	Contract *TestdataCaller // Generic read-only contract binding to access the raw methods on
}

// TestdataTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type TestdataTransactorRaw struct {
	Contract *TestdataTransactor // Generic write-only contract binding to access the raw methods on
}

// NewTestdata creates a new instance of Testdata, bound to a specific deployed contract.
func NewTestdata(address common.Address, backend bind.ContractBackend) (*Testdata, error) {
	contract, err := bindTestdata(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &Testdata{TestdataCaller: TestdataCaller{contract: contract}, TestdataTransactor: TestdataTransactor{contract: contract}, TestdataFilterer: TestdataFilterer{contract: contract}}, nil
}

// NewTestdataCaller creates a new read-only instance of Testdata, bound to a specific deployed contract.
func NewTestdataCaller(address common.Address, caller bind.ContractCaller) (*TestdataCaller, error) {
	contract, err := bindTestdata(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &TestdataCaller{contract: contract}, nil
}

// NewTestdataTransactor creates a new write-only instance of Testdata, bound to a specific deployed contract.
func NewTestdataTransactor(address common.Address, transactor bind.ContractTransactor) (*TestdataTransactor, error) {
	contract, err := bindTestdata(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &TestdataTransactor{contract: contract}, nil
}

// NewTestdataFilterer creates a new log filterer instance of Testdata, bound to a specific deployed contract.
func NewTestdataFilterer(address common.Address, filterer bind.ContractFilterer) (*TestdataFilterer, error) {
	contract, err := bindTestdata(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &TestdataFilterer{contract: contract}, nil
}

// bindTestdata binds a generic wrapper to an already deployed contract.
func bindTestdata(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := TestdataMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Testdata *TestdataRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _Testdata.Contract.TestdataCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Testdata *TestdataRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Testdata.Contract.TestdataTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Testdata *TestdataRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Testdata.Contract.TestdataTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Testdata *TestdataCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _Testdata.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Testdata *TestdataTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Testdata.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Testdata *TestdataTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Testdata.Contract.contract.Transact(opts, method, params...)
}

// Get is a free data retrieval call binding the contract method 0x6d4ce63c.
//
// Solidity: function get() view returns(uint256)
func (_Testdata *TestdataCaller) Get(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _Testdata.contract.Call(opts, &out, "get")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// Get is a free data retrieval call binding the contract method 0x6d4ce63c.
//
// Solidity: function get() view returns(uint256)
func (_Testdata *TestdataSession) Get() (*big.Int, error) {
	return _Testdata.Contract.Get(&_Testdata.CallOpts)
}

// Get is a free data retrieval call binding the contract method 0x6d4ce63c.
//
// Solidity: function get() view returns(uint256)
func (_Testdata *TestdataCallerSession) Get() (*big.Int, error) {
	return _Testdata.Contract.Get(&_Testdata.CallOpts)
}

// Set is a paid mutator transaction binding the contract method 0x60fe47b1.
//
// Solidity: function set(uint256 value) returns()
func (_Testdata *TestdataTransactor) Set(opts *bind.TransactOpts, value *big.Int) (*types.Transaction, error) {
	return _Testdata.contract.Transact(opts, "set", value)
}

// Set is a paid mutator transaction binding the contract method 0x60fe47b1.
//
// Solidity: function set(uint256 value) returns()
func (_Testdata *TestdataSession) Set(value *big.Int) (*types.Transaction, error) {
	return _Testdata.Contract.Set(&_Testdata.TransactOpts, value)
}

// Set is a paid mutator transaction binding the contract method 0x60fe47b1.
//
// Solidity: function set(uint256 value) returns()
func (_Testdata *TestdataTransactorSession) Set(value *big.Int) (*types.Transaction, error) {
	return _Testdata.Contract.Set(&_Testdata.TransactOpts, value)
}
