//go:build inprocess

package inprocess

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"time"
)

// Upgrade orchestration for the subprocess backend: submit a gov software-upgrade
// proposal, vote it through, and wait for it to pass, all via the `seid` CLI against
// the running cluster. Paired with Restart(i, WithEnv("UPGRADE_VERSION_LIST=...")),
// this drives the upgrade suites — a node whose UPGRADE_VERSION_LIST registers the
// scheduled upgrade applies it at the plan height; a node without it panics
// "UPGRADE NEEDED" (see app/upgrades.go). Requires Options.GovVotingPeriod > 0 so
// the proposal passes within the test.

// Deposit and fee are both in usei — sei-chain's bond denom (and gov min-deposit
// denom) is usei, which is what the harness funds each operator with. The deposit
// exceeds the default gov min (10000000usei) so the proposal enters voting
// immediately; the fee clears the chain's usei minimum fee.
const (
	upgradeDeposit = "20000000usei"
	upgradeFee     = "200000usei"
	upgradeGas     = "2000000"

	// Repeated seid CLI tokens, hoisted so goconst doesn't trip on the duplicates.
	subcmdGov  = "gov"
	flagOutput = "--output"
	outputJSON = "json"

	// proposalPollInterval is WaitProposalPassed's status-poll cadence — slower than
	// the in-memory probeInterval because each poll spawns a seid query.
	proposalPollInterval = time.Second
)

// SubmitUpgradeProposal submits a MINOR software-upgrade proposal (named name,
// scheduled at upgradeHeight) from node 0's operator key and returns the new proposal
// id. The proposal enters voting immediately (the deposit clears the gov minimum);
// call VoteYes then WaitProposalPassed to carry it through.
//
// Minor release semantics are what the upgrade suite needs (see sei-cosmos x/upgrade
// BeginBlocker): a node whose binary already registered the handler keeps running
// before the plan height and applies at it, while a node without the handler panics
// "UPGRADE NEEDED" at the height. (A major upgrade instead panics "BINARY UPDATED
// BEFORE TRIGGER" on any node holding the handler early — not modeled here.)
func (sn *SubprocessNetwork) SubmitUpgradeProposal(ctx context.Context, name string, upgradeHeight int64) (string, error) {
	before := sn.maxProposalID(ctx) // 0 when none exist yet

	out, err := sn.seidCmd(ctx, 0,
		"tx", subcmdGov, "submit-proposal", "software-upgrade", name,
		"--title", name, "--description", "harness upgrade test",
		"--upgrade-height", strconv.FormatInt(upgradeHeight, 10),
		"--upgrade-info", `{"upgradeType":"minor"}`,
		"--deposit", upgradeDeposit,
		"--from", operatorKeyName, "--keyring-backend", "test",
		"--chain-id", sn.net.opts.ChainID,
		"--gas", upgradeGas, "--fees", upgradeFee,
		"-b", "sync", "-y", flagOutput, outputJSON,
	)
	if err != nil {
		return "", err
	}
	if err := checkTxCode(out); err != nil {
		return "", fmt.Errorf("submit-proposal: %w", err)
	}

	// -b sync returns before inclusion, so poll gov state until the new proposal
	// appears (id past the pre-submit max).
	tick := time.NewTicker(probeInterval)
	defer tick.Stop()
	for {
		if id := sn.maxProposalID(ctx); id > before {
			return strconv.Itoa(id), nil
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("proposal did not appear: %w", ctx.Err())
		case <-tick.C:
		}
	}
}

// VoteYes casts a yes vote from every validator's operator key. All validators
// voting clears quorum + threshold. The statesync node (if any) is unbonded and is
// skipped.
func (sn *SubprocessNetwork) VoteYes(ctx context.Context, proposalID string) error {
	for i, n := range sn.net.nodes {
		if n.moniker == statesyncNodeMoniker {
			continue
		}
		out, err := sn.seidCmd(ctx, i,
			"tx", subcmdGov, "vote", proposalID, "yes",
			"--from", operatorKeyName, "--keyring-backend", "test",
			"--chain-id", sn.net.opts.ChainID,
			"--gas", upgradeGas, "--fees", upgradeFee,
			"-b", "sync", "-y", flagOutput, outputJSON,
		)
		if err != nil {
			return fmt.Errorf("vote from %s: %w", n.moniker, err)
		}
		if err := checkTxCode(out); err != nil {
			return fmt.Errorf("vote from %s: %w", n.moniker, err)
		}
	}
	return nil
}

// WaitProposalPassed polls proposal status until PROPOSAL_STATUS_PASSED, or fails
// fast on a terminal rejected/failed status, or ctx fires.
func (sn *SubprocessNetwork) WaitProposalPassed(ctx context.Context, proposalID string) error {
	tick := time.NewTicker(proposalPollInterval)
	defer tick.Stop()
	for {
		if status, ok := sn.proposalStatus(ctx, proposalID); ok {
			switch status {
			case "PROPOSAL_STATUS_PASSED":
				return nil
			case "PROPOSAL_STATUS_REJECTED", "PROPOSAL_STATUS_FAILED":
				return fmt.Errorf("proposal %s ended %s (not passed)", proposalID, status)
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("proposal %s did not pass before deadline: %w", proposalID, ctx.Err())
		case <-tick.C:
		}
	}
}

// maxProposalID returns the highest gov proposal id, or 0 when none exist / the
// query is momentarily unavailable (the caller polls).
func (sn *SubprocessNetwork) maxProposalID(ctx context.Context) int {
	out, err := sn.seidCmd(ctx, 0, "query", subcmdGov, "proposals", flagOutput, outputJSON)
	if err != nil {
		return 0 // "no proposals found" is a non-zero exit; treat as none
	}
	var r struct {
		Proposals []struct {
			ID string `json:"proposal_id"`
		} `json:"proposals"`
	}
	if json.Unmarshal([]byte(out), &r) != nil {
		return 0
	}
	highest := 0
	for _, p := range r.Proposals {
		if id, err := strconv.Atoi(p.ID); err == nil && id > highest {
			highest = id
		}
	}
	return highest
}

// proposalStatus reads one proposal's status. ok=false on an unavailable query.
func (sn *SubprocessNetwork) proposalStatus(ctx context.Context, proposalID string) (string, bool) {
	out, err := sn.seidCmd(ctx, 0, "query", subcmdGov, "proposal", proposalID, flagOutput, outputJSON)
	if err != nil {
		return "", false
	}
	// The status is top-level in this gov version (the docker suite reads .status);
	// tolerate a nested {"proposal":{...}} shape too.
	var r struct {
		Status   string `json:"status"`
		Proposal *struct {
			Status string `json:"status"`
		} `json:"proposal"`
	}
	if json.Unmarshal([]byte(out), &r) != nil {
		return "", false
	}
	if r.Proposal != nil && r.Proposal.Status != "" {
		return r.Proposal.Status, true
	}
	return r.Status, r.Status != ""
}

// seidCmd runs the seid binary against node i (its home for the keyring, its RPC for
// --node) and returns stdout. On a non-zero exit it returns the captured stderr in
// the error.
func (sn *SubprocessNetwork) seidCmd(ctx context.Context, i int, args ...string) (string, error) {
	n := sn.net.nodes[i]
	full := append([]string{}, args...)
	full = append(full, "--home", n.home, "--node", n.rpcAddr)
	out, err := exec.CommandContext(ctx, sn.seidBin, full...).Output() //nolint:gosec
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return string(out), fmt.Errorf("seid %s: %w: %s", args[0], err, ee.Stderr)
		}
		return string(out), err
	}
	return string(out), nil
}

// checkTxCode fails if a broadcast tx's JSON response reports a non-zero code
// (CheckTx rejection).
func checkTxCode(out string) error {
	var resp struct {
		Code   int    `json:"code"`
		RawLog string `json:"raw_log"`
	}
	if json.Unmarshal([]byte(out), &resp) == nil && resp.Code != 0 {
		return fmt.Errorf("tx rejected (code %d): %s", resp.Code, resp.RawLog)
	}
	return nil
}
