// Command paritycapture records a parity fixture from consecutive blocks of a
// v2 archive node, for the EVM-only RPC's parity test to replay.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/rpc/parity"
	"github.com/sei-protocol/sei-chain/giga/evmonly/rpc/parity/capture"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
)

// requestTimeout bounds each request to the archive node.
const requestTimeout = 2 * time.Minute

func main() {
	evmRPC := flag.String("evm-rpc", "", "v2 EVM JSON-RPC URL of an archive node that serves debug_traceBlockByNumber")
	tendermintRPC := flag.String("tendermint-rpc", "", "Tendermint RPC URL of the same node")
	start := flag.Uint64("block", 0, "first block of the fixture")
	count := flag.Uint64("count", 1, "number of consecutive blocks in the fixture")
	name := flag.String("name", "", "fixture name (default chain<id>-<block>)")
	coinbase := flag.String("coinbase", keeper.GetCoinbaseAddress().Hex(), "EVM address of the fee collector, which v2 executes blocks with as coinbase")
	out := flag.String("out", "giga/evmonly/rpc/testdata/parity", "directory to write the fixture to")
	flag.Parse()

	if *evmRPC == "" || *tendermintRPC == "" || *start == 0 || *count == 0 || !common.IsHexAddress(*coinbase) {
		flag.Usage()
		fail(errors.New("-evm-rpc, -tendermint-rpc, and -block are required"))
	}
	client := &http.Client{Timeout: requestTimeout}
	src := capture.Source{
		EVM:       parity.HTTPCaller(client, *evmRPC),
		Consensus: tendermintBlocks(client, *tendermintRPC),
		Coinbase:  common.HexToAddress(*coinbase),
	}
	fixtureName := *name
	if fixtureName == "" {
		fixtureName = fmt.Sprintf("block-%d", *start)
	}
	fixture, err := capture.Fixture(context.Background(), src, fixtureName, *start, *count)
	if err != nil {
		fail(err)
	}
	if err := parity.WriteFixture(*out, fixture); err != nil {
		fail(err)
	}
	fmt.Printf("wrote %s: %d blocks, %d accounts, %d requests\n", fixture.Name, len(fixture.Blocks), len(fixture.Prestate), len(fixture.Requests))
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "paritycapture:", err)
	os.Exit(1)
}

// tendermintBlocks reads consensus blocks from the Tendermint RPC at base.
func tendermintBlocks(client *http.Client, base string) func(context.Context, uint64) (capture.ConsensusBlock, error) {
	return func(ctx context.Context, height uint64) (capture.ConsensusBlock, error) {
		endpoint, err := url.JoinPath(base, "block")
		if err != nil {
			return capture.ConsensusBlock{}, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s?height=%d", endpoint, height), nil)
		if err != nil {
			return capture.ConsensusBlock{}, err
		}
		res, err := client.Do(req)
		if err != nil {
			return capture.ConsensusBlock{}, err
		}
		defer func() { _ = res.Body.Close() }()
		// Some nodes wrap the block in a JSON-RPC result and some do not.
		var body struct {
			Result *tendermintBlock `json:"result"`
			tendermintBlock
		}
		if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
			return capture.ConsensusBlock{}, fmt.Errorf("decode tendermint block %d: %w", height, err)
		}
		block := body.tendermintBlock
		if body.Result != nil {
			block = *body.Result
		}
		if block.Block.Header.Time.IsZero() {
			return capture.ConsensusBlock{}, fmt.Errorf("tendermint returned no block at height %d (HTTP %d)", height, res.StatusCode)
		}
		return capture.ConsensusBlock{Time: block.Block.Header.Time, Txs: len(block.Block.Data.Txs)}, nil
	}
}

// tendermintBlock is the part of a Tendermint /block response the capture reads.
type tendermintBlock struct {
	Block struct {
		Header struct {
			Time time.Time `json:"time"`
		} `json:"header"`
		Data struct {
			Txs []string `json:"txs"`
		} `json:"data"`
	} `json:"block"`
}
