package evmonly

// Pins the gov precompile running through the executor on real transactions.

import (
	"crypto/ecdsa"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/gov"
)

// genesisState lets genesis writers that take a precompiles.State seed a MemoryState.
type genesisState struct {
	*MemoryState
}

func (g genesisState) AddBalance(addr common.Address, v *big.Int) {
	g.SetBalance(addr, new(big.Int).Add(g.GetBalance(addr), v))
}

func (g genesisState) SubBalance(addr common.Address, v *big.Int) error {
	g.SetBalance(addr, new(big.Int).Sub(g.GetBalance(addr), v))
	return nil
}

func (genesisState) Failed() bool         { return false }
func (genesisState) Snapshot() int        { panic("unsupported") }
func (genesisState) RevertToSnapshot(int) { panic("unsupported") }

func TestGovPrecompileThroughExecutor(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.HexToECDSA(behaviorAnvilKey0)
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	usei := func(n int64) *big.Int { return new(big.Int).Mul(big.NewInt(n), big.NewInt(1_000_000_000_000)) }

	state := NewMemoryState()
	state.SetBalance(sender, usei(1_000))
	params := gov.DefaultParams()
	params.VotingPeriod = uint64(100 * time.Second)
	params.ExpeditedVotingPeriod = uint64(50 * time.Second)
	params.MinDeposit = usei(10)
	params.MaxDepositPeriod = uint64(50 * time.Second)
	params.MinExpeditedDeposit = usei(20)
	require.NoError(t, gov.SetParams(genesisState{state}, params))

	govABI := gov.ABI
	submit, err := govABI.Pack(gov.SubmitTextProposalMethod, "t", "d", false)
	require.NoError(t, err)
	vote, err := govABI.Pack(gov.VoteMethod, uint64(1), gov.OptionYes)
	require.NoError(t, err)
	to := gov.Address
	txs := [][]byte{
		signLegacyTxWithGasPrice(t, key, chainID, 0, &to, usei(10), submit, 1_000_000, big.NewInt(1)),
		signLegacyTxWithGasPrice(t, key, chainID, 1, &to, big.NewInt(0), vote, 1_000_000, big.NewInt(1)),
	}

	govPrecompile := gov.Precompile{Electorate: gov.GenesisElectorate{sender: big.NewInt(1)}}
	cfg := Config{
		MinGasPrice:       big.NewInt(0),
		CustomPrecompiles: precompiles.NewStaticRegistry(map[common.Address]precompiles.Contract{gov.Address: govPrecompile}, gov.Address),
	}
	block := blockContext(chainID)
	result, err := NewExecutor(cfg, withTestState(state)).ExecuteBlock(t.Context(), BlockRequest{
		Context: block,
		Txs:     txs,
	})
	require.NoError(t, err)
	for i, tx := range result.Txs {
		require.NoError(t, tx.Err, "tx %d", i)
		require.Equal(t, uint64(1), tx.Status, "tx %d", i)
	}
	state.ApplyChangeSet(result.ChangeSet)

	require.Equal(t, usei(10), state.GetBalance(gov.DepositEscrow))
	require.Zero(t, state.GetBalance(gov.Address).Sign())

	// An empty block at the end of the voting period passes the proposal.
	afterVoting := new(big.Int).Set(state.GetBalance(sender))
	block.Number++
	block.Time += 100
	result, err = NewExecutor(cfg, withTestState(state)).ExecuteBlock(t.Context(), BlockRequest{Context: block})
	require.NoError(t, err)
	state.ApplyChangeSet(result.ChangeSet)
	require.Zero(t, state.GetBalance(gov.DepositEscrow).Sign())
	require.Equal(t, new(big.Int).Add(afterVoting, usei(10)), state.GetBalance(sender), "deposit refunded")

	store := NewMemoryStore(state)
	executor := NewExecutor(cfg, withTestStores(store, NewMemoryReceiptStore(), store.EncodeChangeSet))
	query := func(method string, args ...interface{}) ([]byte, error) {
		data, err := govABI.Pack(method, args...)
		require.NoError(t, err)
		msg := callMessage(sender, &to)
		msg.Data = data
		res, err := executor.Call(t.Context(), block, msg)
		require.NoError(t, err)
		return res.ReturnData, res.Err
	}
	call := func(method string, args ...interface{}) interface{} {
		ret, err := query(method, args...)
		require.NoError(t, err)
		out, err := govABI.Methods[method].Outputs.Unpack(ret)
		require.NoError(t, err)
		return out[0]
	}
	_, err = query(gov.GetVoteMethod, uint64(1), sender)
	require.Error(t, err, "the tally removes counted votes")
	var prop gov.ProposalData
	abi.ConvertType(call(gov.ProposalMethod, uint64(1)), &prop)
	require.Equal(t, gov.StatusPassed, prop.Status)
	tally := prop.FinalTallyResult
	require.Equal(t, []string{"1", "0", "0", "0"}, []string{tally.Yes.String(), tally.Abstain.String(), tally.No.String(), tally.NoWithVeto.String()})
}

// anvilKeys are anvil accounts 0-4 of the test mnemonic.
var anvilKeys = []string{
	behaviorAnvilKey0,
	"59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d",
	"5de4111afa1a4b94908f83103eb1f1706367c2e68ca870fc3fb9a804cdab365a",
	"7c852118294e51e653712a81e05800f419141751be58f605c371e15141b007a6",
	"47e179ec197488593b187f80a00eb0da91f1b9d0b13f8733639f19c30a34926a",
}

func TestGovPrecompileOCCMatchesSequential(t *testing.T) {
	chainID := big.NewInt(testChainID)
	usei := func(n int64) *big.Int { return new(big.Int).Mul(big.NewInt(n), big.NewInt(1_000_000_000_000)) }
	keys := make([]*ecdsa.PrivateKey, len(anvilKeys))
	nonces := make([]uint64, len(anvilKeys))
	for i, hex := range anvilKeys {
		key, err := crypto.HexToECDSA(hex)
		require.NoError(t, err)
		keys[i] = key
	}
	to := gov.Address
	var txs [][]byte
	send := func(i int, value *big.Int, method string, args ...interface{}) {
		data, err := gov.ABI.Pack(method, args...)
		require.NoError(t, err)
		txs = append(txs, signLegacyTxWithGasPrice(t, keys[i], chainID, nonces[i], &to, value, data, 2_000_000, big.NewInt(1)))
		nonces[i]++
	}
	send(0, usei(10), gov.SubmitTextProposalMethod, "t", "d", false) // 1 opens voting
	send(1, usei(4), gov.SubmitTextProposalMethod, "t", "d", false)  // 2 waits for deposits
	send(2, usei(3), gov.DepositMethod, uint64(2))
	send(0, nil, gov.VoteMethod, uint64(1), gov.OptionYes)
	send(3, usei(3), gov.DepositMethod, uint64(2)) // 2 opens voting
	send(1, nil, gov.VoteMethod, uint64(1), gov.OptionNo)
	send(4, nil, gov.VoteMethod, uint64(2), gov.OptionYes)
	send(2, nil, gov.VoteMethod, uint64(1), gov.OptionAbstain)
	send(3, usei(1), gov.DepositMethod, uint64(1))
	send(4, nil, gov.VoteMethod, uint64(1), gov.OptionYes)

	run := func(workers int) *BlockResult {
		state := NewMemoryState()
		for _, key := range keys {
			state.SetBalance(crypto.PubkeyToAddress(key.PublicKey), usei(1_000))
		}
		params := gov.DefaultParams()
		params.VotingPeriod, params.ExpeditedVotingPeriod, params.MaxDepositPeriod = uint64(100*time.Second), uint64(50*time.Second), uint64(50*time.Second)
		params.MinDeposit, params.MinExpeditedDeposit = usei(10), usei(40)
		require.NoError(t, gov.SetParams(genesisState{state}, params))
		registry := precompiles.NewStaticRegistry(map[common.Address]precompiles.Contract{gov.Address: gov.Precompile{}}, gov.Address)
		result, err := NewExecutor(Config{MinGasPrice: big.NewInt(0), OCCWorkers: workers, CustomPrecompiles: registry}, withTestState(state)).
			ExecuteBlock(t.Context(), BlockRequest{Context: blockContext(chainID), Txs: txs})
		require.NoError(t, err)
		return result
	}
	sequential, occ := run(1), run(4)
	require.True(t, occ.OCCStats.Attempted)
	require.False(t, occ.OCCStats.Fallback, occ.OCCStats.FallbackReason)
	require.Positive(t, occ.OCCStats.RerunCount, "the block's gov txs conflict")
	for i, tx := range sequential.Txs {
		require.Equal(t, uint64(1), tx.Status, "tx %d", i)
	}
	require.Equal(t, sequential.GasUsed, occ.GasUsed)
	require.Equal(t, sequential.Txs, occ.Txs)
	require.Equal(t, sequential.Receipts, occ.Receipts)
	require.Equal(t, sequential.ChangeSet, occ.ChangeSet)
}

func TestRetiredGovMethodRefundsGasAndValue(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.HexToECDSA(behaviorAnvilKey0)
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	start := new(big.Int).Mul(big.NewInt(1_000), big.NewInt(1_000_000_000_000_000))
	value := big.NewInt(10_000_000_000_000)

	state := NewMemoryState()
	state.SetBalance(sender, start)
	data, err := gov.ABI.Pack("submitProposal", `{"title":"t","description":"d","type":"Text"}`)
	require.NoError(t, err)
	to := gov.Address
	const gasLimit = 1_000_000
	tx := signLegacyTxWithGasPrice(t, key, chainID, 0, &to, value, data, gasLimit, big.NewInt(1))
	cfg := Config{
		MinGasPrice:       big.NewInt(0),
		CustomPrecompiles: precompiles.NewStaticRegistry(map[common.Address]precompiles.Contract{gov.Address: gov.Precompile{}}, gov.Address),
	}
	result, err := NewExecutor(cfg, withTestState(state)).ExecuteBlock(t.Context(), BlockRequest{Context: blockContext(chainID), Txs: [][]byte{tx}})
	require.NoError(t, err)
	got := result.Txs[0]
	require.Equal(t, uint64(0), got.Status)
	require.Less(t, got.GasUsed, uint64(gasLimit/10), "a retired method's revert refunds unused gas")
	state.ApplyChangeSet(result.ChangeSet)
	spent := new(big.Int).Mul(new(big.Int).SetUint64(got.GasUsed), big.NewInt(1))
	require.Equal(t, new(big.Int).Sub(start, spent), state.GetBalance(sender), "the value comes back; only gas is spent")
	require.Zero(t, state.GetBalance(gov.Address).Sign())
}

// callcodeDeposit returns runtime code that CALLCODEs gov.deposit(id) with value
// wei and stores the call's success flag in successSlot.
func callcodeDeposit(id uint64, value *big.Int, successSlot common.Hash) []byte {
	data, err := gov.ABI.Pack("deposit", id)
	if err != nil {
		panic(err)
	}
	var head, tail common.Hash
	copy(head[:], data[:4]) // selector in the high bytes of word 0
	copy(tail[:], data[4:]) // the uint64 argument, already left-padded
	code := appendPush32(nil, head)
	code = appendPush1(code, 0)
	code = append(code, 0x52) // MSTORE
	code = appendPush32(code, tail)
	code = appendPush1(code, 4)
	code = append(code, 0x52) // MSTORE
	code = appendPush1(code, 0)
	code = appendPush1(code, 0)
	code = appendPush1(code, 36)
	code = appendPush1(code, 0)
	code = appendPush32(code, common.BigToHash(value))
	code = appendPush20(code, gov.Address)
	code = appendPush32(code, common.BigToHash(big.NewInt(900_000))) // gas enough to finish the deposit
	code = append(code, 0xf2)                                        // CALLCODE
	code = appendPush32(code, successSlot)
	return append(code, 0x55, 0x00)
}

// CALLCODE hands a precompile a non-zero value that no transfer backed, so gov's
// delegate-call guard is what keeps a deposit from being minted out of gov's
// own balance.
func TestCallcodeDepositIsRejected(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.HexToECDSA(behaviorAnvilKey0)
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	caller := testAddress(0xca)
	successSlot := testHash(0x62)
	usei := func(n int64) *big.Int { return new(big.Int).Mul(big.NewInt(n), big.NewInt(1_000_000_000_000)) }
	value := usei(5)

	state := NewMemoryState()
	state.SetBalance(sender, usei(1_000_000))
	state.SetBalance(caller, value)      // CanTransfer is checked against the caller
	state.SetBalance(gov.Address, value) // a stray donation sitting at the precompile
	state.SetCode(caller, callcodeDeposit(1, value, successSlot))
	params := gov.DefaultParams()
	params.MinDeposit = usei(100)
	params.MinExpeditedDeposit = usei(200)
	require.NoError(t, gov.SetParams(genesisState{state}, params))
	submit, err := gov.ABI.Pack(gov.SubmitTextProposalMethod, "t", "d", false)
	require.NoError(t, err)
	to := gov.Address
	txs := [][]byte{
		signLegacyTxWithGasPrice(t, key, chainID, 0, &to, usei(1), submit, 1_000_000, big.NewInt(1)),
		signLegacyTxWithGasPrice(t, key, chainID, 1, &caller, big.NewInt(0), nil, 1_000_000, big.NewInt(1)),
	}

	cfg := Config{
		MinGasPrice:       big.NewInt(0),
		CustomPrecompiles: precompiles.NewStaticRegistry(map[common.Address]precompiles.Contract{gov.Address: gov.Precompile{}}, gov.Address),
	}
	result, err := NewExecutor(cfg, withTestState(state)).ExecuteBlock(t.Context(), BlockRequest{Context: blockContext(chainID), Txs: txs})
	require.NoError(t, err)
	state.ApplyChangeSet(result.ChangeSet)
	require.Equal(t, uint64(1), result.Txs[0].Status, "proposal 1 is open for deposits")
	require.Equal(t, uint64(1), result.Txs[1].Status, "the outer frame succeeds")
	require.Equal(t, common.Hash{}, state.GetState(caller, successSlot), "the CALLCODE sub-call failed")
	require.Equal(t, value, state.GetBalance(gov.Address), "the donation is untouched")
	require.Equal(t, usei(1), state.GetBalance(gov.DepositEscrow), "only the submit deposit reached escrow")
}
