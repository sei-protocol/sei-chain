package preinstalls

import (
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

var seiMainnetChainID = big.NewInt(1329)

func accounts(t *testing.T, level string, chainID *big.Int) []Account {
	t.Helper()
	got, err := Accounts(level, chainID)
	require.NoError(t, err)
	return got
}

func accountNamed(t *testing.T, accounts []Account, name string) Account {
	t.Helper()
	for _, a := range accounts {
		if a.Name == name {
			return a
		}
	}
	t.Fatalf("no preinstall named %s", name)
	return Account{}
}

func accountNames(accounts []Account) []string {
	names := make([]string, 0, len(accounts))
	for _, a := range accounts {
		names = append(names, a.Name)
	}
	return names
}

func TestAccountsKeepMainnetCodeOnChainOne(t *testing.T) {
	for _, a := range accounts(t, Full, mainnetChainID) {
		require.Equal(t, readBytecode(a.Name), a.Code, a.Name)
	}
}

func TestAccountsMatchSeiMainnetCode(t *testing.T) {
	full := accounts(t, Full, seiMainnetChainID)
	for _, name := range []string{"Permit2", "EntryPointV08"} {
		raw, err := os.ReadFile("testdata/" + name + ".1329.hex")
		require.NoError(t, err)
		want, err := hexutil.Decode(strings.TrimSpace(string(raw)))
		require.NoError(t, err)
		require.Equal(t, want, accountNamed(t, full, name).Code, name)
	}
}

func TestAccountsAreSortedAndDistinct(t *testing.T) {
	for _, level := range Levels() {
		got := accounts(t, level, seiMainnetChainID)
		for i := 1; i < len(got); i++ {
			require.Negative(t, got[i-1].Address.Cmp(got[i].Address), level)
		}
	}
}

func TestLevelsNestInOrder(t *testing.T) {
	order := Levels()
	require.Empty(t, accounts(t, order[0], seiMainnetChainID))
	for i := 1; i < len(order); i++ {
		smaller := accountNames(accounts(t, order[i-1], seiMainnetChainID))
		larger := accountNames(accounts(t, order[i], seiMainnetChainID))
		require.Subset(t, larger, smaller, order[i])
		require.Greater(t, len(larger), len(smaller), order[i])
	}
}

func TestAccountsRefuseAnUnknownLevel(t *testing.T) {
	_, err := Accounts("everything", seiMainnetChainID)
	require.Error(t, err)
}

func TestEntryPointsHoldTheirConstructorsStorage(t *testing.T) {
	full := accounts(t, Full, seiMainnetChainID)
	notEntered := map[common.Hash]common.Hash{common.BigToHash(big.NewInt(2)): common.BigToHash(big.NewInt(1))}
	require.Equal(t, notEntered, accountNamed(t, full, "EntryPointV06").Storage)
	require.Equal(t, notEntered, accountNamed(t, full, "EntryPointV07").Storage)
	require.Empty(t, accountNamed(t, full, "EntryPointV08").Storage)
}

func TestSenderCreatorIsEntryPointsFirstCreate(t *testing.T) {
	full := accounts(t, Full, seiMainnetChainID)
	for _, v := range []string{"V06", "V07", "V08"} {
		entryPoint := accountNamed(t, full, "EntryPoint"+v)
		senderCreator := accountNamed(t, full, "SenderCreator"+v)
		require.Equal(t, crypto.CreateAddress(entryPoint.Address, 1), senderCreator.Address, v)
		require.Equal(t, uint64(2), entryPoint.Nonce, v)
	}
}
