// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package univ2_swapper

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

// Univ2SwapperMetaData contains all meta data concerning the Univ2Swapper contract.
var Univ2SwapperMetaData = &bind.MetaData{
	ABI: "[{\"type\":\"constructor\",\"inputs\":[{\"name\":\"t1_\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"t2_\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"uniV2Router_\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"BIG_NUMBER\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"swap\",\"inputs\":[],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"t1\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"t2\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"uniV2Router\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"}]",
}

// Univ2SwapperABI is the input ABI used to generate the binding from.
// Deprecated: Use Univ2SwapperMetaData.ABI instead.
var Univ2SwapperABI = Univ2SwapperMetaData.ABI

// Univ2Swapper is an auto generated Go binding around an Ethereum contract.
type Univ2Swapper struct {
	Univ2SwapperCaller     // Read-only binding to the contract
	Univ2SwapperTransactor // Write-only binding to the contract
	Univ2SwapperFilterer   // Log filterer for contract events
}

// Univ2SwapperCaller is an auto generated read-only Go binding around an Ethereum contract.
type Univ2SwapperCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// Univ2SwapperTransactor is an auto generated write-only Go binding around an Ethereum contract.
type Univ2SwapperTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// Univ2SwapperFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type Univ2SwapperFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// Univ2SwapperSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type Univ2SwapperSession struct {
	Contract     *Univ2Swapper     // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// Univ2SwapperCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type Univ2SwapperCallerSession struct {
	Contract *Univ2SwapperCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts       // Call options to use throughout this session
}

// Univ2SwapperTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type Univ2SwapperTransactorSession struct {
	Contract     *Univ2SwapperTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts       // Transaction auth options to use throughout this session
}

// Univ2SwapperRaw is an auto generated low-level Go binding around an Ethereum contract.
type Univ2SwapperRaw struct {
	Contract *Univ2Swapper // Generic contract binding to access the raw methods on
}

// Univ2SwapperCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type Univ2SwapperCallerRaw struct {
	Contract *Univ2SwapperCaller // Generic read-only contract binding to access the raw methods on
}

// Univ2SwapperTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type Univ2SwapperTransactorRaw struct {
	Contract *Univ2SwapperTransactor // Generic write-only contract binding to access the raw methods on
}

// NewUniv2Swapper creates a new instance of Univ2Swapper, bound to a specific deployed contract.
func NewUniv2Swapper(address common.Address, backend bind.ContractBackend) (*Univ2Swapper, error) {
	contract, err := bindUniv2Swapper(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &Univ2Swapper{Univ2SwapperCaller: Univ2SwapperCaller{contract: contract}, Univ2SwapperTransactor: Univ2SwapperTransactor{contract: contract}, Univ2SwapperFilterer: Univ2SwapperFilterer{contract: contract}}, nil
}

// NewUniv2SwapperCaller creates a new read-only instance of Univ2Swapper, bound to a specific deployed contract.
func NewUniv2SwapperCaller(address common.Address, caller bind.ContractCaller) (*Univ2SwapperCaller, error) {
	contract, err := bindUniv2Swapper(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &Univ2SwapperCaller{contract: contract}, nil
}

// NewUniv2SwapperTransactor creates a new write-only instance of Univ2Swapper, bound to a specific deployed contract.
func NewUniv2SwapperTransactor(address common.Address, transactor bind.ContractTransactor) (*Univ2SwapperTransactor, error) {
	contract, err := bindUniv2Swapper(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &Univ2SwapperTransactor{contract: contract}, nil
}

// NewUniv2SwapperFilterer creates a new log filterer instance of Univ2Swapper, bound to a specific deployed contract.
func NewUniv2SwapperFilterer(address common.Address, filterer bind.ContractFilterer) (*Univ2SwapperFilterer, error) {
	contract, err := bindUniv2Swapper(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &Univ2SwapperFilterer{contract: contract}, nil
}

// bindUniv2Swapper binds a generic wrapper to an already deployed contract.
func bindUniv2Swapper(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := Univ2SwapperMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Univ2Swapper *Univ2SwapperRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _Univ2Swapper.Contract.Univ2SwapperCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Univ2Swapper *Univ2SwapperRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Univ2Swapper.Contract.Univ2SwapperTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Univ2Swapper *Univ2SwapperRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Univ2Swapper.Contract.Univ2SwapperTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Univ2Swapper *Univ2SwapperCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _Univ2Swapper.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Univ2Swapper *Univ2SwapperTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Univ2Swapper.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Univ2Swapper *Univ2SwapperTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Univ2Swapper.Contract.contract.Transact(opts, method, params...)
}

// BIGNUMBER is a free data retrieval call binding the contract method 0x2f4fda30.
//
// Solidity: function BIG_NUMBER() view returns(uint256)
func (_Univ2Swapper *Univ2SwapperCaller) BIGNUMBER(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _Univ2Swapper.contract.Call(opts, &out, "BIG_NUMBER")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// BIGNUMBER is a free data retrieval call binding the contract method 0x2f4fda30.
//
// Solidity: function BIG_NUMBER() view returns(uint256)
func (_Univ2Swapper *Univ2SwapperSession) BIGNUMBER() (*big.Int, error) {
	return _Univ2Swapper.Contract.BIGNUMBER(&_Univ2Swapper.CallOpts)
}

// BIGNUMBER is a free data retrieval call binding the contract method 0x2f4fda30.
//
// Solidity: function BIG_NUMBER() view returns(uint256)
func (_Univ2Swapper *Univ2SwapperCallerSession) BIGNUMBER() (*big.Int, error) {
	return _Univ2Swapper.Contract.BIGNUMBER(&_Univ2Swapper.CallOpts)
}

// T1 is a free data retrieval call binding the contract method 0xfb5343f3.
//
// Solidity: function t1() view returns(address)
func (_Univ2Swapper *Univ2SwapperCaller) T1(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _Univ2Swapper.contract.Call(opts, &out, "t1")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// T1 is a free data retrieval call binding the contract method 0xfb5343f3.
//
// Solidity: function t1() view returns(address)
func (_Univ2Swapper *Univ2SwapperSession) T1() (common.Address, error) {
	return _Univ2Swapper.Contract.T1(&_Univ2Swapper.CallOpts)
}

// T1 is a free data retrieval call binding the contract method 0xfb5343f3.
//
// Solidity: function t1() view returns(address)
func (_Univ2Swapper *Univ2SwapperCallerSession) T1() (common.Address, error) {
	return _Univ2Swapper.Contract.T1(&_Univ2Swapper.CallOpts)
}

// T2 is a free data retrieval call binding the contract method 0xbaf2f868.
//
// Solidity: function t2() view returns(address)
func (_Univ2Swapper *Univ2SwapperCaller) T2(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _Univ2Swapper.contract.Call(opts, &out, "t2")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// T2 is a free data retrieval call binding the contract method 0xbaf2f868.
//
// Solidity: function t2() view returns(address)
func (_Univ2Swapper *Univ2SwapperSession) T2() (common.Address, error) {
	return _Univ2Swapper.Contract.T2(&_Univ2Swapper.CallOpts)
}

// T2 is a free data retrieval call binding the contract method 0xbaf2f868.
//
// Solidity: function t2() view returns(address)
func (_Univ2Swapper *Univ2SwapperCallerSession) T2() (common.Address, error) {
	return _Univ2Swapper.Contract.T2(&_Univ2Swapper.CallOpts)
}

// UniV2Router is a free data retrieval call binding the contract method 0x958c2e52.
//
// Solidity: function uniV2Router() view returns(address)
func (_Univ2Swapper *Univ2SwapperCaller) UniV2Router(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _Univ2Swapper.contract.Call(opts, &out, "uniV2Router")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// UniV2Router is a free data retrieval call binding the contract method 0x958c2e52.
//
// Solidity: function uniV2Router() view returns(address)
func (_Univ2Swapper *Univ2SwapperSession) UniV2Router() (common.Address, error) {
	return _Univ2Swapper.Contract.UniV2Router(&_Univ2Swapper.CallOpts)
}

// UniV2Router is a free data retrieval call binding the contract method 0x958c2e52.
//
// Solidity: function uniV2Router() view returns(address)
func (_Univ2Swapper *Univ2SwapperCallerSession) UniV2Router() (common.Address, error) {
	return _Univ2Swapper.Contract.UniV2Router(&_Univ2Swapper.CallOpts)
}

// Swap is a paid mutator transaction binding the contract method 0x8119c065.
//
// Solidity: function swap() returns()
func (_Univ2Swapper *Univ2SwapperTransactor) Swap(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Univ2Swapper.contract.Transact(opts, "swap")
}

// Swap is a paid mutator transaction binding the contract method 0x8119c065.
//
// Solidity: function swap() returns()
func (_Univ2Swapper *Univ2SwapperSession) Swap() (*types.Transaction, error) {
	return _Univ2Swapper.Contract.Swap(&_Univ2Swapper.TransactOpts)
}

// Swap is a paid mutator transaction binding the contract method 0x8119c065.
//
// Solidity: function swap() returns()
func (_Univ2Swapper *Univ2SwapperTransactorSession) Swap() (*types.Transaction, error) {
	return _Univ2Swapper.Contract.Swap(&_Univ2Swapper.TransactOpts)
}
