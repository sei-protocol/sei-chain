package evmonlyapp

import (
	"math/big"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/sei-tendermint/cmd/tendermint/commands/preinstalls"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

var (
	arachnidCreate2Proxy = common.HexToAddress("0x4e59b44847b379578588920cA78FbF26c0B4956C")
	permit2              = common.HexToAddress("0x000000000022D473030F116dDEE9F6B43aC78BA3")
)

// newEVMOnlyTestAppWithPreinstalls starts a chain whose alloc funds one account and holds every
// preinstall, and commits its first block.
func newEVMOnlyTestAppWithPreinstalls(t *testing.T) (*evmOnlyApplication, []preinstalls.Account) {
	t.Helper()
	full, err := preinstalls.Accounts(preinstalls.Full, new(big.Int).SetUint64(evmOnlyTestChainID))
	require.NoError(t, err)
	accounts := map[common.Address]evmonly.GenesisAccount{{0xf1}: {Balance: uint256.NewInt(1_000)}}
	for _, account := range full {
		accounts[account.Address] = evmonly.GenesisAccount{Nonce: account.Nonce, Code: account.Code, Storage: account.Storage}
	}
	app := newEVMOnlyTestAppWithAlloc(t, GenesisAlloc{InitialHeight: 1, Accounts: accounts}).(*evmOnlyApplication)
	_, err = app.InitChain(evmOnlyTestInitChain())
	require.NoError(t, err)
	finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(1))
	return app, full
}

func TestEVMOnlyChainStartsWithItsPreinstalls(t *testing.T) {
	app, full := newEVMOnlyTestAppWithPreinstalls(t)
	for _, account := range full {
		require.Equal(t, account.Code, app.EvmCode(account.Address), account.Name)
		require.Equal(t, account.Nonce, app.EvmNonce(account.Address), account.Name)
		for key, want := range account.Storage {
			got, err := app.EvmStorageAt(account.Address, key, 1)
			require.NoError(t, err)
			require.Equal(t, want, got, account.Name)
		}
	}
}

func TestEVMOnlyPreinstalledCreate2ProxyDeploys(t *testing.T) {
	app, _ := newEVMOnlyTestAppWithPreinstalls(t)
	salt := common.Hash{0x01}
	init := initCode([]byte{0x00})
	msg := callMessage(common.Address{0xf1}, &arachnidCreate2Proxy)
	msg.Data = append(salt.Bytes(), init...)

	result, err := app.EvmCall(t.Context(), msg, proxy.CallOverrides{})

	require.NoError(t, err)
	require.False(t, result.Failed(), "%v", result.Err)
	require.Equal(t, crypto.CreateAddress2(arachnidCreate2Proxy, salt, crypto.Keccak256(init)).Bytes(), result.ReturnData)
}

func TestEVMOnlyPreinstalledPermit2HasTheChainsDomainSeparator(t *testing.T) {
	app, _ := newEVMOnlyTestAppWithPreinstalls(t)
	msg := callMessage(common.Address{0xf1}, &permit2)
	msg.Data = crypto.Keccak256([]byte("DOMAIN_SEPARATOR()"))[:4]

	result, err := app.EvmCall(t.Context(), msg, proxy.CallOverrides{})

	require.NoError(t, err)
	require.False(t, result.Failed(), "%v", result.Err)
	typeHash := crypto.Keccak256([]byte("EIP712Domain(string name,uint256 chainId,address verifyingContract)"))
	nameHash := crypto.Keccak256([]byte("Permit2"))
	chainID := common.BigToHash(new(big.Int).SetUint64(evmOnlyTestChainID))
	want := crypto.Keccak256(typeHash, nameHash, chainID[:], common.BytesToHash(permit2[:]).Bytes())
	require.Equal(t, want, result.ReturnData)
}
