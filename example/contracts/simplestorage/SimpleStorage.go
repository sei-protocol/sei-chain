// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package simplestorage

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

// SimplestorageMetaData contains all meta data concerning the Simplestorage contract.
var SimplestorageMetaData = &bind.MetaData{
	ABI: "[{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"SetEvent\",\"type\":\"event\"},{\"inputs\":[],\"name\":\"bad\",\"outputs\":[],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"get\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"set\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
}

// SimplestorageABI is the input ABI used to generate the binding from.
// Deprecated: Use SimplestorageMetaData.ABI instead.
var SimplestorageABI = SimplestorageMetaData.ABI

// Simplestorage is an auto generated Go binding around an Ethereum contract.
type Simplestorage struct {
	SimplestorageCaller     // Read-only binding to the contract
	SimplestorageTransactor // Write-only binding to the contract
	SimplestorageFilterer   // Log filterer for contract events
}

// SimplestorageCaller is an auto generated read-only Go binding around an Ethereum contract.
type SimplestorageCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// SimplestorageTransactor is an auto generated write-only Go binding around an Ethereum contract.
type SimplestorageTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// SimplestorageFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type SimplestorageFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// SimplestorageSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type SimplestorageSession struct {
	Contract     *Simplestorage    // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// SimplestorageCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type SimplestorageCallerSession struct {
	Contract *SimplestorageCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts        // Call options to use throughout this session
}

// SimplestorageTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type SimplestorageTransactorSession struct {
	Contract     *SimplestorageTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts        // Transaction auth options to use throughout this session
}

// SimplestorageRaw is an auto generated low-level Go binding around an Ethereum contract.
type SimplestorageRaw struct {
	Contract *Simplestorage // Generic contract binding to access the raw methods on
}

// SimplestorageCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type SimplestorageCallerRaw struct {
	Contract *SimplestorageCaller // Generic read-only contract binding to access the raw methods on
}

// SimplestorageTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type SimplestorageTransactorRaw struct {
	Contract *SimplestorageTransactor // Generic write-only contract binding to access the raw methods on
}

// NewSimplestorage creates a new instance of Simplestorage, bound to a specific deployed contract.
func NewSimplestorage(address common.Address, backend bind.ContractBackend) (*Simplestorage, error) {
	contract, err := bindSimplestorage(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &Simplestorage{SimplestorageCaller: SimplestorageCaller{contract: contract}, SimplestorageTransactor: SimplestorageTransactor{contract: contract}, SimplestorageFilterer: SimplestorageFilterer{contract: contract}}, nil
}

// NewSimplestorageCaller creates a new read-only instance of Simplestorage, bound to a specific deployed contract.
func NewSimplestorageCaller(address common.Address, caller bind.ContractCaller) (*SimplestorageCaller, error) {
	contract, err := bindSimplestorage(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &SimplestorageCaller{contract: contract}, nil
}

// NewSimplestorageTransactor creates a new write-only instance of Simplestorage, bound to a specific deployed contract.
func NewSimplestorageTransactor(address common.Address, transactor bind.ContractTransactor) (*SimplestorageTransactor, error) {
	contract, err := bindSimplestorage(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &SimplestorageTransactor{contract: contract}, nil
}

// NewSimplestorageFilterer creates a new log filterer instance of Simplestorage, bound to a specific deployed contract.
func NewSimplestorageFilterer(address common.Address, filterer bind.ContractFilterer) (*SimplestorageFilterer, error) {
	contract, err := bindSimplestorage(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &SimplestorageFilterer{contract: contract}, nil
}

// bindSimplestorage binds a generic wrapper to an already deployed contract.
func bindSimplestorage(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := SimplestorageMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Simplestorage *SimplestorageRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _Simplestorage.Contract.SimplestorageCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Simplestorage *SimplestorageRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Simplestorage.Contract.SimplestorageTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Simplestorage *SimplestorageRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Simplestorage.Contract.SimplestorageTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Simplestorage *SimplestorageCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _Simplestorage.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Simplestorage *SimplestorageTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Simplestorage.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Simplestorage *SimplestorageTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Simplestorage.Contract.contract.Transact(opts, method, params...)
}

// Bad is a free data retrieval call binding the contract method 0x9c3674fc.
//
// Solidity: function bad() pure returns()
func (_Simplestorage *SimplestorageCaller) Bad(opts *bind.CallOpts) error {
	var out []interface{}
	err := _Simplestorage.contract.Call(opts, &out, "bad")

	if err != nil {
		return err
	}

	return err

}

// Bad is a free data retrieval call binding the contract method 0x9c3674fc.
//
// Solidity: function bad() pure returns()
func (_Simplestorage *SimplestorageSession) Bad() error {
	return _Simplestorage.Contract.Bad(&_Simplestorage.CallOpts)
}

// Bad is a free data retrieval call binding the contract method 0x9c3674fc.
//
// Solidity: function bad() pure returns()
func (_Simplestorage *SimplestorageCallerSession) Bad() error {
	return _Simplestorage.Contract.Bad(&_Simplestorage.CallOpts)
}

// Get is a free data retrieval call binding the contract method 0x6d4ce63c.
//
// Solidity: function get() view returns(uint256)
func (_Simplestorage *SimplestorageCaller) Get(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _Simplestorage.contract.Call(opts, &out, "get")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// Get is a free data retrieval call binding the contract method 0x6d4ce63c.
//
// Solidity: function get() view returns(uint256)
func (_Simplestorage *SimplestorageSession) Get() (*big.Int, error) {
	return _Simplestorage.Contract.Get(&_Simplestorage.CallOpts)
}

// Get is a free data retrieval call binding the contract method 0x6d4ce63c.
//
// Solidity: function get() view returns(uint256)
func (_Simplestorage *SimplestorageCallerSession) Get() (*big.Int, error) {
	return _Simplestorage.Contract.Get(&_Simplestorage.CallOpts)
}

// Set is a paid mutator transaction binding the contract method 0x60fe47b1.
//
// Solidity: function set(uint256 value) returns()
func (_Simplestorage *SimplestorageTransactor) Set(opts *bind.TransactOpts, value *big.Int) (*types.Transaction, error) {
	return _Simplestorage.contract.Transact(opts, "set", value)
}

// Set is a paid mutator transaction binding the contract method 0x60fe47b1.
//
// Solidity: function set(uint256 value) returns()
func (_Simplestorage *SimplestorageSession) Set(value *big.Int) (*types.Transaction, error) {
	return _Simplestorage.Contract.Set(&_Simplestorage.TransactOpts, value)
}

// Set is a paid mutator transaction binding the contract method 0x60fe47b1.
//
// Solidity: function set(uint256 value) returns()
func (_Simplestorage *SimplestorageTransactorSession) Set(value *big.Int) (*types.Transaction, error) {
	return _Simplestorage.Contract.Set(&_Simplestorage.TransactOpts, value)
}

// SimplestorageSetEventIterator is returned from FilterSetEvent and is used to iterate over the raw logs and unpacked data for SetEvent events raised by the Simplestorage contract.
type SimplestorageSetEventIterator struct {
	Event *SimplestorageSetEvent // Event containing the contract specifics and raw log

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
func (it *SimplestorageSetEventIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SimplestorageSetEvent)
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
		it.Event = new(SimplestorageSetEvent)
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
func (it *SimplestorageSetEventIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SimplestorageSetEventIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SimplestorageSetEvent represents a SetEvent event raised by the Simplestorage contract.
type SimplestorageSetEvent struct {
	Value *big.Int
	Raw   types.Log // Blockchain specific contextual infos
}

// FilterSetEvent is a free log retrieval operation binding the contract event 0x0de2d86113046b9e8bb6b785e96a6228f6803952bf53a40b68a36dce316218c1.
//
// Solidity: event SetEvent(uint256 value)
func (_Simplestorage *SimplestorageFilterer) FilterSetEvent(opts *bind.FilterOpts) (*SimplestorageSetEventIterator, error) {

	logs, sub, err := _Simplestorage.contract.FilterLogs(opts, "SetEvent")
	if err != nil {
		return nil, err
	}
	return &SimplestorageSetEventIterator{contract: _Simplestorage.contract, event: "SetEvent", logs: logs, sub: sub}, nil
}

// WatchSetEvent is a free log subscription operation binding the contract event 0x0de2d86113046b9e8bb6b785e96a6228f6803952bf53a40b68a36dce316218c1.
//
// Solidity: event SetEvent(uint256 value)
func (_Simplestorage *SimplestorageFilterer) WatchSetEvent(opts *bind.WatchOpts, sink chan<- *SimplestorageSetEvent) (event.Subscription, error) {

	logs, sub, err := _Simplestorage.contract.WatchLogs(opts, "SetEvent")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SimplestorageSetEvent)
				if err := _Simplestorage.contract.UnpackLog(event, "SetEvent", log); err != nil {
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

// ParseSetEvent is a log parse operation binding the contract event 0x0de2d86113046b9e8bb6b785e96a6228f6803952bf53a40b68a36dce316218c1.
//
// Solidity: event SetEvent(uint256 value)
func (_Simplestorage *SimplestorageFilterer) ParseSetEvent(log types.Log) (*SimplestorageSetEvent, error) {
	event := new(SimplestorageSetEvent)
	if err := _Simplestorage.contract.UnpackLog(event, "SetEvent", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
