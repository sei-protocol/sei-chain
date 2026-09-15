package chain

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/sei-protocol/sei-chain/admin"
	evmrpcconfig "github.com/sei-protocol/sei-chain/evmrpc/config"
	servertypes "github.com/sei-protocol/sei-chain/sei-cosmos/server/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// This file holds every guard the engine asserts. Each one covers an invariant
// that would otherwise fail silently — a network that never forms consensus, or
// a listener that quietly reintroduces the one-network-per-process limit — and
// each is called from the single function every bring-up path passes through
// rather than at the site that establishes it. doc.go names the invariants.

// validateTopology rejects a validator count CometBFT cannot bring up.
//
// N=2 deadlocks: each validator has exactly one peer, and BlockPool.IsCaughtUp
// (sei-tendermint/internal/blocksync/pool.go) requires more than one peer to
// ever report caught-up, so neither validator leaves block-sync. There is no
// config knob to opt out. N=1 runs as a solo proposer and N>=3 gives every
// validator at least two peers.
func validateTopology(n int) error {
	switch {
	case n < 1:
		return fmt.Errorf("Validators must be 1 or >= 3, got %d", n)
	case n == 2:
		return fmt.Errorf("Validators == 2 deadlocks in CometBFT block-sync (BlockPool.IsCaughtUp requires more than one peer); use 1 or >= 3")
	}
	return nil
}

// assertPeerMesh checks that collectGentxs actually populated PersistentPeers.
// For N>=2 an empty value means the in-place mutation through
// GenAppStateFromConfig did not land on the config the engine holds, and
// consensus will never form — a hang rather than an error without this guard.
func assertPeerMesh(validators []*Validator) error {
	if len(validators) < 2 {
		return nil
	}
	for _, v := range validators {
		if v.tmCfg.P2P.PersistentPeers == "" {
			return fmt.Errorf(
				"gentx-derived peer mesh not wired: collectGentxs did not populate PersistentPeers for %s — did a refactor clone or reorder the config?",
				v.moniker,
			)
		}
	}
	return nil
}

// assertGenesisConsensusParams re-reads each written genesis file and checks
// that the consensus params the engine asked for survived genesis assembly.
//
// The trap it guards is genutil.ExportGenesisFileWithTime, which builds a fresh
// DefaultConsensusParams and patches only Timeout.Commit, discarding everything
// else set earlier. The engine does not call it — GenAppStateFromConfig already
// writes the file from the doc it is handed — and this guard is what keeps that
// true if someone reaches for the helper again.
func assertGenesisConsensusParams(validators []*Validator, want *tmtypes.ConsensusParams) error {
	for _, v := range validators {
		genDoc, err := tmtypes.GenesisDocFromFile(v.tmCfg.GenesisFile())
		if err != nil {
			return fmt.Errorf("re-read genesis for %s: %w", v.moniker, err)
		}
		if genDoc.ConsensusParams == nil {
			return fmt.Errorf("%s genesis carries no consensus params", v.moniker)
		}
		if got := *genDoc.ConsensusParams; !reflect.DeepEqual(got, *want) {
			return fmt.Errorf(
				"%s genesis consensus params did not survive assembly: got %+v, want %+v — did genesis writing start going through ExportGenesisFileWithTime?",
				v.moniker, got, want,
			)
		}
	}
	return nil
}

// assertConfigInvariants checks every tendermint-config invariant at once.
// newValidatorConfig establishes them; this runs at startValidator, immediately
// before tmnode.New, so a refactor that clones or rebuilds a config still has to
// pass through here.
func assertConfigInvariants(cfg *config.Config, moniker string, n int) error {
	if cfg.Instrumentation.Prometheus {
		return fmt.Errorf("%s: Instrumentation.Prometheus is on; its fixed :26660 listener collides across validators in one process", moniker)
	}
	if !isLoopbackOrEmpty(cfg.P2P.ListenAddress) {
		return fmt.Errorf("%s: P2P.ListenAddress %q is not loopback-scoped", moniker, cfg.P2P.ListenAddress)
	}
	if !isLoopbackOrEmpty(cfg.RPC.ListenAddress) {
		return fmt.Errorf("%s: RPC.ListenAddress %q is not loopback-scoped", moniker, cfg.RPC.ListenAddress)
	}
	if !cfg.P2P.AllowDuplicateIP {
		return fmt.Errorf("%s: P2P.AllowDuplicateIP is off; every peer shares 127.0.0.1 on loopback", moniker)
	}
	if floor := uint(100 * n); cfg.P2P.MaxIncomingConnectionAttempts < floor {
		return fmt.Errorf(
			"%s: P2P.MaxIncomingConnectionAttempts = %d, want >= %d; loopback keys the router's conn-tracker on one IP, so the startup burst trips the per-IP cap",
			moniker, cfg.P2P.MaxIncomingConnectionAttempts, floor,
		)
	}
	if n >= 2 && cfg.P2P.PersistentPeers == "" {
		return fmt.Errorf("%s: P2P.PersistentPeers is empty for a %d-validator chain; consensus will never form", moniker, n)
	}
	return nil
}

// isLoopbackOrEmpty reports whether a listen address is unset or scoped to the
// IPv4 loopback. Anything else publishes an externally reachable listener.
func isLoopbackOrEmpty(addr string) bool {
	if addr == "" {
		return true
	}
	host := addr
	if _, rest, ok := strings.Cut(addr, "://"); ok {
		host = rest
	}
	return strings.HasPrefix(host, loopbackHost+":")
}

// assertEVMServingOff checks the resolved EVM and admin configs, not the intent
// the engine passed in, and requires that no listener will be constructed.
//
// This is the guard that makes many networks in one binary sound: the evmrpc
// process-global sync.Once singletons (worker pool, metrics printer) are reached
// only when an EVM listener is built, and they never cleanly re-initialize, so a
// second network would inherit dead ones. It replaces a
// one-network-per-process latch rather than living alongside it.
func assertEVMServingOff(opts servertypes.AppOptions) error {
	evmCfg, err := evmrpcconfig.ReadConfig(opts)
	if err != nil {
		return fmt.Errorf("read resolved EVM config: %w", err)
	}
	if evmCfg.HTTPEnabled || evmCfg.WSEnabled {
		return fmt.Errorf(
			"resolved EVM config serves a listener (http=%t ws=%t); that reaches the process-global evmrpc singletons and limits the process to one network",
			evmCfg.HTTPEnabled, evmCfg.WSEnabled,
		)
	}
	adminCfg, err := admin.ReadConfig(opts)
	if err != nil {
		return fmt.Errorf("read resolved admin config: %w", err)
	}
	if adminCfg.Enabled {
		return fmt.Errorf("resolved admin config serves a gRPC listener on %s; its address is fixed, so it collides across validators", adminCfg.Address)
	}
	return nil
}
