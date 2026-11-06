// Package capture records a parity fixture from consecutive blocks a v2 node
// has already executed: the blocks, the EVM state they start from, and the
// node's responses to the generated requests.
package capture

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/sei-protocol/sei-chain/giga/evmonly/rpc/parity"
)

// customPrecompiles is the address range of Sei's custom precompiles, which
// the EVM-only executor rejects.
var customPrecompiles = [2]*big.Int{big.NewInt(0x1001), big.NewInt(0x10ff)}

// ConsensusBlock is what the consensus layer knows of a block that the EVM
// RPC does not report: its exact time and how many transactions it holds.
type ConsensusBlock struct {
	Time time.Time
	Txs  int
}

// Source is a v2 node to capture from.
type Source struct {
	// EVM is the node's v2 EVM RPC. It must serve debug_traceBlockByNumber
	// with the prestateTracer.
	EVM parity.Caller
	// Consensus returns the consensus block at a height.
	Consensus func(ctx context.Context, height uint64) (ConsensusBlock, error)
	// Coinbase is the address v2 executes blocks with as coinbase: the fee
	// collector's EVM address.
	Coinbase common.Address
}

// Fixture captures count blocks from start into a fixture named name. It
// refuses a block holding a Cosmos transaction, which can change EVM state the
// fixture cannot replay, and a block that calls a custom precompile.
func Fixture(ctx context.Context, src Source, name string, start, count uint64) (*parity.Fixture, error) {
	var chainID hexutil.Uint64
	if err := result(ctx, src.EVM, &chainID, "eth_chainId"); err != nil {
		return nil, err
	}
	fixture := &parity.Fixture{
		Name:     name,
		Source:   fmt.Sprintf("chain %d blocks %d-%d", chainID, start, start+count-1),
		ChainID:  chainID,
		Prestate: map[common.Address]parity.Account{},
	}
	for number := start; number < start+count; number++ {
		block, err := captureBlock(ctx, src, hexutil.Uint64(number))
		if err != nil {
			return nil, fmt.Errorf("block %d: %w", number, err)
		}
		if err := addPrestate(ctx, src.EVM, fixture.Prestate, block.Header.Number); err != nil {
			return nil, fmt.Errorf("block %d prestate: %w", number, err)
		}
		fixture.Blocks = append(fixture.Blocks, block)
	}
	if err := parity.Record(ctx, fixture, src.EVM, parity.RecordOptions{AtTip: false}); err != nil {
		return nil, err
	}
	return fixture, nil
}

// result calls method and decodes its result into out.
func result(ctx context.Context, evm parity.Caller, out any, method string, params ...any) error {
	raw := make([]json.RawMessage, len(params))
	for i, param := range params {
		bz, err := json.Marshal(param)
		if err != nil {
			return err
		}
		raw[i] = bz
	}
	response, err := evm(ctx, method, raw)
	if err != nil {
		return err
	}
	if response.Error != nil {
		return fmt.Errorf("%s: %s", method, response.Error.Message)
	}
	return json.Unmarshal(response.Result, out)
}

// reportedBlock is the part of a v2 eth_getBlockByNumber response a fixture
// keeps.
type reportedBlock struct {
	parity.Header
	GasLimit      hexutil.Uint64    `json:"gasLimit"`
	BaseFeePerGas *hexutil.Big      `json:"baseFeePerGas"`
	Transactions  []json.RawMessage `json:"transactions"`
}

// captureBlock returns the block at number as v2 reports and executed it.
func captureBlock(ctx context.Context, src Source, number hexutil.Uint64) (parity.Block, error) {
	var reported reportedBlock
	if err := result(ctx, src.EVM, &reported, "eth_getBlockByNumber", number, true); err != nil {
		return parity.Block{}, err
	}
	txs := make([]hexutil.Bytes, len(reported.Transactions))
	for i, txJSON := range reported.Transactions {
		raw, err := rawTransaction(txJSON)
		if err != nil {
			return parity.Block{}, fmt.Errorf("transaction %d: %w", i, err)
		}
		txs[i] = raw
	}
	consensus, err := src.Consensus(ctx, uint64(number))
	if err != nil {
		return parity.Block{}, err
	}
	if consensus.Txs != len(txs) {
		return parity.Block{}, fmt.Errorf("block holds %d transactions of which %d are EVM", consensus.Txs, len(txs))
	}
	// v2 derives PREVRANDAO from the binary encoding of the block time.
	timeBinary, err := consensus.Time.MarshalBinary()
	if err != nil {
		return parity.Block{}, err
	}
	header := reported.Header
	header.Time = consensus.Time
	return parity.Block{
		Header: header,
		Execution: parity.Execution{
			Coinbase:   src.Coinbase,
			PrevRandao: crypto.Keccak256Hash(timeBinary),
			GasLimit:   reported.GasLimit,
			// v2 reports the base fee it executed the block at.
			BaseFee:     reported.BaseFeePerGas,
			BlobBaseFee: (*hexutil.Big)(big.NewInt(1)),
		},
		Txs: txs,
	}, nil
}

// rawTransaction re-encodes an RPC transaction object as raw RLP, checking
// that it hashes to the hash the node reported.
func rawTransaction(txJSON json.RawMessage) (hexutil.Bytes, error) {
	var reported struct {
		Hash common.Hash `json:"hash"`
	}
	if err := json.Unmarshal(txJSON, &reported); err != nil {
		return nil, err
	}
	var tx ethtypes.Transaction
	if err := tx.UnmarshalJSON(txJSON); err != nil {
		return nil, fmt.Errorf("%s is not an Ethereum transaction: %w", reported.Hash, err)
	}
	if tx.Hash() != reported.Hash {
		return nil, fmt.Errorf("%s re-encodes to hash %s", reported.Hash, tx.Hash())
	}
	return tx.MarshalBinary()
}

// prestateAccount is one account of a prestateTracer result.
type prestateAccount struct {
	Balance *hexutil.Big                `json:"balance"`
	Nonce   uint64                      `json:"nonce"`
	Code    hexutil.Bytes               `json:"code"`
	Storage map[common.Hash]common.Hash `json:"storage"`
}

// addPrestate adds to prestate the state of each account and slot the block
// touches, as it was before the first transaction that touched it.
func addPrestate(ctx context.Context, evm parity.Caller, prestate map[common.Address]parity.Account, number hexutil.Uint64) error {
	var traces []struct {
		TxHash common.Hash                         `json:"txHash"`
		Result map[common.Address]*prestateAccount `json:"result"`
	}
	if err := result(ctx, evm, &traces, "debug_traceBlockByNumber", number, map[string]any{"tracer": "prestateTracer"}); err != nil {
		return err
	}
	for _, trace := range traces {
		for address, account := range trace.Result {
			if isCustomPrecompile(address) {
				return fmt.Errorf("transaction %s calls custom precompile %s", trace.TxHash, address)
			}
			mergeAccount(prestate, address, account)
		}
	}
	return nil
}

// mergeAccount adds account to prestate, keeping what an earlier transaction
// already recorded for the address and for each of its slots.
func mergeAccount(prestate map[common.Address]parity.Account, address common.Address, account *prestateAccount) {
	merged, seen := prestate[address]
	if !seen {
		balance := account.Balance
		if balance == nil {
			balance = new(hexutil.Big)
		}
		merged = parity.Account{Balance: balance, Nonce: hexutil.Uint64(account.Nonce), Code: account.Code}
	}
	for slot, value := range account.Storage {
		if merged.Storage == nil {
			merged.Storage = map[common.Hash]common.Hash{}
		}
		if _, ok := merged.Storage[slot]; !ok {
			merged.Storage[slot] = value
		}
	}
	prestate[address] = merged
}

func isCustomPrecompile(address common.Address) bool {
	value := new(big.Int).SetBytes(address.Bytes())
	return value.Cmp(customPrecompiles[0]) >= 0 && value.Cmp(customPrecompiles[1]) <= 0
}
