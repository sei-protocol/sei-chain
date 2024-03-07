// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package univ2_router

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

// Univ2RouterMetaData contains all meta data concerning the Univ2Router contract.
var Univ2RouterMetaData = &bind.MetaData{
	ABI: "[{\"type\":\"constructor\",\"inputs\":[{\"name\":\"_factory\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"addLiquidity\",\"inputs\":[{\"name\":\"tokenA\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"tokenB\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"amountADesired\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"amountBDesired\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"amountAMin\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"amountBMin\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"to\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"deadline\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"amountA\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"amountB\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"liquidity\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"factory\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"contractIUniswapV2Factory\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"removeLiquidity\",\"inputs\":[{\"name\":\"tokenA\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"tokenB\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"liquidity\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"amountAMin\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"amountBMin\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"to\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"deadline\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"amountA\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"amountB\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"swapExactTokensForTokens\",\"inputs\":[{\"name\":\"amountIn\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"amountOutMin\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"path\",\"type\":\"address[]\",\"internalType\":\"address[]\"},{\"name\":\"to\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"deadline\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"amounts\",\"type\":\"uint256[]\",\"internalType\":\"uint256[]\"}],\"stateMutability\":\"nonpayable\"},{\"type\":\"error\",\"name\":\"Expired\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"InsufficientAmountA\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"InsufficientAmountB\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"SafeTransferFromFailed\",\"inputs\":[]}]",
}

// Univ2RouterABI is the input ABI used to generate the binding from.
// Deprecated: Use Univ2RouterMetaData.ABI instead.
var Univ2RouterABI = Univ2RouterMetaData.ABI

// Univ2Router is an auto generated Go binding around an Ethereum contract.
type Univ2Router struct {
	Univ2RouterCaller     // Read-only binding to the contract
	Univ2RouterTransactor // Write-only binding to the contract
	Univ2RouterFilterer   // Log filterer for contract events
}

// Univ2RouterCaller is an auto generated read-only Go binding around an Ethereum contract.
type Univ2RouterCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// Univ2RouterTransactor is an auto generated write-only Go binding around an Ethereum contract.
type Univ2RouterTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// Univ2RouterFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type Univ2RouterFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// Univ2RouterSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type Univ2RouterSession struct {
	Contract     *Univ2Router      // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// Univ2RouterCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type Univ2RouterCallerSession struct {
	Contract *Univ2RouterCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts      // Call options to use throughout this session
}

// Univ2RouterTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type Univ2RouterTransactorSession struct {
	Contract     *Univ2RouterTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts      // Transaction auth options to use throughout this session
}

// Univ2RouterRaw is an auto generated low-level Go binding around an Ethereum contract.
type Univ2RouterRaw struct {
	Contract *Univ2Router // Generic contract binding to access the raw methods on
}

// Univ2RouterCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type Univ2RouterCallerRaw struct {
	Contract *Univ2RouterCaller // Generic read-only contract binding to access the raw methods on
}

// Univ2RouterTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type Univ2RouterTransactorRaw struct {
	Contract *Univ2RouterTransactor // Generic write-only contract binding to access the raw methods on
}

// NewUniv2Router creates a new instance of Univ2Router, bound to a specific deployed contract.
func NewUniv2Router(address common.Address, backend bind.ContractBackend) (*Univ2Router, error) {
	contract, err := bindUniv2Router(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &Univ2Router{Univ2RouterCaller: Univ2RouterCaller{contract: contract}, Univ2RouterTransactor: Univ2RouterTransactor{contract: contract}, Univ2RouterFilterer: Univ2RouterFilterer{contract: contract}}, nil
}

// NewUniv2RouterCaller creates a new read-only instance of Univ2Router, bound to a specific deployed contract.
func NewUniv2RouterCaller(address common.Address, caller bind.ContractCaller) (*Univ2RouterCaller, error) {
	contract, err := bindUniv2Router(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &Univ2RouterCaller{contract: contract}, nil
}

// NewUniv2RouterTransactor creates a new write-only instance of Univ2Router, bound to a specific deployed contract.
func NewUniv2RouterTransactor(address common.Address, transactor bind.ContractTransactor) (*Univ2RouterTransactor, error) {
	contract, err := bindUniv2Router(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &Univ2RouterTransactor{contract: contract}, nil
}

// NewUniv2RouterFilterer creates a new log filterer instance of Univ2Router, bound to a specific deployed contract.
func NewUniv2RouterFilterer(address common.Address, filterer bind.ContractFilterer) (*Univ2RouterFilterer, error) {
	contract, err := bindUniv2Router(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &Univ2RouterFilterer{contract: contract}, nil
}

// bindUniv2Router binds a generic wrapper to an already deployed contract.
func bindUniv2Router(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := Univ2RouterMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Univ2Router *Univ2RouterRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _Univ2Router.Contract.Univ2RouterCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Univ2Router *Univ2RouterRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Univ2Router.Contract.Univ2RouterTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Univ2Router *Univ2RouterRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Univ2Router.Contract.Univ2RouterTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Univ2Router *Univ2RouterCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _Univ2Router.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Univ2Router *Univ2RouterTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Univ2Router.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Univ2Router *Univ2RouterTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Univ2Router.Contract.contract.Transact(opts, method, params...)
}

// Factory is a free data retrieval call binding the contract method 0xc45a0155.
//
// Solidity: function factory() view returns(address)
func (_Univ2Router *Univ2RouterCaller) Factory(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _Univ2Router.contract.Call(opts, &out, "factory")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Factory is a free data retrieval call binding the contract method 0xc45a0155.
//
// Solidity: function factory() view returns(address)
func (_Univ2Router *Univ2RouterSession) Factory() (common.Address, error) {
	return _Univ2Router.Contract.Factory(&_Univ2Router.CallOpts)
}

// Factory is a free data retrieval call binding the contract method 0xc45a0155.
//
// Solidity: function factory() view returns(address)
func (_Univ2Router *Univ2RouterCallerSession) Factory() (common.Address, error) {
	return _Univ2Router.Contract.Factory(&_Univ2Router.CallOpts)
}

// AddLiquidity is a paid mutator transaction binding the contract method 0xe8e33700.
//
// Solidity: function addLiquidity(address tokenA, address tokenB, uint256 amountADesired, uint256 amountBDesired, uint256 amountAMin, uint256 amountBMin, address to, uint256 deadline) returns(uint256 amountA, uint256 amountB, uint256 liquidity)
func (_Univ2Router *Univ2RouterTransactor) AddLiquidity(opts *bind.TransactOpts, tokenA common.Address, tokenB common.Address, amountADesired *big.Int, amountBDesired *big.Int, amountAMin *big.Int, amountBMin *big.Int, to common.Address, deadline *big.Int) (*types.Transaction, error) {
	return _Univ2Router.contract.Transact(opts, "addLiquidity", tokenA, tokenB, amountADesired, amountBDesired, amountAMin, amountBMin, to, deadline)
}

// AddLiquidity is a paid mutator transaction binding the contract method 0xe8e33700.
//
// Solidity: function addLiquidity(address tokenA, address tokenB, uint256 amountADesired, uint256 amountBDesired, uint256 amountAMin, uint256 amountBMin, address to, uint256 deadline) returns(uint256 amountA, uint256 amountB, uint256 liquidity)
func (_Univ2Router *Univ2RouterSession) AddLiquidity(tokenA common.Address, tokenB common.Address, amountADesired *big.Int, amountBDesired *big.Int, amountAMin *big.Int, amountBMin *big.Int, to common.Address, deadline *big.Int) (*types.Transaction, error) {
	return _Univ2Router.Contract.AddLiquidity(&_Univ2Router.TransactOpts, tokenA, tokenB, amountADesired, amountBDesired, amountAMin, amountBMin, to, deadline)
}

// AddLiquidity is a paid mutator transaction binding the contract method 0xe8e33700.
//
// Solidity: function addLiquidity(address tokenA, address tokenB, uint256 amountADesired, uint256 amountBDesired, uint256 amountAMin, uint256 amountBMin, address to, uint256 deadline) returns(uint256 amountA, uint256 amountB, uint256 liquidity)
func (_Univ2Router *Univ2RouterTransactorSession) AddLiquidity(tokenA common.Address, tokenB common.Address, amountADesired *big.Int, amountBDesired *big.Int, amountAMin *big.Int, amountBMin *big.Int, to common.Address, deadline *big.Int) (*types.Transaction, error) {
	return _Univ2Router.Contract.AddLiquidity(&_Univ2Router.TransactOpts, tokenA, tokenB, amountADesired, amountBDesired, amountAMin, amountBMin, to, deadline)
}

// RemoveLiquidity is a paid mutator transaction binding the contract method 0xbaa2abde.
//
// Solidity: function removeLiquidity(address tokenA, address tokenB, uint256 liquidity, uint256 amountAMin, uint256 amountBMin, address to, uint256 deadline) returns(uint256 amountA, uint256 amountB)
func (_Univ2Router *Univ2RouterTransactor) RemoveLiquidity(opts *bind.TransactOpts, tokenA common.Address, tokenB common.Address, liquidity *big.Int, amountAMin *big.Int, amountBMin *big.Int, to common.Address, deadline *big.Int) (*types.Transaction, error) {
	return _Univ2Router.contract.Transact(opts, "removeLiquidity", tokenA, tokenB, liquidity, amountAMin, amountBMin, to, deadline)
}

// RemoveLiquidity is a paid mutator transaction binding the contract method 0xbaa2abde.
//
// Solidity: function removeLiquidity(address tokenA, address tokenB, uint256 liquidity, uint256 amountAMin, uint256 amountBMin, address to, uint256 deadline) returns(uint256 amountA, uint256 amountB)
func (_Univ2Router *Univ2RouterSession) RemoveLiquidity(tokenA common.Address, tokenB common.Address, liquidity *big.Int, amountAMin *big.Int, amountBMin *big.Int, to common.Address, deadline *big.Int) (*types.Transaction, error) {
	return _Univ2Router.Contract.RemoveLiquidity(&_Univ2Router.TransactOpts, tokenA, tokenB, liquidity, amountAMin, amountBMin, to, deadline)
}

// RemoveLiquidity is a paid mutator transaction binding the contract method 0xbaa2abde.
//
// Solidity: function removeLiquidity(address tokenA, address tokenB, uint256 liquidity, uint256 amountAMin, uint256 amountBMin, address to, uint256 deadline) returns(uint256 amountA, uint256 amountB)
func (_Univ2Router *Univ2RouterTransactorSession) RemoveLiquidity(tokenA common.Address, tokenB common.Address, liquidity *big.Int, amountAMin *big.Int, amountBMin *big.Int, to common.Address, deadline *big.Int) (*types.Transaction, error) {
	return _Univ2Router.Contract.RemoveLiquidity(&_Univ2Router.TransactOpts, tokenA, tokenB, liquidity, amountAMin, amountBMin, to, deadline)
}

// SwapExactTokensForTokens is a paid mutator transaction binding the contract method 0x38ed1739.
//
// Solidity: function swapExactTokensForTokens(uint256 amountIn, uint256 amountOutMin, address[] path, address to, uint256 deadline) returns(uint256[] amounts)
func (_Univ2Router *Univ2RouterTransactor) SwapExactTokensForTokens(opts *bind.TransactOpts, amountIn *big.Int, amountOutMin *big.Int, path []common.Address, to common.Address, deadline *big.Int) (*types.Transaction, error) {
	return _Univ2Router.contract.Transact(opts, "swapExactTokensForTokens", amountIn, amountOutMin, path, to, deadline)
}

// SwapExactTokensForTokens is a paid mutator transaction binding the contract method 0x38ed1739.
//
// Solidity: function swapExactTokensForTokens(uint256 amountIn, uint256 amountOutMin, address[] path, address to, uint256 deadline) returns(uint256[] amounts)
func (_Univ2Router *Univ2RouterSession) SwapExactTokensForTokens(amountIn *big.Int, amountOutMin *big.Int, path []common.Address, to common.Address, deadline *big.Int) (*types.Transaction, error) {
	return _Univ2Router.Contract.SwapExactTokensForTokens(&_Univ2Router.TransactOpts, amountIn, amountOutMin, path, to, deadline)
}

// SwapExactTokensForTokens is a paid mutator transaction binding the contract method 0x38ed1739.
//
// Solidity: function swapExactTokensForTokens(uint256 amountIn, uint256 amountOutMin, address[] path, address to, uint256 deadline) returns(uint256[] amounts)
func (_Univ2Router *Univ2RouterTransactorSession) SwapExactTokensForTokens(amountIn *big.Int, amountOutMin *big.Int, path []common.Address, to common.Address, deadline *big.Int) (*types.Transaction, error) {
	return _Univ2Router.Contract.SwapExactTokensForTokens(&_Univ2Router.TransactOpts, amountIn, amountOutMin, path, to, deadline)
}

