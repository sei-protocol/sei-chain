package ordering

import (
	"encoding/json"
	"math/rand/v2"
	"os"
	"strconv"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

// jsonUint64 accepts a JSON number or a decimal string.
type jsonUint64 uint64

func (n *jsonUint64) UnmarshalJSON(b []byte) error {
	if s, err := strconv.Unquote(string(b)); err == nil {
		b = []byte(s)
	}
	v, err := strconv.ParseUint(string(b), 10, 64)
	*n = jsonUint64(v)
	return err
}

type vectorCase struct {
	Name  string
	Input struct {
		Txs []struct {
			Hash   common.Hash
			Sender common.Address
			Nonce  jsonUint64
			Tip    string
		}
		NextNonces []struct {
			Sender common.Address
			Nonce  jsonUint64
		} `json:"next_nonces"`
	}
	Output struct {
		Valid bool
		Order []common.Hash
	}
}

func nonces(m map[common.Address]uint64) func(common.Address) (uint64, bool) {
	return func(a common.Address) (uint64, bool) { n, ok := m[a]; return n, ok }
}

func hashes(txs []Tx, order []int) []common.Hash {
	out := make([]common.Hash, len(order))
	for i, j := range order {
		out[i] = txs[j].Hash
	}
	return out
}

// TestVectors checks the cases of specs vectors/03-ordering.json.
func TestVectors(t *testing.T) {
	raw, err := os.ReadFile("testdata/03-ordering.json")
	require.NoError(t, err)
	var file struct{ Cases []vectorCase }
	require.NoError(t, json.Unmarshal(raw, &file))
	require.NotEmpty(t, file.Cases)
	for _, c := range file.Cases {
		t.Run(c.Name, func(t *testing.T) {
			var txs []Tx
			for _, tx := range c.Input.Txs {
				tip, err := uint256.FromDecimal(tx.Tip)
				require.NoError(t, err)
				txs = append(txs, Tx{Hash: tx.Hash, Sender: tx.Sender, Nonce: uint64(tx.Nonce), Tip: *tip})
			}
			next := map[common.Address]uint64{}
			for _, n := range c.Input.NextNonces {
				next[n.Sender] = uint64(n.Nonce)
			}
			order, err := Order(txs, nonces(next))
			if !c.Output.Valid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, c.Output.Order, hashes(txs, order))
		})
	}
}

func TestOrderIgnoresInputOrder(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for range 500 {
		var txs []Tx
		for range rng.IntN(24) {
			s, n, tip := byte(rng.IntN(4)), uint64(rng.IntN(4)), uint64(rng.IntN(4))
			txs = append(txs, Tx{
				Hash:   common.Hash{s, byte(n), byte(tip)},
				Sender: common.Address{s},
				Nonce:  n,
				Tip:    *uint256.NewInt(tip),
			})
		}
		zero := func(common.Address) (uint64, bool) { return 0, true }
		want, err := Order(txs, zero)
		require.NoError(t, err)
		shuffled := append([]Tx(nil), txs...)
		rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		got, err := Order(shuffled, zero)
		require.NoError(t, err)
		require.Equal(t, hashes(txs, want), hashes(shuffled, got))
	}
}
