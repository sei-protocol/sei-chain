//go:build inprocess

package inprocess

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	srvconfig "github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/genutil"
	tmconfig "github.com/sei-protocol/sei-chain/sei-tendermint/config"
)

// statesyncNodeMoniker is the late-joining RPC node's name — the moniker the
// statesync suite targets (node: sei-rpc-node), resolved by the runner's nodeFor.
const statesyncNodeMoniker = "sei-rpc-node"

// stopWaitTimeout bounds how long a stop/kill waits for one node to be reaped
// before giving up. A process wedged in uninterruptible I/O (a slow pebbledb
// fsync) isn't reaped promptly; without a bound, teardown would block there. It
// also doubles as the graceful-stop grace period before SIGKILL escalation and as
// exec.Cmd.WaitDelay.
const stopWaitTimeout = 5 * time.Second

// nodeProc is one node's running process and the state the reaper and the control
// verbs (Stop/Kill/Restart/IsRunning) share. Its own methods hold mu; it is never
// copied (SubprocessNetwork.procs is []*nodeProc), so its sync.Mutex is safe.
type nodeProc struct {
	node     *node    // the provisioned node (home/config/ports); set once at build
	extraEnv []string // env applied at each (re)start (e.g. UPGRADE_VERSION_LIST); caller-owned, not mu-guarded (the reaper never reads it)

	mu      sync.Mutex
	cmd     *exec.Cmd     // current process; replaced on restart
	log     *os.File      // current log sink; closed by the reaper on exit
	running bool          // true between start and the reaper observing exit
	done    chan struct{} // closed by the reaper when the current cmd's Wait returns
	waitErr error         // the current process's exit error (nil = clean exit)
}

// SubprocessNetwork is the Tier-2 harness: N real `seid` processes on disk, no
// docker. It reuses the in-process provisioning (genesis, keys, gentx-derived peer
// mesh — see provision) but boots each node with `seid start` instead of an
// in-goroutine app. That makes it slower than the in-process backend but able to
// exercise the operational lifecycle the in-process one can't — process
// kill/restart (Stop/Kill/Restart), on-disk WAL recovery, upgrade-by-env. Each node
// is its own OS process, so the process-global-singleton / one-network-per-process
// constraint (see networkStarted) does not apply here.
//
// Not goroutine-safe across calls (mirrors Network): callers drive one
// SubprocessNetwork from a single goroutine. The per-node reaper goroutines are the
// only concurrency, and they touch only their nodeProc under its mutex.
type SubprocessNetwork struct {
	net     *Network
	seidBin string
	// ctx parents every spawned process (see startProc) and is reused by Restart, so
	// it must outlive the network — cancelling it SIGKILLs every node's group. Stored
	// (not passed per-call) because the supervisor's lifetime is the process tree's.
	ctx    context.Context
	procs  []*nodeProc // index-aligned with net.nodes
	closed bool
}

// StartSubprocess provisions opts.Validators node homes on disk (reusing the
// in-process provisioning) and boots each as a `seid start` subprocess running
// seidBin. It returns once every process is spawned — NOT once consensus is live;
// call WaitReady for that. On any error mid-bring-up it tears down whatever came up.
//
// ctx bounds the spawned processes: cancelling it SIGKILLs every node's process
// group (see startProc). It is the caller's backstop when Close's defer can't run.
func StartSubprocess(ctx context.Context, opts Options, seidBin string) (_ *SubprocessNetwork, retErr error) {
	opts = opts.withDefaults()
	// N>=3 only. The in-process backend supports N=1 by pinning the sole validator
	// into the genesis doc in memory (see startNode); that pin is never written to
	// disk, so a subprocess node — which reads genesis from disk — would see an
	// empty valset and stall at height 1. N=2 deadlocks CometBFT block-sync.
	if opts.Validators < 3 {
		switch opts.Validators {
		case 1:
			return nil, fmt.Errorf("inprocess: StartSubprocess requires Validators >= 3: N=1 needs the genesis validator pinned on disk, which only the in-process backend does (in memory); a solo subprocess node reads an empty valset and stalls at height 1")
		case 2:
			return nil, fmt.Errorf("inprocess: StartSubprocess requires Validators >= 3: N=2 deadlocks in CometBFT block-sync (BlockPool.IsCaughtUp requires >1 peer)")
		default:
			return nil, fmt.Errorf("inprocess: StartSubprocess requires Validators >= 3, got %d", opts.Validators)
		}
	}
	if seidBin == "" {
		return nil, fmt.Errorf("inprocess: StartSubprocess requires a seid binary path")
	}

	baseDir, ownBaseDir, err := resolveBaseDir(opts.BaseDir)
	if err != nil {
		return nil, err
	}
	net := &Network{opts: opts, baseDir: baseDir, ownBaseDir: ownBaseDir}
	sn := &SubprocessNetwork{net: net, seidBin: seidBin, ctx: ctx}
	defer func() {
		if retErr != nil {
			sn.Close()
		}
	}()

	if _, err := net.provision(); err != nil {
		return nil, err
	}

	// Write the on-disk config `seid start` reads that the in-process path keeps in
	// memory (config.toml from tmCfg; the app.toml base + EVM/SeiDB sections) — then
	// spawn each node.
	for _, n := range net.nodes {
		if err := sn.writeNodeConfig(n); err != nil {
			return nil, fmt.Errorf("write subprocess config for %s: %w", n.moniker, err)
		}
	}
	sn.procs = make([]*nodeProc, len(net.nodes))
	for i, n := range net.nodes {
		sn.procs[i] = &nodeProc{node: n}
	}
	for _, p := range sn.procs {
		if err := sn.startProc(p); err != nil {
			return nil, fmt.Errorf("start %s: %w", p.node.moniker, err)
		}
	}
	return sn, nil
}

// startProc launches (or relaunches) one node's `seid start` in its own process
// group and hands the single Wait to a reaper goroutine. The reaper is the one
// owner of cmd.Wait, so it observes every exit — clean, panic (upgrade suites), or
// our own kill — updating running/waitErr and closing the log; the control verbs
// only signal and wait on done. Setpgid lets cmd.Cancel and the verbs signal the
// whole tree (any children seid spawns); binding cmd to sn.ctx makes cancellation
// the teardown backstop when Close can't run. The log is opened O_APPEND so a
// restart preserves the prior run's output (the upgrade/crash suites read the
// pre-restart panic).
func (sn *SubprocessNetwork) startProc(p *nodeProc) error {
	logf, err := os.OpenFile(filepath.Join(p.node.home, "seid.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(sn.ctx, sn.seidBin, "start", "--home", p.node.home) //nolint:gosec
	cmd.Stdout = logf
	cmd.Stderr = logf
	cmd.Env = append(os.Environ(), p.extraEnv...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Default CommandContext kills only the leader; -pid signals the whole group so
	// Setpgid children aren't orphaned. WaitDelay bounds the post-cancel reaper.
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = stopWaitTimeout
	if err := cmd.Start(); err != nil {
		_ = logf.Close()
		return err
	}

	done := make(chan struct{})
	p.mu.Lock()
	p.cmd = cmd
	p.log = logf
	p.running = true
	p.waitErr = nil
	p.done = done
	p.mu.Unlock()

	go func() {
		werr := cmd.Wait()
		_ = logf.Close()
		p.mu.Lock()
		// Only record exit if this is still the current process. A restart that gave
		// up on a wedged old process (signalAndWait returned !exited, so Restart
		// aborts) leaves this reaper live; when the old process finally dies it must
		// not clobber a newer process's running/waitErr.
		if p.cmd == cmd {
			p.running = false
			p.waitErr = werr
		}
		p.mu.Unlock()
		close(done)
	}()
	return nil
}

// Stop gracefully stops node i (SIGTERM, escalating to SIGKILL after
// stopWaitTimeout) and waits for it to exit. Idempotent: a no-op if the node is
// already down. The node's home/data survive, so a later Restart recovers via WAL.
func (sn *SubprocessNetwork) Stop(i int) error {
	_, err := sn.signalAndWait(sn.procs[i], syscall.SIGTERM)
	return err
}

// Kill hard-kills node i (SIGKILL) and waits for it to exit — an ungraceful crash
// for the crash-recovery suites. Idempotent.
func (sn *SubprocessNetwork) Kill(i int) error {
	_, err := sn.signalAndWait(sn.procs[i], syscall.SIGKILL)
	return err
}

// Restart stops node i (if running) and starts it again from the same home. WithEnv
// sets the process env for this and subsequent starts (the upgrade suites pass
// UPGRADE_VERSION_LIST to bring a node up "ahead" of the chain). A node that already
// exited on its own (crash-recovery, or an upgrade panic) is simply started again →
// WAL replay.
//
// It refuses to start over a process it could not confirm dead: the restarted node
// reuses the same fixed RPC/P2P/EVM/gRPC ports, so starting while the old process
// still holds them would silently EADDRINUSE-crash the new one. This only happens if
// SIGKILL can't reap the old process within the bound (a rare uninterruptible-I/O
// wedge).
func (sn *SubprocessNetwork) Restart(i int, opts ...RestartOption) error {
	var cfg restartConfig
	for _, o := range opts {
		o(&cfg)
	}
	p := sn.procs[i]
	exited, err := sn.signalAndWait(p, syscall.SIGTERM)
	if err != nil {
		return err
	}
	if !exited {
		return fmt.Errorf("inprocess: cannot restart %s: previous process did not exit within %s (still holding its ports)", p.node.moniker, stopWaitTimeout)
	}
	if cfg.env != nil {
		p.extraEnv = cfg.env // caller-owned; startProc reads it on this same goroutine
	}
	return sn.startProc(p)
}

// IsRunning reports whether node i's process is currently up. It flips to false the
// moment the reaper observes an exit, so the upgrade suites can assert that a
// non-upgraded node panicked at the upgrade height.
func (sn *SubprocessNetwork) IsRunning(i int) bool {
	p := sn.procs[i]
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running
}

// RestartOption configures a Restart.
type RestartOption func(*restartConfig)

type restartConfig struct{ env []string }

// WithEnv sets the process environment (KEY=VALUE strings) applied on the next
// start and retained across later restarts, replacing any previously set env. Used
// by the upgrade suites to set UPGRADE_VERSION_LIST.
func WithEnv(kv ...string) RestartOption {
	return func(c *restartConfig) { c.env = kv }
}

// signalAndWait signals node p's process group and waits for the reaper to observe
// exit, escalating a graceful signal to SIGKILL after stopWaitTimeout. It reports
// whether the process actually exited within the bounds: exited=false means the
// process is still alive and unreaped (a rare uninterruptible-I/O wedge SIGKILL
// can't clear) — the caller must NOT start a replacement over it (see Restart). An
// already-down process (or one that vanished mid-signal, ESRCH) is exited=true. Only
// a genuinely failed signal syscall is an error.
func (sn *SubprocessNetwork) signalAndWait(p *nodeProc, sig syscall.Signal) (exited bool, err error) {
	p.mu.Lock()
	cmd, done, running := p.cmd, p.done, p.running
	p.mu.Unlock()
	if !running || cmd == nil || cmd.Process == nil {
		return true, nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, sig); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return true, nil // already gone
		}
		return false, fmt.Errorf("signal %v to %s: %w", sig, p.node.moniker, err)
	}
	select {
	case <-done:
		return true, nil
	case <-time.After(stopWaitTimeout):
	}
	if sig != syscall.SIGKILL {
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			return false, fmt.Errorf("SIGKILL to %s: %w", p.node.moniker, err)
		}
		select {
		case <-done:
			return true, nil
		case <-time.After(stopWaitTimeout):
		}
	}
	return false, nil
}

// AddStatesyncNode provisions and starts a late-joining, non-validator full node
// (moniker sei-rpc-node) that STATE-SYNCS from the running validators' snapshots
// rather than replaying from genesis. It must be called after the validators have
// produced at least one snapshot (height >= SnapshotInterval — the caller
// sequences this), because a statesync node that finds no snapshot loops forever at
// height 0. It returns the node's handle; the caller waits for it to sync (its TM
// RPC latest_block_height goes > 0). The node joins the network (so Close tears it
// down and the runner can target it by moniker). Requires SnapshotInterval > 0.
func (sn *SubprocessNetwork) AddStatesyncNode(ctx context.Context) (Node, error) {
	if sn.net.opts.SnapshotInterval == 0 {
		return Node{}, fmt.Errorf("inprocess: AddStatesyncNode requires Options.SnapshotInterval > 0 (validators must produce snapshots to sync from)")
	}
	if len(sn.net.nodes) < 2 {
		return Node{}, fmt.Errorf("inprocess: AddStatesyncNode needs >= 2 validators to peer with, have %d", len(sn.net.nodes))
	}

	// Self-sequence: a joiner that starts before any snapshot exists discovers
	// nothing and loops forever at height 0 (no error). Wait for a validator to have
	// written one first.
	if err := waitValidatorSnapshot(ctx, filepath.Join(sn.net.nodes[0].home, "snapshots")); err != nil {
		return Node{}, fmt.Errorf("no validator snapshot to sync from: %w", err)
	}

	nodeDir := filepath.Join(sn.net.baseDir, statesyncNodeMoniker, "simd")
	if err := os.MkdirAll(filepath.Join(nodeDir, "config"), 0o750); err != nil {
		return Node{}, err
	}
	tmCfg, addrs, err := buildNodeConfig(nodeDir, statesyncNodeMoniker, sn.net.opts.TimeoutCommit)
	if err != nil {
		return Node{}, err
	}
	tmCfg.Mode = tmconfig.ModeFull // a plain full node, not in the valset
	nodeID, pubKey, err := genutil.InitializeNodeValidatorFiles(tmCfg)
	if err != nil {
		return Node{}, fmt.Errorf("init node files for %s: %w", statesyncNodeMoniker, err)
	}

	// Peer with every validator (the statesync reactor blocks until it has >= 2
	// peers), and anchor the light client to a currently-committed block.
	peers := make([]string, len(sn.net.nodes))
	for i, v := range sn.net.nodes {
		peers[i] = fmt.Sprintf("%s@%s:%s", v.nodeID, v.p2pHost, v.p2pPort)
	}
	tmCfg.P2P.PersistentPeers = strings.Join(peers, ",")

	valRPC := sn.Node(0).TendermintRPC()
	trustHeight, trustHash, err := fetchTrustBlock(ctx, valRPC)
	if err != nil {
		return Node{}, fmt.Errorf("read trust block from %s: %w", valRPC, err)
	}
	// Two DISTINCT validators as light-client providers (primary + witness). More
	// robust than the same URL twice, and it survives the upgrade suites bouncing
	// node 0 — N>=3 is enforced, so nodes[1] exists.
	tmCfg.StateSync.Enable = true
	tmCfg.StateSync.RPCServers = []string{sn.net.nodes[0].rpcAddr, sn.net.nodes[1].rpcAddr}
	tmCfg.StateSync.TrustHeight = trustHeight
	tmCfg.StateSync.TrustHash = trustHash
	tmCfg.StateSync.TrustPeriod = 168 * time.Hour
	tmCfg.StateSync.DiscoveryTime = 15 * time.Second

	if err := tmconfig.WriteConfigFile(nodeDir, tmCfg); err != nil {
		return Node{}, fmt.Errorf("write config.toml: %w", err)
	}
	// Same chain => identical genesis. State sync verifies the restored app hash
	// against headers derived from this genesis, so it must match the validators'.
	if err := copyFile(sn.net.nodes[0].tmCfg.GenesisFile(), tmCfg.GenesisFile()); err != nil {
		return Node{}, fmt.Errorf("copy genesis: %w", err)
	}
	// The harness genesis carries an empty valset (CometBFT derives it via InitChain
	// on the validators — see the empty-valset invariant). A statesync node SKIPS
	// InitChain, so it would build initial state from that empty valset and fail with
	// "validatorSet proposer error: nil validator". Inject the live validator set
	// (identical to the genesis set — powers don't change) so initial-state creation
	// succeeds; statesync then overrides it at the snapshot height.
	vals, err := fetchGenesisValidators(ctx, valRPC)
	if err != nil {
		return Node{}, fmt.Errorf("read validator set from %s: %w", valRPC, err)
	}
	if err := injectGenesisValidators(tmCfg.GenesisFile(), vals); err != nil {
		return Node{}, fmt.Errorf("inject genesis validators: %w", err)
	}
	appPath := filepath.Join(nodeDir, "config", "app.toml")
	// SnapshotInterval 0: the joiner restores snapshots, it need not produce them.
	// The SeiDB section (via appendEVMAppConfig) must match the validators or the
	// restored app hash won't verify.
	if err := writeSubprocessAppBase(appPath, nodeDir, 0); err != nil {
		return Node{}, fmt.Errorf("write app.toml base: %w", err)
	}
	// Match the validators' SeiDB config (incl. the memiavl snapshot cadence) so the
	// restored app hash verifies and the joiner's WAL is bounded too.
	if err := appendEVMAppConfig(appPath, addrs.httpPort, addrs.wsPort, sn.net.opts.SnapshotInterval); err != nil {
		return Node{}, fmt.Errorf("append evm app config: %w", err)
	}
	if err := writeClientConfig(filepath.Join(nodeDir, "config/client.toml"), sn.net.opts.ChainID, addrs.rpcAddr); err != nil {
		return Node{}, fmt.Errorf("write client.toml: %w", err)
	}

	n := &node{
		moniker: statesyncNodeMoniker, nodeID: nodeID, pubKey: pubKey,
		home: nodeDir, tmCfg: tmCfg,
		p2pHost: addrs.p2pHost, p2pPort: addrs.p2pPort, rpcAddr: addrs.rpcAddr,
		httpPort: addrs.httpPort, wsPort: addrs.wsPort,
	}
	// Append only after the process starts, so a start failure doesn't leave a dead
	// node in the network for WaitReady to block on.
	p := &nodeProc{node: n}
	if err := sn.startProc(p); err != nil {
		return Node{}, fmt.Errorf("start %s: %w", statesyncNodeMoniker, err)
	}
	sn.net.nodes = append(sn.net.nodes, n)
	sn.procs = append(sn.procs, p)
	return Node{n: n}, nil
}

// fetchTrustBlock reads the light-client trust anchor (latest committed height +
// block hash) from a validator's CometBFT RPC /status. It uses the same dual-shape
// parse as latestHeight (the node's /status may be enveloped or unwrapped — see
// readiness.go) since sync_info carries both the height and the hash.
func fetchTrustBlock(ctx context.Context, tmRPC string) (height int64, hash string, err error) {
	body, ok := getJSON(ctx, probeClient, http.MethodGet, tmRPC+"/status", "")
	if !ok {
		return 0, "", fmt.Errorf("status unreachable at %s", tmRPC)
	}
	var s struct {
		Result *struct {
			SyncInfo syncInfo `json:"sync_info"`
		} `json:"result,omitempty"`
		SyncInfo syncInfo `json:"sync_info"`
	}
	if err := json.Unmarshal(body, &s); err != nil {
		return 0, "", fmt.Errorf("parse /status body: %w", err)
	}
	si := s.SyncInfo
	if s.Result != nil && s.Result.SyncInfo.LatestBlockHeight != "" {
		si = s.Result.SyncInfo
	}
	h, err := strconv.ParseInt(si.LatestBlockHeight, 10, 64)
	if err != nil {
		return 0, "", fmt.Errorf("parse trust height %q: %w", si.LatestBlockHeight, err)
	}
	if h == 0 || si.LatestBlockHash == "" {
		return 0, "", fmt.Errorf("no committed block yet at %s", tmRPC)
	}
	return h, si.LatestBlockHash, nil
}

// genesisValidator is one entry of the CometBFT genesis `validators` array. PubKey
// is passed through verbatim ({"type":...,"value":...}) from the /validators RPC,
// which uses the identical shape.
type genesisValidator struct {
	Address string          `json:"address"`
	PubKey  json.RawMessage `json:"pub_key"`
	Power   string          `json:"power"`
	Name    string          `json:"name"`
}

// fetchGenesisValidators reads a validator's /validators RPC and returns the set in
// genesis-validators shape (voting_power -> power). Dual-shape like fetchTrustBlock.
func fetchGenesisValidators(ctx context.Context, tmRPC string) ([]genesisValidator, error) {
	body, ok := getJSON(ctx, probeClient, http.MethodGet, tmRPC+"/validators", "")
	if !ok {
		return nil, fmt.Errorf("validators unreachable at %s", tmRPC)
	}
	type rpcVal struct {
		Address     string          `json:"address"`
		PubKey      json.RawMessage `json:"pub_key"`
		VotingPower string          `json:"voting_power"`
	}
	var s struct {
		Result *struct {
			Validators []rpcVal `json:"validators"`
		} `json:"result,omitempty"`
		Validators []rpcVal `json:"validators"`
	}
	if err := json.Unmarshal(body, &s); err != nil {
		return nil, fmt.Errorf("parse /validators body: %w", err)
	}
	vals := s.Validators
	if s.Result != nil && len(s.Result.Validators) > 0 {
		vals = s.Result.Validators
	}
	if len(vals) == 0 {
		return nil, fmt.Errorf("no validators reported by %s", tmRPC)
	}
	out := make([]genesisValidator, len(vals))
	for i, v := range vals {
		out[i] = genesisValidator{Address: v.Address, PubKey: v.PubKey, Power: v.VotingPower}
	}
	return out, nil
}

// injectGenesisValidators rewrites the genesis file's top-level `validators` array,
// leaving every other field byte-identical.
func injectGenesisValidators(genesisPath string, vals []genesisValidator) error {
	raw, err := os.ReadFile(genesisPath)
	if err != nil {
		return err
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return err
	}
	vb, err := json.Marshal(vals)
	if err != nil {
		return err
	}
	doc["validators"] = vb
	out, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	return os.WriteFile(genesisPath, out, 0o600)
}

// waitValidatorSnapshot blocks until snapDir holds at least one numeric height
// subdir (a written cosmos state-sync snapshot) or ctx fires.
func waitValidatorSnapshot(ctx context.Context, snapDir string) error {
	tick := time.NewTicker(probeInterval)
	defer tick.Stop()
	for {
		if entries, err := os.ReadDir(snapDir); err == nil {
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				if _, err := strconv.Atoi(e.Name()); err == nil {
					return nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("no snapshot under %s: %w", snapDir, ctx.Err())
		case <-tick.C:
		}
	}
}

// copyFile copies src to dst (0o600). Used to give the statesync node the
// validators' exact genesis.
func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o600)
}

// Node returns a handle to the i-th validator. The handle surface mirrors the
// in-process Network so consumers (the YAML runner) are backend-agnostic.
func (sn *SubprocessNetwork) Node(i int) Node { return Node{n: sn.net.nodes[i]} }

// Nodes returns handles to every validator in index order.
func (sn *SubprocessNetwork) Nodes() []Node {
	out := make([]Node, len(sn.net.nodes))
	for i := range sn.net.nodes {
		out[i] = Node{n: sn.net.nodes[i]}
	}
	return out
}

// Len is the validator count.
func (sn *SubprocessNetwork) Len() int { return len(sn.net.nodes) }

// WaitReady blocks until every node is producing blocks and serving EVM, or ctx
// fires — the same readiness probes the in-process backend uses (HTTP against each
// node's own RPC), so the gate is backend-independent. The returned error already
// names the node (Node.WaitReady prepends its moniker).
func (sn *SubprocessNetwork) WaitReady(ctx context.Context) error {
	for i := range sn.net.nodes {
		if err := sn.Node(i).WaitReady(ctx); err != nil {
			return err
		}
	}
	return nil
}

// Close kills every seid process group and removes the owned temp dir. Idempotent
// and safe on a partial start (nil-guarded).
//
// A completed Close leaves nothing behind — each SIGKILL reaps the node's group and
// the reaper closes its log — but it does NOT defend against the test binary being
// hard-killed: `go test -timeout` sends SIGQUIT (defers don't run) and Setpgid has
// detached each node into its own group, so those groups reparent to PID 1. The ctx
// from StartSubprocess is the backstop there — keep suite time under the outer
// -timeout so this defer runs.
func (sn *SubprocessNetwork) Close() {
	if sn.closed {
		return
	}
	sn.closed = true
	for _, p := range sn.procs {
		if p != nil {
			_, _ = sn.signalAndWait(p, syscall.SIGKILL)
		}
	}
	if sn.net.ownBaseDir && sn.net.baseDir != "" {
		_ = os.RemoveAll(sn.net.baseDir)
	}
}

// LogTails returns a bounded tail of every node's captured seid.log, for a
// post-mortem when the cluster fails to come up. Best-effort: on Close's own
// SIGKILL a child can't flush, so a node that hung then got killed may have a
// short or stale log.
func (sn *SubprocessNetwork) LogTails() string {
	const perNode = 2000
	var b strings.Builder
	for _, n := range sn.net.nodes {
		fmt.Fprintf(&b, "=== %s seid.log (tail) ===\n", n.moniker)
		raw, err := os.ReadFile(filepath.Join(n.home, "seid.log"))
		if err != nil {
			fmt.Fprintf(&b, "(no log: %v)\n", err)
			continue
		}
		s := string(raw)
		if len(s) > perNode {
			s = s[len(s)-perNode:]
		}
		b.WriteString(s)
		b.WriteString("\n")
	}
	return b.String()
}

// writeNodeConfig writes the on-disk config `seid start` needs that the in-process
// path holds in memory: config.toml (the gentx-derived peer mesh + the loopback
// RPC/P2P listen addrs held in n.tmCfg) and the app.toml base + EVM/SeiDB sections
// (the in-process backend injects the latter via AppOptions and calls
// RegisterLocalServices directly — see appoptions.go / newNodeApp).
func (sn *SubprocessNetwork) writeNodeConfig(n *node) error {
	if err := tmconfig.WriteConfigFile(n.home, n.tmCfg); err != nil {
		return fmt.Errorf("write config.toml: %w", err)
	}
	appPath := filepath.Join(n.home, "config", "app.toml")
	if err := writeSubprocessAppBase(appPath, n.home, sn.net.opts.SnapshotInterval); err != nil {
		return fmt.Errorf("write app.toml base: %w", err)
	}
	return appendEVMAppConfig(appPath, n.httpPort, n.wsPort, sn.net.opts.SnapshotInterval)
}

// writeSubprocessAppBase rewrites the cosmos-base app.toml (over provision's
// gRPC-off default) to enable the cosmos gRPC server on a free per-node port, plus
// (when snapshotInterval > 0) cosmos state-sync snapshots into <home>/snapshots.
//
// The gRPC enable is load-bearing: `seid start` constructs the EVM HTTP/WS servers
// (via app.RegisterLocalServices) ONLY when the API or gRPC server is enabled (see
// sei-cosmos server.startInProcess) — with both off, a node reaches consensus but
// never serves EVM. gRPC is the lightest server that trips that gate; no suite
// dials it. API stays off (it would need its own per-node 1317 port). Per-node
// gRPC port so N nodes don't collide.
//
// The snapshot-directory override (validators, when snapshotInterval > 0) is also
// load-bearing: the default is <home>/data/snapshots, but the snapshot suite checks
// <home>/snapshots. The restoring joiner (snapshotInterval 0 here) keeps the default
// — its snapshot store is local restore scratch, unrelated to what it syncs from.
func writeSubprocessAppBase(path, home string, snapshotInterval uint64) error {
	grpcPort, err := freePort()
	if err != nil {
		return err
	}
	appCfg := srvconfig.DefaultConfig()
	appCfg.GRPC.Enable = true
	appCfg.GRPC.Address = fmt.Sprintf("127.0.0.1:%d", grpcPort)
	// GRPCWeb defaults on (:9091); force off or N nodes collide on that port too.
	appCfg.GRPCWeb.Enable = false
	appCfg.API.Enable = false
	appCfg.Telemetry.Enabled = false
	if snapshotInterval > 0 {
		appCfg.StateSync.SnapshotInterval = snapshotInterval
		appCfg.StateSync.SnapshotKeepRecent = 3
		appCfg.StateSync.SnapshotDirectory = filepath.Join(home, "snapshots")
	}
	srvconfig.WriteConfigFile(path, appCfg)
	return nil
}

// appendEVMAppConfig appends the sei-chain app sections (EVM enabled on this node's
// ports; SeiDB on) to the cosmos-base app.toml writeSubprocessAppBase just wrote.
// These sections are sei-chain-specific — the cosmos template renders none of them
// — so there is nothing to conflict with.
//
// When scSnapshotInterval > 0 it also sets the memiavl (state-commit) snapshot
// cadence. parseSCConfigs (app/seidb.go) reads sc-snapshot-interval unconditionally,
// so leaving it unset means memiavl never persists a snapshot and never truncates
// its WAL; setting it (to the cosmos state-sync interval) bounds the WAL and matches
// how a production node with snapshots runs — which the statesync path exercises.
func appendEVMAppConfig(path string, httpPort, wsPort int, scSnapshotInterval uint64) error {
	stateCommit := "sc-enable = true"
	if scSnapshotInterval > 0 {
		stateCommit += fmt.Sprintf("\nsc-snapshot-interval = %d\nsc-keep-recent = 2", scSnapshotInterval)
	}
	extra := fmt.Sprintf(`
[evm]
http_enabled = true
http_port = %d
ws_enabled = true
ws_port = %d

[state-commit]
%s

[state-store]
ss-enable = true
ss-backend = "pebbledb"
`, httpPort, wsPort, stateCommit)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.WriteString(extra)
	return err
}
