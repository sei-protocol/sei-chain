// evmshard-fixture emits a golden fixture for Committee.EvmShard: a committee
// and, for a deterministic set of EVM sender addresses, the index of the
// validator that owns each one.
//
// A client that routes transactions to the owning validator (sei-load) cannot
// import Committee, so it re-implements the rule and checks that
// re-implementation against this fixture. The golden test beside this tool
// pins the rule on this side, so a change to EvmShard fails here and prompts
// regeneration.
//
//	go run ./cmd/evmshard-fixture -weights 1,1,1,1 -chain-commit $(git rev-parse HEAD) > evmshard_uniform4.json
//	go run ./cmd/evmshard-fixture -weights 7,1,1,1 -chain-commit $(git rev-parse HEAD) > evmshard_7111.json
//
// The generator path and, when given, the chain commit are recorded in the
// fixture so a consumer can tell which rule it was pinned against.
//
// Validators are listed in derivation order, which is not the committee's sort
// order, so a consumer has to sort by key to reproduce the indices.
package main

import (
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

type validator struct {
	// ConsensusPubKey is the key in its autobahn.json text form,
	// "validator:ed25519:public:<hex>".
	ConsensusPubKey string `json:"consensusPubKey"`
	Power           uint64 `json:"power"`
}

type fixture struct {
	// Generator is the path of this tool within sei-chain.
	Generator string `json:"generator"`
	// ChainCommit is the sei-chain commit the fixture was generated at, if
	// the caller supplied one.
	ChainCommit string      `json:"chainCommit,omitempty"`
	Seed        string      `json:"seed"`
	Validators  []validator `json:"validators"`
	// Owners maps each sender address to the index into Validators of the
	// validator that owns it.
	Owners map[common.Address]int `json:"owners"`
}

func main() {
	weightsFlag := flag.String("weights", "1,1,1,1", "comma-separated voting power per validator, in derivation order")
	addresses := flag.Int("addresses", 200, "number of sender addresses to assign")
	seed := flag.String("seed", "evmshard-fixture", "string every key and address is derived from")
	chainCommit := flag.String("chain-commit", "", "sei-chain commit to record in the fixture")
	flag.Parse()
	if err := run(*weightsFlag, *addresses, *seed, *chainCommit, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

const generatorPath = "sei-tendermint/cmd/evmshard-fixture"

func run(weightsFlag string, addresses int, seed, chainCommit string, out io.Writer) error {
	var weights []uint64
	for _, s := range strings.Split(weightsFlag, ",") {
		w, err := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
		if err != nil {
			return fmt.Errorf("-weights %q: %w", weightsFlag, err)
		}
		weights = append(weights, w)
	}
	f := fixture{Generator: generatorPath, ChainCommit: chainCommit, Seed: seed, Owners: make(map[common.Address]int, addresses)}
	committeeWeights := make(map[types.PublicKey]uint64, len(weights))
	index := make(map[types.PublicKey]int, len(weights))
	for i, w := range weights {
		raw := sha256.Sum256(fmt.Appendf(nil, "%s/validator/%d", seed, i))
		key, err := types.PublicKeyFromBytes(raw[:])
		if err != nil {
			return err
		}
		committeeWeights[key] = w
		index[key] = i
		f.Validators = append(f.Validators, validator{ConsensusPubKey: key.String(), Power: w})
	}
	committee, err := types.NewCommittee(committeeWeights)
	if err != nil {
		return fmt.Errorf("NewCommittee: %w", err)
	}
	for j := range addresses {
		raw := sha256.Sum256(fmt.Appendf(nil, "%s/address/%d", seed, j))
		addr := common.BytesToAddress(raw[:20])
		f.Owners[addr] = index[committee.EvmShard(addr)]
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(f)
}
