package evmonlyapp

import (
	"encoding/binary"
	"maps"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

var genesisTestCode = []byte{0x60, 0x00, 0xf3}

func genesisTestContract() evmonly.GenesisAccount {
	return evmonly.GenesisAccount{Nonce: 2, Code: genesisTestCode, Storage: map[common.Hash]common.Hash{{0x02}: {0x01}}}
}

func requireGenesisTestContract(t *testing.T, app *evmOnlyApplication, addr common.Address) {
	t.Helper()
	require.Equal(t, genesisTestCode, app.EvmCode(addr))
	require.Equal(t, uint64(2), app.EvmNonce(addr))
	value, err := app.EvmStorageAt(addr, common.Hash{0x02}, 1)
	require.NoError(t, err)
	require.Equal(t, common.Hash{0x01}, value)
}

func TestEVMOnlyApplicationRefusesAnAllocThatFundsNoAccount(t *testing.T) {
	storage := openEVMOnlyTestStorageAt(t, t.TempDir())
	t.Cleanup(func() { require.NoError(t, storage.Close()) })
	_, err := NewEVMOnlyApplication(
		evmOnlyTestChainID, nil, storage,
		gigaconfig.DefaultConfig.Execution,
		utils.None[precompiles.Registry](),
		utils.Some(GenesisAlloc{InitialHeight: 1, Accounts: map[common.Address]evmonly.GenesisAccount{{0xc0}: genesisTestContract()}}),
		utils.None[string](),
	)
	require.Error(t, err)
}

func TestEVMOnlyAllocWithABalanceStartsOtherAccountsEmpty(t *testing.T) {
	funder, contract := common.Address{0xf1}, common.Address{0xc0}
	app := newEVMOnlyTestAppWithAlloc(t, GenesisAlloc{InitialHeight: 1, Accounts: map[common.Address]evmonly.GenesisAccount{
		funder:   {Balance: uint256.NewInt(1_000)},
		contract: genesisTestContract(),
	}}).(*evmOnlyApplication)
	_, err := app.InitChain(evmOnlyTestInitChain())
	require.NoError(t, err)
	finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(1))

	requireGenesisTestContract(t, app, contract)
	require.Equal(t, *uint256.NewInt(1_000), app.EvmBalance(funder, nil))
	require.Equal(t, uint256.Int{}, app.EvmBalance(contract, nil))
	require.Equal(t, uint256.Int{}, app.EvmBalance(common.Address{0xaa}, nil))
}

// TestGenesisAllocOfBalancesKeepsItsDigest pins the digest of an alloc that sets balances alone to the
// one a chain started with before accounts carried more than a balance.
func TestGenesisAllocOfBalancesKeepsItsDigest(t *testing.T) {
	alloc := GenesisAlloc{InitialHeight: 1, Accounts: balanceAlloc(map[common.Address]*uint256.Int{
		{2}: uint256.NewInt(7),
		{1}: uint256.NewInt(5),
	})}
	buf := binary.BigEndian.AppendUint64(nil, 1)
	for _, entry := range []struct {
		addr    common.Address
		balance uint64
	}{{common.Address{1}, 5}, {common.Address{2}, 7}} {
		balance := uint256.NewInt(entry.balance).Bytes32()
		buf = append(append(buf, entry.addr[:]...), balance[:]...)
	}
	require.Equal(t, crypto.Keccak256Hash(buf), alloc.digest())
}

func TestGenesisAllocDigestCoversEveryField(t *testing.T) {
	base := GenesisAlloc{InitialHeight: 1, Accounts: map[common.Address]evmonly.GenesisAccount{{1}: genesisTestContract()}}
	variants := map[string]func(*evmonly.GenesisAccount){
		"zero balance": func(a *evmonly.GenesisAccount) { a.Balance = uint256.NewInt(0) },
		"nonce":        func(a *evmonly.GenesisAccount) { a.Nonce++ },
		"code":         func(a *evmonly.GenesisAccount) { a.Code = []byte{0x00} },
		"storage":      func(a *evmonly.GenesisAccount) { a.Storage = map[common.Hash]common.Hash{{0x02}: {0x02}} },
	}
	for name, change := range variants {
		changed := GenesisAlloc{InitialHeight: 1, Accounts: maps.Clone(base.Accounts)}
		account := changed.Accounts[common.Address{1}]
		change(&account)
		changed.Accounts[common.Address{1}] = account
		require.NotEqual(t, base.digest(), changed.digest(), name)
	}
}
