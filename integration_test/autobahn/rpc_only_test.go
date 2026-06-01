//go:build autobahn_integration

package autobahn

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	rpcOnlyContainer   = "sei-rpc-node"
	rpcOnlyBootTimeout = 5 * time.Minute
	rpcOnlyBootPoll    = 5 * time.Second

	// evmRPCURLOnContainerLocalhost is the EVM RPC address inside the
	// rpc-node container — used with `docker exec ... curl` for readiness
	// checks (the rpc-node's 8545 isn't host-published).
	evmRPCURLOnContainerLocalhost = "http://localhost:8545"
)

// setupRPCOnlyNode boots an autobahn rpc-only sidecar alongside the validator
// cluster. Backgrounded via cmd.Start() because `make run-rpc-node-skipbuild`
// uses `docker run --rm` (foreground until the container exits); the actual
// container detaches from this process once it starts.
//
// Uses run-rpc-node-skipbuild so the rpc-node reuses the seid binary the
// validator containers already compiled — skips a second multi-minute
// `go install` cycle. The autobahn role itself comes from mode = "full"
// in docker/rpcnode/config/config.toml (see IsAutobahnRPCOnly in
// sei-tendermint config).
func setupRPCOnlyNode() error {
	fmt.Println("=== Starting rpc-only sidecar ===")
	_ = runMake(nil, "kill-rpc-node") // best-effort cleanup

	cmd := exec.Command("make", "run-rpc-node-skipbuild")
	cmd.Env = append(os.Environ(), "AUTOBAHN=true", "CLUSTER_SIZE=4")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start make run-rpc-node-skipbuild: %w", err)
	}
	// Reap the process when it eventually exits (e.g. on container kill);
	// not blocking on Wait here since the container runs for the duration
	// of the test suite.
	go func() { _ = cmd.Wait() }()

	deadline := time.Now().Add(rpcOnlyBootTimeout)
	for time.Now().Before(deadline) {
		if rpcOnlyRunning() && rpcOnlyEVMReady() {
			fmt.Println("rpc-only sidecar is ready")
			return nil
		}
		time.Sleep(rpcOnlyBootPoll)
	}
	return fmt.Errorf("rpc-only sidecar didn't come up within %s", rpcOnlyBootTimeout)
}

func rpcOnlyRunning() bool {
	out, err := exec.Command("docker", "ps",
		"--filter", "name="+rpcOnlyContainer,
		"--filter", "status=running",
		"--format", "{{.Names}}").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == rpcOnlyContainer
}

func rpcOnlyEVMReady() bool {
	r, err := evmRPCInContainer(rpcOnlyContainer, "eth_chainId", []any{})
	return err == nil && r.Error == nil && len(r.Result) > 0
}

type evmRPCResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *evmRPCError    `json:"error,omitempty"`
}

type evmRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// evmRPCInContainer POSTs a JSON-RPC call to the given container's
// localhost:8545. The rpc-only container's 8545 isn't host-published; this
// is the only way to talk to it without changing the run target.
func evmRPCInContainer(container, method string, params any) (*evmRPCResponse, error) {
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": method, "params": params,
	})
	if err != nil {
		return nil, err
	}
	out, err := exec.Command("docker", "exec", container,
		"curl", "-sf", "-X", "POST",
		"-H", "content-type: application/json",
		"--data", string(body),
		evmRPCURLOnContainerLocalhost).Output()
	if err != nil {
		return nil, fmt.Errorf("docker exec curl: %v", err)
	}
	var r evmRPCResponse
	if err := json.Unmarshal(out, &r); err != nil {
		return nil, fmt.Errorf("decode (body=%s): %w", out, err)
	}
	return &r, nil
}
