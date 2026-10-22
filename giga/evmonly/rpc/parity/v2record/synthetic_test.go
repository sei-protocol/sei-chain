package v2record

import (
	"crypto/ecdsa"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/rpc/parity"
)

// recordEnv names the environment variable that enables recording.
const recordEnv = "EVMONLY_PARITY_RECORD"

// fixtureDir is where the EVM-only RPC's parity test reads fixtures from.
var fixtureDir = filepath.Join("..", "..", "testdata", "parity")

// TestRecordSyntheticFixtures runs each synthetic scenario on an in-process v2
// node and rewrites its fixture from v2's responses. It runs only when
// EVMONLY_PARITY_RECORD=1.
func TestRecordSyntheticFixtures(t *testing.T) {
	if os.Getenv(recordEnv) != "1" {
		t.Skipf("set %s=1 to rewrite the synthetic fixtures in %s", recordEnv, fixtureDir)
	}
	require.Empty(t, os.Getenv("CI"), "fixtures are recorded locally and reviewed, never on CI")
	chain, calls := runSyntheticBasic(t)
	require.NoError(t, parity.WriteFixture(fixtureDir, chain.record(calls)))
	// Blocks 3 and 4 sit mid-chain, so the capture reads them as historical.
	require.NoError(t, parity.WriteFixture(fixtureDir, chain.capture("captured-synthetic-basic", 3, 2)))
}

func testKey(t *testing.T, name string) *ecdsa.PrivateKey {
	key, err := crypto.ToECDSA(crypto.Keccak256([]byte("evmonly-parity-" + name)))
	require.NoError(t, err)
	return key
}

// runSyntheticBasic builds a chain covering each transaction type, contract creation by
// transaction and by CREATE2, self-destruct, logs with and without topics,
// reverts, out-of-gas, the block-context opcodes, and an empty block, and
// returns it with the calls its fixture records.
func runSyntheticBasic(t *testing.T) (*v2Chain, []parity.Call) {
	alice, bob, carol, dave := testKey(t, "alice"), testKey(t, "bob"), testKey(t, "carol"), testKey(t, "dave")
	aliceAddr, bobAddr, daveAddr := crypto.PubkeyToAddress(alice.PublicKey), crypto.PubkeyToAddress(bob.PublicKey), crypto.PubkeyToAddress(dave.PublicKey)
	carolAddr := crypto.PubkeyToAddress(carol.PublicKey)
	fresh := common.HexToAddress("0x00000000000000000000000000000000000f2e54")
	token, tokenCode := erc20(t)
	seiToWei := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)

	c := newV2Chain(t, "synthetic-basic",
		"Transfers of each type, contract deployment by transaction and CREATE2, ERC20 logs, reverts, out-of-gas, block-context opcodes, and an empty block.",
		[]*ecdsa.PrivateKey{alice, bob, dave})

	// carol is unfunded: alice's transfer creates her account.
	deployed := c.block(
		legacyTx(alice, &carolAddr, seiToWei, nil, 21_000),
		accessListTx(bob, &aliceAddr, big.NewInt(12345), nil, 30_000, ethtypes.AccessList{{Address: daveAddr, StorageKeys: []common.Hash{{0x01}}}}),
		dynamicFeeTx(dave, nil, nil, tokenCode, 2_000_000),
		dynamicFeeTx(alice, nil, nil, deployCode(contextLogger), 200_000),
		dynamicFeeTx(bob, nil, nil, deployCode(topicLogger), 200_000),
		dynamicFeeTx(dave, nil, nil, deployCode(create2Factory), 200_000),
	)
	tokenAddr := crypto.CreateAddress(daveAddr, deployed[2].Nonce())
	contextAddr := crypto.CreateAddress(aliceAddr, deployed[3].Nonce())
	topicAddr := crypto.CreateAddress(bobAddr, deployed[4].Nonce())
	factoryAddr := crypto.CreateAddress(daveAddr, deployed[5].Nonce())

	c.block(
		dynamicFeeTx(dave, &tokenAddr, seiToWei, pack(t, token, "deposit"), 200_000),
		dynamicFeeTx(dave, &tokenAddr, nil, pack(t, token, "transfer", aliceAddr, big.NewInt(1_000_000)), 200_000),
		dynamicFeeTx(dave, &tokenAddr, nil, pack(t, token, "approve", bobAddr, big.NewInt(5_000_000)), 200_000),
		dynamicFeeTx(alice, &contextAddr, nil, nil, 100_000),
		legacyTx(bob, &topicAddr, nil, []byte("parity"), 100_000),
		dynamicFeeTx(carol, &fresh, big.NewInt(1), nil, 21_000),
	)
	c.block(
		// Transferring more than alice holds reverts.
		dynamicFeeTx(alice, &tokenAddr, nil, pack(t, token, "transfer", bobAddr, seiToWei), 200_000),
		// Too little gas to finish logging the context.
		dynamicFeeTx(bob, &contextAddr, nil, nil, 21_100),
		dynamicFeeTx(dave, &factoryAddr, big.NewInt(777), deployCode(selfDestructor), 300_000),
		dynamicFeeTx(bob, &tokenAddr, nil, pack(t, token, "transferFrom", daveAddr, bobAddr, big.NewInt(2_000_000)), 200_000),
	)
	// The CREATE2 child self-destructs in a later transaction than the one that created it.
	childAddr := crypto.CreateAddress2(factoryAddr, common.Hash{}, crypto.Keccak256(deployCode(selfDestructor)))
	c.block(dynamicFeeTx(carol, &childAddr, nil, nil, 100_000))
	c.block()

	return c, []parity.Call{
		{Method: "eth_call", Params: []any{callArgs(aliceAddr, tokenAddr, pack(t, token, "balanceOf", aliceAddr)), "latest"}},
		{Method: "eth_call", Params: []any{callArgs(aliceAddr, tokenAddr, pack(t, token, "transfer", bobAddr, seiToWei)), "latest"}},
		{Method: "eth_call", Params: []any{callArgs(aliceAddr, contextAddr, nil), "latest"}},
		{Method: "eth_estimateGas", Params: []any{callArgs(daveAddr, tokenAddr, pack(t, token, "transfer", aliceAddr, big.NewInt(1))), "latest"}},
		{Method: "eth_estimateGas", Params: []any{callArgs(aliceAddr, carolAddr, nil), "latest"}},
		{Method: "eth_estimateGas", Params: []any{callArgs(aliceAddr, tokenAddr, pack(t, token, "transfer", bobAddr, seiToWei)), "latest"}},
	}
}

func callArgs(from, to common.Address, input []byte) map[string]any {
	args := map[string]any{"from": from, "to": to}
	if input != nil {
		args["input"] = hexutil.Bytes(input)
	}
	return args
}
