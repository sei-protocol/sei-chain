package evmonly

import "github.com/ethereum/go-ethereum/common"

// occPath is the way a block runs once the executor has chosen to run it in parallel.
type occPath int

const (
	// occPathAuto runs the block speculatively and chooses how to finish it from the block's measured
	// dependencies.
	occPathAuto occPath = iota
	// occPathFrontier validates the speculative results in block order, rerunning each one that does not hold.
	occPathFrontier
	// occPathSequential executes the block on the sequential path.
	occPathSequential
	// occPathBlockSTM executes the block under Block-STM and hands its results to the frontier.
	occPathBlockSTM
)

// occDependencySample is how many of a block's transactions run speculatively before the executor chooses how
// to finish the block. It is enough transactions for chains through shared keys to show, few enough that the
// pass is cheap when the block then runs on another path, and the frontier keeps the results.
const occDependencySample = 256

// occDependencyRun is how many consecutive transactions each piece of the sample holds, so that chains between
// neighbouring transactions show in the sample as they do in the block.
const occDependencyRun = 16

// occDependencySpans returns the transactions of a block of n that run speculatively before the executor
// chooses how to finish it: the whole block when it holds at most occDependencySample transactions, and
// otherwise runs of occDependencyRun consecutive transactions spaced evenly from the block's first transaction
// to its last, so that a block whose dependent transactions sit in one part of it is measured as a whole.
func occDependencySpans(n int) []occTxRange {
	if n <= occDependencySample {
		return []occTxRange{{start: 0, end: n}}
	}
	runs := occDependencySample / occDependencyRun
	spans := make([]occTxRange, runs)
	for i := range spans {
		start := i * (n - occDependencyRun) / (runs - 1)
		spans[i] = occTxRange{start: start, end: start + occDependencyRun}
	}
	return spans
}

// occSpansOutside returns the transactions of a block of n outside spans, which are in block order and do not
// overlap.
func occSpansOutside(spans []occTxRange, n int) []occTxRange {
	var outside []occTxRange
	next := 0
	for _, span := range spans {
		if span.start > next {
			outside = append(outside, occTxRange{start: next, end: span.start})
		}
		next = span.end
	}
	if next < n {
		outside = append(outside, occTxRange{start: next, end: n})
	}
	return outside
}

// occSampledResults returns the speculative results in spans, in block order.
func occSampledResults(results []occTxExecution, spans []occTxRange) []occTxExecution {
	if len(spans) == 1 {
		return results[spans[0].start:spans[0].end]
	}
	sampled := make([]occTxExecution, 0, occDependencySample)
	for _, span := range spans {
		sampled = append(sampled, results[span.start:span.end]...)
	}
	return sampled
}

// occDependentShare is the share of a block's speculative results, as a divisor, that must read a key a lower
// result wrote for the block to leave the frontier. The frontier reruns each such result on its own, one
// after another, with a parallel validation pass between reruns, while Block-STM runs a block without such
// reads about as fast as the frontier does, so a few of them are enough to prefer it. A read shows in the
// sample only when its writer was sampled too, so the sample holds fewer of them than the block does.
const occDependentShare = 64

// occBlockSTMMinParallelism is the fewest transactions per link of the longest chain of reads of lower writes
// at which a block that leaves the frontier runs under Block-STM rather than sequentially. Block-STM
// finishes no sooner than that chain executes in order, and each link costs it an abort or a wait on the
// link below, so below this parallelism the sequential path finishes first.
const occBlockSTMMinParallelism = 4

// occChainedParallelism is the parallelism Block-STM needs instead when at least occChainedShare of the
// block's transactions read a lower write: nearly every execution then risks an abort, and the waits and
// re-executions grow with the chains' fan-in, which the depth alone does not show. The sampled chains are
// shorter than the block's, since the sample skips links, so the parallelism they show is higher than the
// block's.
const occChainedParallelism = 48

// occChainedShare is that share, in eighths.
const occChainedShare = 3

// routeAfterSpeculation returns the path a block takes, from how the speculative results of its sampled
// transactions depend on each other. A block smaller than one parallel validation pass stays on the frontier,
// where a rerun costs too little to matter.
func routeAfterSpeculation(results []occTxExecution) occPath {
	n := len(results)
	if n < occMinParallelValidation {
		return occPathFrontier
	}
	deps := measureDependencies(results)
	chained := deps.dependent*8 >= n*occChainedShare
	switch {
	case deps.dependent*occDependentShare < n:
		return occPathFrontier
	case n >= deps.depth*occChainedParallelism:
		return occPathBlockSTM
	case !chained && n >= deps.depth*occBlockSTMMinParallelism:
		return occPathBlockSTM
	default:
		return occPathSequential
	}
}

// occDependencies is how a block's speculative results depend on each other through the keys they read.
type occDependencies struct {
	// dependent counts the results that read a key a lower result wrote.
	dependent int
	// depth is the most results on one chain of such reads in block order, so a block of independent
	// transactions has depth 1 and a block that must run in order has depth len(results).
	depth int
}

// measureDependencies walks the speculative results in block order and records, for every key written so far,
// the deepest chain that wrote it. A read of a key is matched to its writers the way stateAccessIndex matches
// conflicts: an account-level write covers every key of the address, and an account-level read sees every
// write to the address other than its storage. Fee credits to the coinbase are commutative deltas outside the
// write sets: they chain nothing between transactions that only credit the coinbase, and, as in
// stateAccessIndex, a non-zero credit is a write that a later read of the credited address's balance or account
// sees.
func measureDependencies(results []occTxExecution) occDependencies {
	exact := make(map[stateAccessKey]int, 4*len(results))
	account := map[common.Address]int{}
	touched := make(map[common.Address]int, 2*len(results))
	credited := map[common.Address]int{}
	var deps occDependencies
	for i := range results {
		below := 0
		for key := range results[i].readSet {
			below = max(below, exact[key], account[key.address])
			if key.kind == stateAccessAccount {
				below = max(below, touched[key.address])
			}
			if key.kind == stateAccessAccount || key.kind == stateAccessBalance {
				below = max(below, credited[key.address])
			}
		}
		if below > 0 {
			deps.dependent++
		}
		depth := below + 1
		deps.depth = max(deps.depth, depth)
		for key := range dependencyWrites(&results[i]) {
			exact[key] = max(exact[key], depth)
			if key.kind != stateAccessStorage {
				touched[key.address] = max(touched[key.address], depth)
			}
			if key.kind == stateAccessAccount {
				account[key.address] = max(account[key.address], depth)
			}
		}
		for addr, delta := range results[i].commutativeBalanceDeltas {
			if delta != nil && delta.Sign() != 0 {
				credited[addr] = max(credited[addr], depth)
			}
		}
	}
	return deps
}

// dependencyWrites returns the keys a speculative result passes on to later readers: its writes, or, for a
// result whose speculative execution failed and so wrote nothing, every key it read.
func dependencyWrites(result *occTxExecution) map[stateAccessKey]struct{} {
	if result.err != nil {
		return result.readSet
	}
	return result.writeSet
}
