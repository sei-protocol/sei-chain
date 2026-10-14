package parity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

const (
	// maxStateAddresses bounds the addresses whose latest state is requested.
	maxStateAddresses = 48
	// maxLogFilters bounds the addresses, and separately the topics, that
	// eth_getLogs is filtered by.
	maxLogFilters = 6
	// maxStorageReads bounds the eth_getStorageAt requests.
	maxStorageReads = 4
)

// latestTag is the block tag every state request is recorded with.
const latestTag = "latest"

// UnknownHash is the hash Record looks up as a missing transaction and block.
var UnknownHash = common.HexToHash("0x00000000000000000000000000000000000000000000000000000000deadbeef")

// Caller sends one JSON-RPC request. An error means the request could not be
// made, not that the node answered with a JSON-RPC error.
type Caller func(ctx context.Context, method string, params []json.RawMessage) (Response, error)

// Call is a request a fixture adds to the generated set, such as an eth_call
// against a contract the fixture deploys.
type Call struct {
	Method string
	Params []any
}

// RecordOptions configures Record.
type RecordOptions struct {
	// AtTip reports whether the reference node's head is the fixture's last
	// block. When it is not, requests that depend on the head are skipped.
	AtTip bool
	// Calls are recorded after the generated requests.
	Calls []Call
}

// Record sends the requests generated from f's blocks to the reference node
// and appends each with its response to f.Requests. State requests are
// recorded with the latest tag; a reference past the fixture's tip receives
// the tip's number in its place.
func Record(ctx context.Context, f *Fixture, ref Caller, opts RecordOptions) error {
	r := &recorder{ctx: ctx, fixture: f, ref: ref, atTip: opts.AtTip}
	steps := []func() error{
		r.recordChain,
		r.recordBlocks,
		r.recordTransactions,
		r.recordMisses,
		r.recordLogs,
		r.recordState,
		r.recordFees,
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	for _, call := range opts.Calls {
		if _, err := r.call(call.Method, call.Params...); err != nil {
			return err
		}
	}
	return nil
}

type recorder struct {
	ctx     context.Context
	fixture *Fixture
	ref     Caller
	atTip   bool
	// receipts holds v2's receipt for each transaction, in block order.
	receipts []receiptSummary
}

// receiptSummary is the part of a v2 receipt that later requests are derived from.
type receiptSummary struct {
	From            common.Address  `json:"from"`
	To              *common.Address `json:"to"`
	ContractAddress *common.Address `json:"contractAddress"`
	Logs            []struct {
		Address common.Address `json:"address"`
		Topics  []common.Hash  `json:"topics"`
	} `json:"logs"`
}

// call records method with params and the reference's response to it.
func (r *recorder) call(method string, params ...any) (Response, error) {
	raw := make([]json.RawMessage, len(params))
	for i, param := range params {
		bz, err := json.Marshal(param)
		if err != nil {
			return Response{}, fmt.Errorf("encode %s param %d: %w", method, i, err)
		}
		raw[i] = bz
	}
	response, err := r.ref(r.ctx, method, r.referenceParams(raw))
	if err != nil {
		return Response{}, fmt.Errorf("%s: %w", method, err)
	}
	r.fixture.Requests = append(r.fixture.Requests, Request{Method: method, Params: raw, V2: response})
	return response, nil
}

// referenceParams returns params as the reference must receive them: unchanged
// at the tip, and otherwise with each latest tag replaced by the tip's number.
func (r *recorder) referenceParams(params []json.RawMessage) []json.RawMessage {
	if r.atTip {
		return params
	}
	latest, _ := json.Marshal(latestTag)
	tip, _ := json.Marshal(r.fixture.Tip().Header.Number)
	replaced := make([]json.RawMessage, len(params))
	for i, param := range params {
		replaced[i] = param
		if bytes.Equal(param, latest) {
			replaced[i] = tip
		}
	}
	return replaced
}

func (r *recorder) recordChain() error {
	if _, err := r.call("eth_chainId"); err != nil {
		return err
	}
	if !r.atTip {
		return nil
	}
	_, err := r.call("eth_blockNumber")
	return err
}

func (r *recorder) recordBlocks() error {
	for _, block := range r.fixture.Blocks {
		number, hash := block.Header.Number, block.Header.Hash
		calls := []Call{
			{"eth_getBlockByNumber", []any{number, false}},
			{"eth_getBlockByNumber", []any{number, true}},
			{"eth_getBlockByHash", []any{hash, false}},
			{"eth_getBlockByHash", []any{hash, true}},
			{"eth_getBlockTransactionCountByNumber", []any{number}},
			{"eth_getBlockTransactionCountByHash", []any{hash}},
			{"eth_getBlockReceipts", []any{number}},
		}
		for _, c := range calls {
			if _, err := r.call(c.Method, c.Params...); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *recorder) recordTransactions() error {
	for _, block := range r.fixture.Blocks {
		for i, raw := range block.Txs {
			var tx ethtypes.Transaction
			if err := tx.UnmarshalBinary(raw); err != nil {
				return fmt.Errorf("decode block %d tx %d: %w", block.Header.Number, i, err)
			}
			index := hexutil.Uint64(i) //nolint:gosec // i indexes a block's transactions.
			if _, err := r.call("eth_getTransactionByHash", tx.Hash()); err != nil {
				return err
			}
			if _, err := r.call("eth_getTransactionByBlockNumberAndIndex", block.Header.Number, index); err != nil {
				return err
			}
			if _, err := r.call("eth_getTransactionByBlockHashAndIndex", block.Header.Hash, index); err != nil {
				return err
			}
			response, err := r.call("eth_getTransactionReceipt", tx.Hash())
			if err != nil {
				return err
			}
			var summary receiptSummary
			if response.Error == nil {
				if err := json.Unmarshal(response.Result, &summary); err != nil {
					return fmt.Errorf("decode receipt %s: %w", tx.Hash(), err)
				}
			}
			r.receipts = append(r.receipts, summary)
		}
	}
	return nil
}

// recordMisses records lookups of blocks and transactions that do not exist.
func (r *recorder) recordMisses() error {
	tip := r.fixture.Tip().Header
	calls := []Call{
		{"eth_getTransactionByHash", []any{UnknownHash}},
		{"eth_getTransactionReceipt", []any{UnknownHash}},
		{"eth_getBlockByHash", []any{UnknownHash, false}},
		{"eth_getTransactionByBlockNumberAndIndex", []any{tip.Number, hexutil.Uint64(len(r.fixture.Tip().Txs))}},
	}
	if r.atTip {
		calls = append(calls, Call{"eth_getBlockByNumber", []any{tip.Number + 1, false}})
	}
	for _, c := range calls {
		if _, err := r.call(c.Method, c.Params...); err != nil {
			return err
		}
	}
	return nil
}

func (r *recorder) recordLogs() error {
	first, tip := r.fixture.Blocks[0].Header.Number, r.fixture.Tip().Header.Number
	span := map[string]any{"fromBlock": first, "toBlock": tip}
	if _, err := r.call("eth_getLogs", span); err != nil {
		return err
	}
	for _, block := range r.fixture.Blocks {
		if _, err := r.call("eth_getLogs", map[string]any{"blockHash": block.Header.Hash}); err != nil {
			return err
		}
	}
	addresses, topics := r.logFilters()
	for _, address := range addresses {
		if _, err := r.call("eth_getLogs", map[string]any{"fromBlock": first, "toBlock": tip, "address": address}); err != nil {
			return err
		}
	}
	for _, topic := range topics {
		if _, err := r.call("eth_getLogs", map[string]any{"fromBlock": first, "toBlock": tip, "topics": [][]common.Hash{{topic}}}); err != nil {
			return err
		}
	}
	if len(addresses) > 0 && len(topics) > 0 {
		filter := map[string]any{"fromBlock": first, "toBlock": tip, "address": addresses, "topics": [][]common.Hash{topics}}
		if _, err := r.call("eth_getLogs", filter); err != nil {
			return err
		}
	}
	return nil
}

// logFilters returns the emitting addresses and first topics of the
// fixture's logs, each sorted and bounded by maxLogFilters.
func (r *recorder) logFilters() ([]common.Address, []common.Hash) {
	addresses := map[common.Address]struct{}{}
	topics := map[common.Hash]struct{}{}
	for _, receipt := range r.receipts {
		for _, log := range receipt.Logs {
			addresses[log.Address] = struct{}{}
			if len(log.Topics) > 0 {
				topics[log.Topics[0]] = struct{}{}
			}
		}
	}
	return firstSorted(addresses, common.Address.Cmp, maxLogFilters), firstSorted(topics, common.Hash.Cmp, maxLogFilters)
}

func (r *recorder) recordState() error {
	for _, address := range r.stateAddresses() {
		for _, method := range []string{"eth_getBalance", "eth_getTransactionCount", "eth_getCode"} {
			if _, err := r.call(method, address, latestTag); err != nil {
				return err
			}
		}
	}
	reads := 0
	for _, address := range firstSorted(r.fixture.Prestate, common.Address.Cmp, len(r.fixture.Prestate)) {
		storage := r.fixture.Prestate[address].Storage
		if len(storage) == 0 || reads == maxStorageReads {
			continue
		}
		slot := firstSorted(storage, common.Hash.Cmp, 1)[0]
		if _, err := r.call("eth_getStorageAt", address, slot, latestTag); err != nil {
			return err
		}
		reads++
	}
	// A historical read: the EVM-only RPC serves only the latest state.
	if len(r.receipts) > 0 {
		if _, err := r.call("eth_getBalance", r.receipts[0].From, r.fixture.Blocks[0].Header.Number); err != nil {
			return err
		}
	}
	return nil
}

// stateAddresses returns the prestate addresses and the addresses the
// fixture's transactions touched, sorted and bounded by maxStateAddresses.
func (r *recorder) stateAddresses() []common.Address {
	addresses := map[common.Address]struct{}{}
	for address := range maps.Keys(r.fixture.Prestate) {
		addresses[address] = struct{}{}
	}
	for _, receipt := range r.receipts {
		addresses[receipt.From] = struct{}{}
		for _, address := range []*common.Address{receipt.To, receipt.ContractAddress} {
			if address != nil && *address != (common.Address{}) {
				addresses[*address] = struct{}{}
			}
		}
	}
	return firstSorted(addresses, common.Address.Cmp, maxStateAddresses)
}

func (r *recorder) recordFees() error {
	blocks := hexutil.Uint64(len(r.fixture.Blocks)) //nolint:gosec // a fixture holds few blocks.
	_, err := r.call("eth_feeHistory", blocks, latestTag, []float64{25, 75})
	return err
}

// firstSorted returns up to limit keys of m in ascending order.
func firstSorted[K comparable, V any](m map[K]V, cmp func(K, K) int, limit int) []K {
	keys := slices.SortedFunc(maps.Keys(m), cmp)
	return keys[:min(limit, len(keys))]
}
