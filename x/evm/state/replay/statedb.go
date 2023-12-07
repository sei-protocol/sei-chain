package replay

import (
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/x/evm/state"
)

type ReplayStateDB struct {
	*state.DBImpl

	rpcClient          *rpc.Client
	genesisBlockNumber int64
	cachedBalances     map[string]struct{}
	cachedStates       map[string]map[string]struct{}
	interBlockCache    *InterBlockCache
}

type InterBlockCache struct {
	accessedBalances map[string]struct{}
	accessedStates   map[string]map[string]struct{}
}

func NewReplayStateDB(ctx sdk.Context, k state.EVMKeeper, rpcClient *rpc.Client, genesisBlockNumber int64, interBlockCache *InterBlockCache) *ReplayStateDB {
	return &ReplayStateDB{
		DBImpl:             state.NewDBImpl(ctx, k, true),
		rpcClient:          rpcClient,
		genesisBlockNumber: genesisBlockNumber,
		interBlockCache:    interBlockCache,
		cachedBalances:     map[string]struct{}{},
		cachedStates:       map[string]map[string]struct{}{},
	}
}

func (s *ReplayStateDB) SubBalance(evmAddr common.Address, amt *big.Int) {
	defer func() { s.DBImpl.SubBalance(evmAddr, amt) }()
	if _, ok := s.cachedBalances[evmAddr.Hex()]; ok {
		return
	}
	if _, ok := s.interBlockCache.accessedBalances[evmAddr.Hex()]; ok {
		return
	}
	s.readBalanceFromRemoteAndCache(evmAddr)
}

func (s *ReplayStateDB) AddBalance(evmAddr common.Address, amt *big.Int) {
	defer func() { s.DBImpl.AddBalance(evmAddr, amt) }()
	if _, ok := s.cachedBalances[evmAddr.Hex()]; ok {
		return
	}
	if _, ok := s.interBlockCache.accessedBalances[evmAddr.Hex()]; ok {
		return
	}
	s.readBalanceFromRemoteAndCache(evmAddr)
}

func (s *ReplayStateDB) GetBalance(evmAddr common.Address) *big.Int {
	if _, ok := s.cachedBalances[evmAddr.Hex()]; ok {
		return s.DBImpl.GetBalance(evmAddr)
	}
	return s.readBalanceFromRemoteAndCache(evmAddr)
}

func (s *ReplayStateDB) readBalanceFromRemoteAndCache(addr common.Address) *big.Int {
	res := new(hexutil.Big)
	if err := s.rpcClient.Call(res, "eth_getBalance", addr, rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(s.genesisBlockNumber))); err != nil {
		panic(err)
	}
	s.DBImpl.SetBalance(addr, res.ToInt())
	s.cachedBalances[addr.Hex()] = struct{}{}
	return res.ToInt()
}

func (s *ReplayStateDB) GetCommittedState(addr common.Address, hash common.Hash) common.Hash {
	if addrStates, ok := s.interBlockCache.accessedStates[addr.Hex()]; ok {
		if _, ok := addrStates[hash.Hex()]; ok {
			return s.DBImpl.GetCommittedState(addr, hash)
		}
	}
	return s.readStateFromRemoteAndCache(addr, hash)
}

func (s *ReplayStateDB) GetState(addr common.Address, hash common.Hash) common.Hash {
	if addrStates, ok := s.cachedStates[addr.Hex()]; ok {
		if _, ok := addrStates[hash.Hex()]; ok {
			return s.DBImpl.GetState(addr, hash)
		}
	}
	if addrStates, ok := s.interBlockCache.accessedStates[addr.Hex()]; ok {
		if _, ok := addrStates[hash.Hex()]; ok {
			return s.DBImpl.GetState(addr, hash)
		}
	}
	return s.readStateFromRemoteAndCache(addr, hash)
}

func (s *ReplayStateDB) SetState(addr common.Address, key common.Hash, val common.Hash) {
	if _, ok := s.cachedStates[addr.Hex()]; !ok {
		s.cachedStates[addr.Hex()] = map[string]struct{}{}
	}
	s.cachedStates[addr.Hex()][key.Hex()] = struct{}{}
	s.DBImpl.SetState(addr, key, val)
}

func (s *ReplayStateDB) readStateFromRemoteAndCache(addr common.Address, hash common.Hash) common.Hash {
	res := new(hexutil.Bytes)
	if err := s.rpcClient.Call(res, "eth_getStorageAt", addr, hash.Hex(), rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(s.genesisBlockNumber))); err != nil {
		panic(err)
	}
	val := common.BytesToHash(*res)
	s.SetState(addr, hash, val)
	return val
}

func (s *ReplayStateDB) Finalize() error {
	if err := s.DBImpl.Finalize(); err != nil {
		panic(err)
	}
	for cachedBalance := range s.cachedBalances {
		s.interBlockCache.accessedBalances[cachedBalance] = struct{}{}
	}
	for cachedAddr, states := range s.cachedStates {
		for cachedKey := range states {
			s.interBlockCache.accessedStates[cachedAddr][cachedKey] = struct{}{}
		}
	}
	return nil
}
