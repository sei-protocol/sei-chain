Call graph bounds mismatch
Medium Severity

CallGraphHarness caps depth at 8 and frames at 64, but capture config allows TRACE_MAX_DEPTH up to 32 and TRACE_MAX_FRAMES up to 256. encodeCallGraphHarness clamps depth with Math.min(8, ...) instead of truncating, which flattens deep trees into invalid specs that revert in _children, and oversized frame lists are still submitted instead of falling back to ProfileLoadHarness.

         callType > 4 ||
                (i == 0 && depth != 0) ||
                (i != 0 && depth > previousDepth + 1)
            ) revert MalformedSpec();

2- README.md:9 states "EVM and Cosmos chain IDs, user/deployment manifests, and deployed bytecode are verified before submission." That is accurate for replay:users but not for replay:run — update the claim (or, preferably, close the gap flagged inline in runReplay.ts).

3- traceCapture.ts normalizeCallTrace bounds visit recursion by maxDepth, but the countSubtree helper it calls on truncation recurses over the entire remaining subtree with no depth bound. A pathologically deep captured call trace could overflow the stack and fail a whole segment. Low likelihood; an explicit depth cap or iterative walk would remove it.

4- Consider adding a test for the Cosmos adapter (cosmosAdapters.ts). test/core.spec.ts covers the EVM adapters, config parsing, trace normalization, scheduling, and correlation well, but buildCosmosReplay — including privileged-pattern detection and the MsgSend amount-bounding fallback — has no unit coverage.

5- [blocker] The target Cosmos chain ID is never verified before the Cosmos lane broadcasts.

verifyTargetRpc only checks evmChainId against the EVM provider. The Cosmos lane goes through cosmosClient() (line 937), which calls SigningStargateClient.connectWithSigner(rpcUrl, ...) — cosmjs auto-detects the chain ID from the node and signs with it, so there is no local mismatch guard anywhere on this path.

That this is an oversight rather than a decision is clear from provisionUsers.ts:60-66, which does exactly the missing check:

const cosmosChainId = await admin.getChainId();
if (cosmosChainId !== target.cosmosChainId) { /* refuse */ }
and from README.md:9, which promises "EVM and Cosmos chain IDs … are verified before submission."

Failure scenario: an operator sets TARGET_NETWORK=arctic-1 but leaves a stale/typo'd TARGET_COSMOS_RPC pointing at a pacific-1 endpoint (the two default hostnames in config.ts differ by one label). The EVM check passes against the correct arctic-1 EVM RPC, so startup succeeds; every Cosmos-lane transaction — bank sends and shaped privileged traffic — is then signed for pacific-1 and broadcast to mainnet, funded by the same mnemonic. Because MAX_TPS defaults to 25 and runs default to 2 hours, this is sustained rather than a single stray tx.

Suggest asserting the Cosmos chain ID once at startup, next to verifyTargetRpc, so the run refuses to begin rather than discovering the mismatch per-worker.

6- [suggestion] This early return short-circuits validateCaptureManifest() and never compares the manifest's range against START_BLOCK/END_BLOCK.

A complete: true manifest is accepted unconditionally, so reusing a REPLAY_DIR after changing MAINNET_RPC/COSMOS_RPC, SEGMENT_BLOCKS, or the requested block range silently reports "Capture already complete" and exits 0 — downstream replay:run then replays stale data captured under different settings. The incomplete-manifest branch below (line 94-97) has the inverse problem: it validates the source/segment size but then adopts manifest.source.firstBlock/lastBlock, discarding a changed START_BLOCK/END_BLOCK without warning.

Suggest calling validateCaptureManifest(existingManifest) before the complete check, and rejecting (or at least logging) when an explicit START_BLOCK/END_BLOCK disagrees with the recorded range.

7- [suggestion] Batch responses are correlated to requests by array position, but nothing guarantees the array is complete.

rpcBatch sorts by id and checks for error entries, but never asserts body.length === requests.length. If an endpoint or intermediary proxy omits a single entry, responses[index] no longer lines up with batch[index], and every subsequent address gets the next address's code hash — silently written into the segment file with no error. attachCreationRuntime (line 417) has the same shape, mis-assigning deployedRuntimeCodeBytes/deployedRuntimeCodeHash across contract creations.

Those values feed adapter routing (recipientCodeHash picks the ERC4626 target in evmAdapters.ts:381-390) and encodeSyntheticCreationHarness, so corruption is silent and persistent in the corpus.

Suggest either validating the length in rpcBatch, or keying results by response.id (each request's id is already captured in the request objects) instead of relying on positional alignment.

8- [suggestion] A single appendFile failure permanently poisons this promise chain.

this.queue is reassigned to the chained promise with no .catch, so once it rejects, every later record() returns a promise that rejects without running its callback, and flush() rejects forever. Since record() is awaited in the hot loop (runReplay.ts:423, 472) and again in the finally block (line 565), one transient ENOSPC/EMFILE aborts the whole replay run and the finally rejection can mask the original error.

Suggest terminating the chain with a .catch that logs and increments a dropped-records counter, so audit-write failures degrade the audit trail rather than the run:

this.queue = this.queue.then(async () => { /* ... */ }).catch(error => {
    console.error('Bucket audit write failed:', error);
});
Related nit: initialize() writes to auditPath/unmatchedPath without mkdir -p on the parent, so a user-supplied BUCKET_AUDIT_PATH in a non-existent directory fails at startup (the default path works only because replayDirectory already exists).

9- [suggestion] A timed-out Cosmos broadcast is counted as both submitted and rejected.

The comment correctly notes the node accepted the transaction and only the poll window elapsed, but control falls through to metrics.rejected++ / recordOutcome('cosmos', 'rejected') on line 873-874, and the audit record is written with outcome 'rejected'. So submitted + rejected double-counts these, and the rejected metric — the main signal for "the target chain refused our load" — is inflated by transactions that were in fact accepted.

Suggest an early return after recording submitted (plus a distinct timeout outcome or reason in the audit record) so the two cases stay distinguishable on the dashboard.

10 - The bug: firstTimestamp is computed once, before the replay loop starts, as:

const firstTimestamp =
    selected
        .flatMap(segment => segment.blocks)
        .find(block => block.number >= nextSourceBlock)?.timestamp ??
    selected[0].source.startTimestamp;
nextSourceBlock is checkpoint.lastCompletedSourceBlock + 1 when resuming. The .find() only succeeds if the startup snapshot selected already contains a block at or beyond the resume point. If the process restarts while already caught up to the tip it had captured (a normal steady state for buffered/follow mode, since selected is just whatever was captured/retained on disk at process start, and new blocks only show up later inside the while loop via readReplaySegments), the .find() returns undefined and the fallback fires: firstTimestamp becomes selected[0].source.startTimestamp, i.e. the start of the oldest retained segment — which is earlier, sometimes much earlier, than the actual resume point.

Where it bites: later in the same loop, pacing is computed from that same fixed anchor:

const targetElapsedMs =
    ((block.timestamp - firstTimestamp) * 1_000) / TIME_SCALE;
await sleepUntil(replayStartedAt + pausedMilliseconds + targetElapsedMs);
For the first pending block after resume, block.timestamp - firstTimestamp is the full source-time span from the stale anchor to "now", not the (near-zero) span from the resume point. pausedMilliseconds doesn't cancel this out — it only tracks in-run buffering waits that have already elapsed in wall clock by the time the block is processed, so the residual stall is roughly (block.timestamp - firstTimestamp) / TIME_SCALE seconds before any replay traffic is submitted, instead of resuming immediately.

anchor the fallback to the first available/pending block near the resume point (e.g. the earliest block with number >= nextSourceBlock once segments are read inside the loop, or lastAvailableBlock's timestamp at loop start) rather than unconditionally falling back to selected[0].source.startTimestamp.

This is confined to the new load-generator tooling (integration_test/load_generator, no production chain code) and is self-healing after the one bad stall (subsequent blocks share the same anchor, so relative pacing recovers), which bounds the practical severity — but it does defeat the promptness of the "resume" / buffered-continuous feature this PR's README documents as a headline capability.

11- In syntheticDepositAmount(), the generic branch (index 0) is used for MasterChef deposit(uint256 pid, uint256 amount), but for that method amount is ABI arg index 1, not 0 — so every replayed farm deposit bounds the captured pool id (usually 0) instead of the amount, and ends up submitting a hardcoded ~1 wei deposit regardless of the source transaction's real size. Fix by special-casing the farm-deposit selector (like supply) to read decoded?.[1].
Fix: add a farm-deposit case alongside the existing supply special-case, e.g. decoded?.[method === 'supply' || source.selector === SELECTORS.farmDeposit ? 1 : 0], so the amount argument (index 1) is read for MasterChef deposits just as it already is for Aave-style supply.

This is isolated to integration_test/load_generator, a standalone load-testing/replay tool with no production chain code changes (per the PR description), and the replayed transaction still executes successfully with a nonzero amount — so it degrades replay fidelity for one traffic lane rather than causing any crash, revert, or data-loss risk.