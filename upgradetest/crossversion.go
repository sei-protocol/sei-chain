package upgradetest

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	crossVersionPhaseEnv         = "UPGRADE_TEST_PHASE"
	crossVersionArtifactEnv      = "UPGRADE_TEST_ARTIFACT"
	crossVersionNodeEnv          = "UPGRADE_TEST_NODE"
	crossVersionUpgradeNameEnv   = "UPGRADE_TEST_UPGRADE_NAME"
	crossVersionTargetHeightEnv  = "UPGRADE_TEST_TARGET_HEIGHT"
	crossVersionReleaseBinaryEnv = "UPGRADE_TEST_RELEASE_BINARY"
	validatorCount               = 4
)

// A CrossVersion gives a tagged test access to the validator running each side
// of an upgrade and to the artifact shared between its two test processes.
type CrossVersion struct {
	node         string
	artifactPath string
	values       map[string]json.RawMessage
}

// CommandResult is the complete result of a command executed in a validator.
type CommandResult struct {
	Stdout string
	Stderr string
	Err    error
}

// ExportedGenesis is the application state emitted by seid export.
type ExportedGenesis struct {
	AppState map[string]json.RawMessage `json:"app_state"`
}

type crossVersionArtifact struct {
	Values map[string]json.RawMessage `json:"values"`
}

// RunCrossVersion runs the callback selected by UPGRADE_TEST_PHASE. Without a
// phase it skips the test so ordinary tagged app tests remain self-contained.
func RunCrossVersion(
	t *testing.T,
	before func(*testing.T, *CrossVersion),
	after func(*testing.T, *CrossVersion),
) {
	t.Helper()

	phase := os.Getenv(crossVersionPhaseEnv)
	if phase == "" {
		t.Skip("cross-version phase is driven by make upgrade-test-cross-version")
	}
	if phase != "before" && phase != "after" {
		t.Fatalf("%s must be before or after, got %q", crossVersionPhaseEnv, phase)
	}

	artifactPath := os.Getenv(crossVersionArtifactEnv)
	if artifactPath == "" {
		t.Fatalf("%s is required", crossVersionArtifactEnv)
	}
	node := os.Getenv(crossVersionNodeEnv)
	if node == "" {
		t.Fatalf("%s is required", crossVersionNodeEnv)
	}
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o750); err != nil {
		t.Fatalf("create cross-version artifact directory: %v", err)
	}

	crossVersion := &CrossVersion{
		node:         node,
		artifactPath: artifactPath,
		values:       map[string]json.RawMessage{},
	}
	if phase == "after" {
		crossVersion.load(t)
		after(t, crossVersion)
	} else {
		before(t, crossVersion)
	}
	crossVersion.save(t)
}

// Record stores a JSON value for the other side of the upgrade.
func (c *CrossVersion) Record(t *testing.T, name string, value any) {
	t.Helper()
	if name == "" {
		t.Fatal("cross-version artifact key must not be empty")
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode cross-version artifact %q: %v", name, err)
	}
	c.values[name] = encoded
}

// Replay decodes a value recorded on the other side of the upgrade.
func (c *CrossVersion) Replay(t *testing.T, name string, target any) {
	t.Helper()
	encoded, ok := c.values[name]
	if !ok {
		t.Fatalf("cross-version artifact has no %q value", name)
	}
	if err := json.Unmarshal(encoded, target); err != nil {
		t.Fatalf("decode cross-version artifact %q: %v", name, err)
	}
}

// UpgradeName returns the upgrade name selected by the orchestrator.
func (c *CrossVersion) UpgradeName(t *testing.T) string {
	t.Helper()
	return requiredEnv(t, crossVersionUpgradeNameEnv)
}

// TargetHeight returns the height selected by the governance proposal.
func (c *CrossVersion) TargetHeight(t *testing.T) int64 {
	t.Helper()
	value := requiredEnv(t, crossVersionTargetHeightEnv)
	height, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		t.Fatalf("parse %s=%q: %v", crossVersionTargetHeightEnv, value, err)
	}
	return height
}

// ReleaseBinary returns the path at which the validator keeps the old binary.
func (c *CrossVersion) ReleaseBinary(t *testing.T) string {
	t.Helper()
	return requiredEnv(t, crossVersionReleaseBinaryEnv)
}

// Node returns the validator this process is bound to.
func (c *CrossVersion) Node() string {
	return c.node
}

// Nodes returns the validator names in the cluster.
func (c *CrossVersion) Nodes() []string {
	nodes := make([]string, validatorCount)
	for i := range nodes {
		nodes[i] = fmt.Sprintf("sei-node-%d", i)
	}
	return nodes
}

// Seid executes the running seid binary in the primary validator.
func (c *CrossVersion) Seid(input string, args ...string) CommandResult {
	return c.SeidOn(c.node, input, args...)
}

// SeidOn executes the running seid binary in a named validator.
func (c *CrossVersion) SeidOn(node, input string, args ...string) CommandResult {
	return c.BinaryOn(node, input, "/root/go/bin/seid", args...)
}

// Binary executes a seid-compatible binary in the primary validator.
func (c *CrossVersion) Binary(input, binary string, args ...string) CommandResult {
	return c.BinaryOn(c.node, input, binary, args...)
}

// BinaryOn executes a seid-compatible binary in a named validator.
func (c *CrossVersion) BinaryOn(node, input, binary string, args ...string) CommandResult {
	command := append([]string{binary}, args...)
	return runDocker(node, input, command...)
}

// MustSeid executes seid and fails the test when the process exits unsuccessfully.
func (c *CrossVersion) MustSeid(t *testing.T, input string, args ...string) string {
	t.Helper()
	result := c.Seid(input, args...)
	if result.Err != nil {
		t.Fatalf("seid %s failed: %v\n%s", strings.Join(args, " "), result.Err, result.Combined())
	}
	return result.Stdout
}

// RequireBlockAgreement requires every validator to report the same application
// hash and block hash at each height.
func (c *CrossVersion) RequireBlockAgreement(t *testing.T, heights ...int64) {
	t.Helper()
	if len(heights) == 0 {
		t.Fatal("no heights to compare")
	}
	maxHeight := heights[0]
	for _, height := range heights {
		if height <= 0 {
			t.Fatalf("height must be positive, got %d", height)
		}
		if height > maxHeight {
			maxHeight = height
		}
	}
	nodes := c.Nodes()
	for _, node := range nodes {
		c.WaitForHeightOn(t, node, maxHeight, 3*time.Minute)
	}
	for _, height := range heights {
		views := make([]blockView, 0, len(nodes))
		for _, node := range nodes {
			views = append(views, c.blockAt(t, node, height))
		}
		if err := validatorBlockAgreementError(height, views); err != nil {
			t.Fatal(err)
		}
	}
}

func (c *CrossVersion) blockAt(t *testing.T, node string, height int64) blockView {
	t.Helper()
	if node == "" {
		t.Fatal("node must not be empty")
	}
	if height <= 0 {
		t.Fatalf("height must be positive, got %d", height)
	}
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "block",
		"params": map[string]any{
			"height": strconv.FormatInt(height, 10),
		},
	})
	if err != nil {
		t.Fatalf("encode block query: %v", err)
	}
	result := c.BinaryOn(node, "", "curl", "-sf", "-H", "Content-Type: application/json",
		"-d", string(body), "http://127.0.0.1:26657")
	label := fmt.Sprintf("block-%s-%d", node, height)
	c.WriteDiagnostic(t, label+".stdout", []byte(result.Stdout))
	c.WriteDiagnostic(t, label+".stderr", []byte(result.Stderr))
	if result.Err != nil {
		t.Fatalf("block query at height %d on %s failed: %v\n%s", height, node, result.Err, result.Combined())
	}
	parsed, err := parseBlockIdentity([]byte(result.Stdout))
	if err != nil {
		t.Fatalf("decode block query at height %d on %s: %v\n%s", height, node, err, result.Stdout)
	}
	if parsed.height != height {
		t.Fatalf("block query at height %d on %s returned height %d\n%s",
			height, node, parsed.height, result.Stdout)
	}
	return blockView{node: node, appHash: parsed.appHash, blockHash: parsed.blockHash}
}

// QueryStore reads key from storeName on the running node via ABCI query.
func (c *CrossVersion) QueryStore(t *testing.T, storeName string, key []byte) []byte {
	t.Helper()
	if storeName == "" {
		t.Fatal("store name must not be empty")
	}
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "abci_query",
		"params": map[string]any{
			"path":  "/store/" + storeName + "/key",
			"data":  hex.EncodeToString(key),
			"prove": false,
		},
	})
	if err != nil {
		t.Fatalf("encode ABCI query: %v", err)
	}
	result := c.Binary("", "curl", "-sf", "-H", "Content-Type: application/json",
		"-d", string(body), "http://127.0.0.1:26657")
	label := fmt.Sprintf("abci-query-%s-%s", storeName, hex.EncodeToString(key))
	c.WriteDiagnostic(t, label+".stdout", []byte(result.Stdout))
	c.WriteDiagnostic(t, label+".stderr", []byte(result.Stderr))
	if result.Err != nil {
		t.Fatalf("ABCI query /store/%s/key failed: %v\n%s", storeName, result.Err, result.Combined())
	}
	value, code, log, err := parseABCIQueryResponse([]byte(result.Stdout))
	if err != nil {
		t.Fatalf("decode ABCI query /store/%s/key: %v\n%s", storeName, err, result.Stdout)
	}
	if code != 0 {
		t.Fatalf("ABCI query /store/%s/key returned code %d: %s", storeName, code, log)
	}
	return value
}

// KeyAddress returns an address from a validator's test keyring.
func (c *CrossVersion) KeyAddress(t *testing.T, node, key string, extraArgs ...string) string {
	t.Helper()
	args := append([]string{"keys", "show", key, "-a"}, extraArgs...)
	result := c.SeidOn(node, "12345678\n", args...)
	if result.Err != nil {
		t.Fatalf("%s key %s: %v\n%s", node, key, result.Err, result.Combined())
	}
	address := strings.TrimSpace(result.Stdout)
	if address == "" {
		t.Fatalf("%s key %s returned an empty address", node, key)
	}
	return address
}

// DeliveredTx is the committed DeliverTx result of a broadcast transaction.
type DeliveredTx struct {
	Hash    string
	Height  int64
	Code    int64
	GasUsed int64
	RawLog  string
}

// RequireDeliverTxSuccess waits until a synchronously broadcast transaction is
// included in a committed block and requires that its DeliverTx result succeeded.
func (c *CrossVersion) RequireDeliverTxSuccess(t *testing.T, label string, result CommandResult) DeliveredTx {
	t.Helper()
	slug := strings.ReplaceAll(label, " ", "-")
	c.WriteDiagnostic(t, slug+".broadcast.stdout", []byte(result.Stdout))
	c.WriteDiagnostic(t, slug+".broadcast.stderr", []byte(result.Stderr))
	c.requireCheckTxSuccess(t, label, result)
	hash, err := parseBroadcastTxHash(result.Stdout)
	if err != nil {
		t.Fatalf("%s: %v\n%s", label, err, result.Stdout)
	}

	deadline := time.Now().Add(3 * time.Minute)
	var last CommandResult
	for time.Now().Before(deadline) {
		last = c.Seid("", "q", "tx", hash, "--output", "json")
		if last.Err == nil {
			c.WriteDiagnostic(t, slug+".included.json", []byte(last.Stdout))
			delivered, err := parseDeliveredTx(last.Stdout)
			if err != nil {
				t.Fatalf("%s: decode included tx: %v\n%s", label, err, last.Stdout)
			}
			if delivered.Hash != "" && !strings.EqualFold(delivered.Hash, hash) {
				t.Fatalf("%s query returned hash %s, want %s", label, delivered.Hash, hash)
			}
			if delivered.Height <= 0 {
				t.Fatalf("%s was not included in a block (hash %s)", label, hash)
			}
			if delivered.Code != 0 {
				t.Fatalf("%s delivered with code %d: %s", label, delivered.Code, delivered.RawLog)
			}
			if delivered.GasUsed <= 0 {
				t.Fatalf("%s consumed no gas", label)
			}
			delivered.Hash = hash
			return delivered
		}
		time.Sleep(time.Second)
	}
	c.WriteDiagnostic(t, slug+".query.stdout", []byte(last.Stdout))
	c.WriteDiagnostic(t, slug+".query.stderr", []byte(last.Stderr))
	t.Fatalf("%s was not included within 3m (hash %s): %v\n%s",
		label, hash, last.Err, last.Combined())
	return DeliveredTx{}
}

// requireCheckTxSuccess requires that a synchronous broadcast's CheckTx result succeeded.
func (c *CrossVersion) requireCheckTxSuccess(t *testing.T, label string, result CommandResult) {
	t.Helper()
	if result.Err != nil {
		t.Fatalf("%s failed: %v\n%s", label, result.Err, result.Combined())
	}
	var response struct {
		Code   json.RawMessage `json:"code"`
		RawLog string          `json:"raw_log"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &response); err != nil {
		t.Fatalf("%s did not return JSON: %v\n%s", label, err, result.Stdout)
	}
	if len(response.Code) == 0 {
		return
	}
	code, err := parseJSONInt(response.Code)
	if err != nil {
		t.Fatalf("%s returned an invalid code: %v", label, err)
	}
	if code != 0 {
		t.Fatalf("%s was rejected with code %d: %s", label, code, response.RawLog)
	}
}

// ModuleVersions returns the sorted names in the on-chain module version map.
func (c *CrossVersion) ModuleVersions(t *testing.T) []string {
	t.Helper()
	output := c.MustSeid(t, "", "q", "upgrade", "module_versions", "--output", "json")
	var response struct {
		ModuleVersions []struct {
			Name string `json:"name"`
		} `json:"module_versions"`
	}
	if err := json.Unmarshal([]byte(output), &response); err != nil {
		t.Fatalf("decode module version map: %v\n%s", err, output)
	}
	names := make([]string, 0, len(response.ModuleVersions))
	for _, version := range response.ModuleVersions {
		names = append(names, version.Name)
	}
	sort.Strings(names)
	return names
}

// Height returns the primary validator's latest committed height.
func (c *CrossVersion) Height(t *testing.T) int64 {
	t.Helper()
	return c.HeightOn(t, c.node)
}

// HeightOn returns a named validator's latest committed height.
func (c *CrossVersion) HeightOn(t *testing.T, node string) int64 {
	t.Helper()
	height, output, err := c.tryHeightOn(node)
	if err != nil {
		t.Fatalf("query %s height: %v\n%s", node, err, output)
	}
	return height
}

// WaitForHeight waits until the primary validator reaches a committed height.
func (c *CrossVersion) WaitForHeight(t *testing.T, target int64, timeout time.Duration) {
	t.Helper()
	c.WaitForHeightOn(t, c.node, target, timeout)
}

// WaitForHeightOn waits until a named validator reaches a committed height.
func (c *CrossVersion) WaitForHeightOn(t *testing.T, node string, target int64, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastOutput string
	var lastErr error
	for time.Now().Before(deadline) {
		height, output, err := c.tryHeightOn(node)
		lastOutput, lastErr = output, err
		if err == nil && height >= target {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("%s did not reach height %d within %s: %v\n%s",
		node, target, timeout, lastErr, lastOutput)
}

// WaitForBlocks waits for the primary validator to commit more blocks.
func (c *CrossVersion) WaitForBlocks(t *testing.T, blocks int64) {
	t.Helper()
	c.WaitForHeight(t, c.Height(t)+blocks, 3*time.Minute)
}

// StopNode stops seid in the primary validator.
func (c *CrossVersion) StopNode(t *testing.T) {
	t.Helper()
	c.StopNodeOn(t, c.node)
}

// StopNodeOn stops seid in a named validator.
func (c *CrossVersion) StopNodeOn(t *testing.T, node string) {
	t.Helper()
	c.signalSeidOn(t, node, "TERM")
}

// KillNodeOn sends SIGKILL to seid in a named validator and waits until it has exited.
func (c *CrossVersion) KillNodeOn(t *testing.T, node string) {
	t.Helper()
	c.signalSeidOn(t, node, "KILL")
}

// StartObservation is the state of a validator after StartNodeObserving returns.
type StartObservation struct {
	// Running reports whether seid was still alive at the end of the window.
	Running bool
	// Height is the last committed height a status query returned. It is 0 when
	// no query succeeded.
	Height int64
	// Log is the restart log written by this start.
	Log string
}

// StartNodeOn starts seid in a named validator using binary.
func (c *CrossVersion) StartNodeOn(t *testing.T, node, binary string) {
	t.Helper()
	c.launchSeidOn(t, node, binary, seidNodeLogPath(node), false)
	c.waitForSeidState(t, node, "running", time.Minute)
}

// StartNodeObserving starts seid on node with binary and watches it until timeout.
// A process that exits during the window is a returned outcome, not a test failure.
func (c *CrossVersion) StartNodeObserving(t *testing.T, node, binary string, timeout time.Duration) StartObservation {
	t.Helper()
	if timeout <= 0 {
		t.Fatalf("observation timeout must be positive, got %s", timeout)
	}
	c.launchSeidOn(t, node, binary, seidObservedLogPath(node), true)

	deadline := time.Now().Add(timeout)
	var observed StartObservation
	var lastInspectErr error
	sawRunning := false
	for time.Now().Before(deadline) {
		state, err := c.processStateOn(node)
		if err != nil {
			lastInspectErr = err
			time.Sleep(time.Second)
			continue
		}
		lastInspectErr = nil
		if state == "running" {
			sawRunning = true
			observed.Running = true
			if height, _, heightErr := c.tryHeightOn(node); heightErr == nil {
				observed.Height = height
			}
		} else {
			observed.Running = false
			if sawRunning {
				break
			}
		}
		time.Sleep(time.Second)
	}
	if lastInspectErr != nil && !sawRunning {
		t.Fatalf("inspect seid in %s: %v", node, lastInspectErr)
	}
	observed.Log = c.readSeidLog(node, seidObservedLogPath(node))
	return observed
}

func (c *CrossVersion) launchSeidOn(t *testing.T, node, binary, logPath string, fresh bool) {
	t.Helper()
	if node == "" {
		t.Fatal("node must not be empty")
	}
	if binary == "" {
		t.Fatal("binary must not be empty")
	}
	state, err := c.processStateOn(node)
	if err != nil {
		t.Fatalf("inspect seid in %s: %v", node, err)
	}
	if state == "running" {
		t.Fatalf("seid in %s is already running", node)
	}

	redirect := ">>"
	if fresh {
		redirect = ">"
	}
	script := fmt.Sprintf(
		"exec env -u UPGRADE_VERSION_LIST %s start --chain-id sei --inv-check-period 10 %s %s 2>&1",
		strconv.Quote(binary),
		redirect,
		logPath,
	)
	result := runDockerDetached(node, "sh", "-c", script)
	if result.Err != nil {
		t.Fatalf("start seid in %s: %v\n%s", node, result.Err, result.Combined())
	}
}

// seidNodeLogPath is the log a validator writes from the moment the cluster
// starts it. The orchestrator reads this file to recognise an upgrade halt, so
// a restart has to keep appending to it rather than divert output elsewhere.
func seidNodeLogPath(node string) string {
	return "build/generated/logs/seid-" + strings.TrimPrefix(node, "sei-node-") + ".log"
}

// seidObservedLogPath is the log a single observed start writes. It is truncated
// per launch, so a halt found in it belongs to that launch and not to one the
// validator logged earlier.
func seidObservedLogPath(node string) string {
	return "build/generated/logs/seid-" + strings.TrimPrefix(node, "sei-node-") + "-observed.log"
}

func (c *CrossVersion) readSeidLog(node, path string) string {
	result := c.BinaryOn(node, "", "cat", path)
	if result.Err != nil {
		return result.Combined()
	}
	return result.Stdout
}

func (c *CrossVersion) signalSeidOn(t *testing.T, node, signal string) {
	t.Helper()
	if node == "" {
		t.Fatal("node must not be empty")
	}
	if signal != "TERM" && signal != "KILL" {
		t.Fatalf("unsupported seid signal %q", signal)
	}
	result := runDocker(node, "", "sh", "-c", seidSignalScript(signal))
	if result.Err != nil {
		t.Fatalf("signal seid in %s: %v\n%s", node, result.Err, result.Combined())
	}
	c.waitForSeidState(t, node, "stopped", time.Minute)
}

func seidSignalScript(signal string) string {
	return fmt.Sprintf(`
for comm in /proc/[0-9]*/comm; do
  [ -r "$comm" ] || continue
  read -r name <"$comm" || continue
  if [ "$name" = seid ]; then
    process_dir="${comm%%/comm}"
    read -r stat_pid stat_comm process_state stat_rest <"$process_dir/stat" || continue
    [ "$process_state" = Z ] && continue
    pid="${comm#/proc/}"
    pid="${pid%%/comm}"
    kill -%s "$pid"
  fi
done`, signal)
}

func (c *CrossVersion) waitForSeidState(t *testing.T, node, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastState string
	var lastErr error
	for time.Now().Before(deadline) {
		state, err := c.processStateOn(node)
		lastState, lastErr = state, err
		if err == nil && state == want {
			return
		}
		time.Sleep(time.Second)
	}
	if lastErr != nil {
		t.Fatalf("seid in %s did not become %s within %s: %v", node, want, timeout, lastErr)
	}
	t.Fatalf("seid in %s did not become %s within %s; last state %s", node, want, timeout, lastState)
}

// Export runs a binary against the stopped validator and returns its app state.
func (c *CrossVersion) Export(t *testing.T, binary, label string) ExportedGenesis {
	t.Helper()
	var last CommandResult
	for attempt := 1; attempt <= 10; attempt++ {
		last = c.Binary("", binary, "export", "--home", "/root/.sei", "--chain-id", "sei")
		c.WriteDiagnostic(t, fmt.Sprintf("%s-%d.stdout", label, attempt), []byte(last.Stdout))
		c.WriteDiagnostic(t, fmt.Sprintf("%s-%d.stderr", label, attempt), []byte(last.Stderr))
		if last.Err == nil {
			genesis, err := extractGenesis([]byte(last.Stdout))
			if err == nil {
				encoded, marshalErr := json.Marshal(genesis)
				if marshalErr != nil {
					t.Fatalf("encode %s export: %v", label, marshalErr)
				}
				c.WriteDiagnostic(t, label+".json", encoded)
				return genesis
			}
			last.Err = err
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("%s export failed: %v\n%s", label, last.Err, last.Combined())
	return ExportedGenesis{}
}

// WriteDiagnostic writes one file beside the cross-version artifact.
func (c *CrossVersion) WriteDiagnostic(t *testing.T, name string, content []byte) {
	t.Helper()
	if name == "" || filepath.Base(name) != name {
		t.Fatalf("invalid cross-version diagnostic name %q", name)
	}
	path := filepath.Join(filepath.Dir(c.artifactPath), name)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write cross-version diagnostic %s: %v", path, err)
	}
}

// Combined returns stdout and stderr as one diagnostic string.
func (r CommandResult) Combined() string {
	switch {
	case r.Stdout == "":
		return r.Stderr
	case r.Stderr == "":
		return r.Stdout
	default:
		return r.Stdout + "\n" + r.Stderr
	}
}

func (c *CrossVersion) tryHeightOn(node string) (int64, string, error) {
	result := c.SeidOn(node, "", "status")
	output := result.Combined()
	if result.Err != nil {
		return 0, output, result.Err
	}
	var response struct {
		SyncInfo struct {
			LatestBlockHeight json.RawMessage `json:"latest_block_height"`
		} `json:"SyncInfo"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &response); err != nil {
		return 0, output, err
	}
	height, err := parseJSONInt(response.SyncInfo.LatestBlockHeight)
	return height, output, err
}

func (c *CrossVersion) processStateOn(node string) (string, error) {
	result := runDocker(node, "", "sh", "-c", `
for comm in /proc/[0-9]*/comm; do
  [ -r "$comm" ] || continue
  read -r name <"$comm" || continue
  if [ "$name" = seid ]; then
    process_dir="${comm%/comm}"
    read -r stat_pid stat_comm process_state stat_rest <"$process_dir/stat" || continue
    [ "$process_state" = Z ] || { printf running; exit 0; }
  fi
done
printf stopped`)
	if result.Err != nil {
		return "", fmt.Errorf("%w: %s", result.Err, result.Combined())
	}
	state := strings.TrimSpace(result.Stdout)
	if state != "running" && state != "stopped" {
		return "", fmt.Errorf("invalid process state %q", state)
	}
	return state, nil
}

func (c *CrossVersion) load(t *testing.T) {
	t.Helper()
	content, err := os.ReadFile(c.artifactPath)
	if err != nil {
		t.Fatalf("read cross-version artifact %s: %v", c.artifactPath, err)
	}
	var artifact crossVersionArtifact
	if err := json.Unmarshal(content, &artifact); err != nil {
		t.Fatalf("decode cross-version artifact %s: %v", c.artifactPath, err)
	}
	if artifact.Values == nil {
		t.Fatalf("cross-version artifact %s has no values", c.artifactPath)
	}
	c.values = artifact.Values
}

func (c *CrossVersion) save(t *testing.T) {
	t.Helper()
	content, err := json.MarshalIndent(crossVersionArtifact{Values: c.values}, "", "  ")
	if err != nil {
		t.Fatalf("encode cross-version artifact: %v", err)
	}
	content = append(content, '\n')
	if err := os.MkdirAll(filepath.Dir(c.artifactPath), 0o750); err != nil {
		t.Fatalf("create cross-version artifact directory: %v", err)
	}
	temporary := c.artifactPath + ".tmp"
	if err := os.WriteFile(temporary, content, 0o600); err != nil {
		t.Fatalf("write cross-version artifact: %v", err)
	}
	if err := os.Rename(temporary, c.artifactPath); err != nil {
		t.Fatalf("publish cross-version artifact: %v", err)
	}
}

func runDocker(node, input string, command ...string) CommandResult {
	args := []string{"exec"}
	if input != "" {
		args = append(args, "-i")
	}
	args = append(args, node)
	args = append(args, command...)

	cmd := exec.Command("docker", args...) //nolint:gosec // test-controlled commands run only in disposable validators
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return CommandResult{Stdout: stdout.String(), Stderr: stderr.String(), Err: err}
}

func runDockerDetached(node string, command ...string) CommandResult {
	args := append([]string{"exec", "-d", node}, command...)
	cmd := exec.Command("docker", args...) //nolint:gosec // test-controlled commands run only in disposable validators
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return CommandResult{Stdout: stdout.String(), Stderr: stderr.String(), Err: err}
}

func extractGenesis(output []byte) (ExportedGenesis, error) {
	offset := 0
	for _, line := range bytes.SplitAfter(output, []byte{'\n'}) {
		candidate := bytes.TrimLeft(line, " \t\r\n")
		if len(candidate) == 0 || candidate[0] != '{' {
			offset += len(line)
			continue
		}
		start := offset + len(line) - len(candidate)
		var genesis ExportedGenesis
		decoder := json.NewDecoder(bytes.NewReader(output[start:]))
		if err := decoder.Decode(&genesis); err == nil && genesis.AppState != nil {
			return genesis, nil
		}
		offset += len(line)
	}
	return ExportedGenesis{}, fmt.Errorf("no genesis document found")
}

func parseJSONInt(encoded json.RawMessage) (int64, error) {
	if len(encoded) == 0 {
		return 0, fmt.Errorf("value is empty")
	}
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err == nil {
		return strconv.ParseInt(number.String(), 10, 64)
	}
	var text string
	if err := json.Unmarshal(encoded, &text); err != nil {
		return 0, fmt.Errorf("decode integer %s: %w", encoded, err)
	}
	return strconv.ParseInt(text, 10, 64)
}

func parseBroadcastTxHash(stdout string) (string, error) {
	var resp struct {
		TxHash string `json:"txhash"`
	}
	if err := json.Unmarshal([]byte(stdout), &resp); err != nil {
		return "", fmt.Errorf("decode broadcast JSON: %w", err)
	}
	if resp.TxHash == "" {
		return "", fmt.Errorf("broadcast JSON has no txhash")
	}
	return resp.TxHash, nil
}

func parseDeliveredTx(stdout string) (DeliveredTx, error) {
	var resp struct {
		TxHash  string          `json:"txhash"`
		Height  json.RawMessage `json:"height"`
		Code    json.RawMessage `json:"code"`
		GasUsed json.RawMessage `json:"gas_used"`
		RawLog  string          `json:"raw_log"`
	}
	if err := json.Unmarshal([]byte(stdout), &resp); err != nil {
		return DeliveredTx{}, fmt.Errorf("decode included tx JSON: %w", err)
	}
	var delivered DeliveredTx
	delivered.Hash = resp.TxHash
	delivered.RawLog = resp.RawLog
	if len(resp.Height) > 0 {
		height, err := parseJSONInt(resp.Height)
		if err != nil {
			return DeliveredTx{}, fmt.Errorf("decode height: %w", err)
		}
		delivered.Height = height
	}
	if len(resp.Code) > 0 {
		code, err := parseJSONInt(resp.Code)
		if err != nil {
			return DeliveredTx{}, fmt.Errorf("decode code: %w", err)
		}
		delivered.Code = code
	}
	if len(resp.GasUsed) > 0 {
		gasUsed, err := parseJSONInt(resp.GasUsed)
		if err != nil {
			return DeliveredTx{}, fmt.Errorf("decode gas_used: %w", err)
		}
		delivered.GasUsed = gasUsed
	}
	return delivered, nil
}

func parseABCIQueryResponse(output []byte) ([]byte, uint32, string, error) {
	var envelope struct {
		Error  json.RawMessage `json:"error"`
		Result struct {
			Response struct {
				Code      json.RawMessage `json:"code"`
				Log       string          `json:"log"`
				Value     json.RawMessage `json:"value"`
				Codespace string          `json:"codespace"`
			} `json:"response"`
		} `json:"result"`
	}
	if err := json.Unmarshal(output, &envelope); err != nil {
		return nil, 0, "", fmt.Errorf("decode JSON-RPC envelope: %w", err)
	}
	if len(envelope.Error) > 0 && string(envelope.Error) != "null" {
		return nil, 0, "", fmt.Errorf("JSON-RPC error: %s", envelope.Error)
	}
	code := uint32(0)
	if len(envelope.Result.Response.Code) > 0 && string(envelope.Result.Response.Code) != "null" {
		parsed, err := parseJSONInt(envelope.Result.Response.Code)
		if err != nil {
			return nil, 0, "", fmt.Errorf("decode ABCI code: %w", err)
		}
		if parsed < 0 {
			return nil, 0, "", fmt.Errorf("negative ABCI code %d", parsed)
		}
		code = uint32(parsed)
	}
	value, err := decodeABCIBytes(envelope.Result.Response.Value)
	if err != nil {
		return nil, 0, "", err
	}
	return value, code, envelope.Result.Response.Log, nil
}

func decodeABCIBytes(raw json.RawMessage) ([]byte, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return nil, fmt.Errorf("decode ABCI bytes %s: %w", raw, err)
	}
	if text == "" {
		return nil, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(text)
	if err != nil {
		return nil, fmt.Errorf("decode ABCI value %q: %w", text, err)
	}
	return decoded, nil
}

type blockView struct {
	node      string
	appHash   []byte
	blockHash []byte
}

type parsedBlock struct {
	appHash   []byte
	blockHash []byte
	height    int64
}

func parseBlockIdentity(output []byte) (parsedBlock, error) {
	var envelope struct {
		Error  json.RawMessage `json:"error"`
		Result struct {
			BlockID struct {
				Hash string `json:"hash"`
			} `json:"block_id"`
			Block struct {
				Header struct {
					Height  json.RawMessage `json:"height"`
					AppHash string          `json:"app_hash"`
				} `json:"header"`
			} `json:"block"`
		} `json:"result"`
	}
	if err := json.Unmarshal(output, &envelope); err != nil {
		return parsedBlock{}, fmt.Errorf("decode JSON-RPC envelope: %w", err)
	}
	if len(envelope.Error) > 0 && string(envelope.Error) != "null" {
		return parsedBlock{}, fmt.Errorf("JSON-RPC error: %s", envelope.Error)
	}
	appHash, err := decodeRPCHex("app_hash", envelope.Result.Block.Header.AppHash)
	if err != nil {
		return parsedBlock{}, err
	}
	blockHash, err := decodeRPCHex("block_hash", envelope.Result.BlockID.Hash)
	if err != nil {
		return parsedBlock{}, err
	}
	height, err := parseJSONInt(envelope.Result.Block.Header.Height)
	if err != nil {
		return parsedBlock{}, fmt.Errorf("decode block height: %w", err)
	}
	return parsedBlock{appHash: appHash, blockHash: blockHash, height: height}, nil
}

func decodeRPCHex(label, text string) ([]byte, error) {
	text = strings.TrimPrefix(text, "0x")
	if text == "" {
		return nil, fmt.Errorf("block has no %s", label)
	}
	decoded, err := hex.DecodeString(text)
	if err != nil {
		return nil, fmt.Errorf("decode %s %q: %w", label, text, err)
	}
	return decoded, nil
}

func validatorBlockAgreementError(height int64, views []blockView) error {
	if len(views) < 2 {
		return fmt.Errorf("need at least two validators to compare at height %d", height)
	}
	first := views[0]
	for _, view := range views[1:] {
		if bytes.Equal(view.appHash, first.appHash) && bytes.Equal(view.blockHash, first.blockHash) {
			continue
		}
		return fmt.Errorf("%s", formatValidatorBlockDisagreement(height, views))
	}
	return nil
}

func formatValidatorBlockDisagreement(height int64, views []blockView) string {
	var b strings.Builder
	fmt.Fprintf(&b, "validators disagreed at height %d:", height)
	for _, view := range views {
		fmt.Fprintf(&b, "\n  %s app_hash=%x block_hash=%x", view.node, view.appHash, view.blockHash)
	}
	return b.String()
}

func requiredEnv(t *testing.T, name string) string {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		t.Fatalf("%s is required", name)
	}
	return value
}
