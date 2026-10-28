package gov

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

// memState is a map-backed precompiles.State and LogSink. With failAt set, the
// read after the failAt-th fails, as running out of gas does: it and every later
// read return zero, counted in readsAfterFailure.
type memState struct {
	balances map[common.Address]*big.Int
	storage  map[common.Address]map[common.Hash]common.Hash
	logs     []*ethtypes.Log

	reads, failAt, readsAfterFailure int
}

var (
	_ precompiles.State   = (*memState)(nil)
	_ precompiles.LogSink = (*memState)(nil)
)

func newMemState() *memState {
	return &memState{
		balances: map[common.Address]*big.Int{},
		storage:  map[common.Address]map[common.Hash]common.Hash{},
	}
}

func (m *memState) GetBalance(a common.Address) *big.Int {
	if b, ok := m.balances[a]; ok {
		return new(big.Int).Set(b)
	}
	return new(big.Int)
}

func (m *memState) AddBalance(a common.Address, v *big.Int) {
	m.balances[a] = new(big.Int).Add(m.GetBalance(a), v)
}

func (m *memState) SubBalance(a common.Address, v *big.Int) error {
	b := m.GetBalance(a)
	if b.Cmp(v) < 0 {
		return errors.New("insufficient balance")
	}
	m.balances[a] = b.Sub(b, v)
	return nil
}

func (m *memState) GetNonce(common.Address) uint64  { return 0 }
func (m *memState) SetNonce(common.Address, uint64) {}
func (m *memState) GetCode(common.Address) []byte   { return nil }
func (m *memState) Failed() bool                    { return m.failAt > 0 && m.reads > m.failAt }
func (m *memState) AddLog(l *ethtypes.Log)          { m.logs = append(m.logs, l) }

func (m *memState) GetState(a common.Address, k common.Hash) common.Hash {
	m.reads++
	if m.Failed() {
		m.readsAfterFailure++
		return common.Hash{}
	}
	return m.storage[a][k]
}

func (m *memState) SetState(a common.Address, k, v common.Hash) {
	if m.storage[a] == nil {
		m.storage[a] = map[common.Hash]common.Hash{}
	}
	if v == (common.Hash{}) {
		delete(m.storage[a], k)
		return
	}
	m.storage[a][k] = v
}

// abiOf returns an ABI holding the single function sig, e.g. "f(uint64,string)".
func abiOf(t *testing.T, sig string) abi.ABI {
	name, rest, _ := strings.Cut(sig, "(")
	params := strings.TrimSuffix(rest, ")")
	var inputs []string
	if params != "" {
		for i, typ := range strings.Split(params, ",") {
			inputs = append(inputs, fmt.Sprintf(`{"name":"a%d","type":%q}`, i, typ))
		}
	}
	parsed, err := abi.JSON(strings.NewReader(fmt.Sprintf(
		`[{"type":"function","name":%q,"inputs":[%s],"outputs":[]}]`, name, strings.Join(inputs, ","),
	)))
	require.NoError(t, err)
	return parsed
}
