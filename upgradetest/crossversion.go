package upgradetest

import (
	"bytes"
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

// RequireTxSuccess checks the CheckTx result returned by a synchronous broadcast.
func (c *CrossVersion) RequireTxSuccess(t *testing.T, label string, result CommandResult) {
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
	height, output, err := c.tryHeight()
	if err != nil {
		t.Fatalf("query %s height: %v\n%s", c.node, err, output)
	}
	return height
}

// WaitForHeight waits until the primary validator reaches a committed height.
func (c *CrossVersion) WaitForHeight(t *testing.T, target int64, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastOutput string
	var lastErr error
	for time.Now().Before(deadline) {
		height, output, err := c.tryHeight()
		lastOutput, lastErr = output, err
		if err == nil && height >= target {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("%s did not reach height %d within %s: %v\n%s",
		c.node, target, timeout, lastErr, lastOutput)
}

// WaitForBlocks waits for the primary validator to commit more blocks.
func (c *CrossVersion) WaitForBlocks(t *testing.T, blocks int64) {
	t.Helper()
	c.WaitForHeight(t, c.Height(t)+blocks, 3*time.Minute)
}

// StopNode stops seid in the primary validator.
func (c *CrossVersion) StopNode(t *testing.T) {
	t.Helper()
	result := runDocker(c.node, "", "sh", "-c", `
for comm in /proc/[0-9]*/comm; do
  [ -r "$comm" ] || continue
  read -r name <"$comm" || continue
  if [ "$name" = seid ]; then
    process_dir="${comm%/comm}"
    read -r stat_pid stat_comm process_state stat_rest <"$process_dir/stat" || continue
    [ "$process_state" = Z ] && continue
    pid="${comm#/proc/}"
    pid="${pid%/comm}"
    kill -TERM "$pid"
  fi
done`)
	if result.Err != nil {
		t.Fatalf("stop seid in %s: %v\n%s", c.node, result.Err, result.Combined())
	}

	deadline := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) {
		state, err := c.processState()
		if err == nil && state == "stopped" {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("seid in %s did not stop within one minute", c.node)
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

func (c *CrossVersion) tryHeight() (int64, string, error) {
	result := c.Seid("", "status")
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

func (c *CrossVersion) processState() (string, error) {
	result := runDocker(c.node, "", "sh", "-c", `
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

func requiredEnv(t *testing.T, name string) string {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		t.Fatalf("%s is required", name)
	}
	return value
}
