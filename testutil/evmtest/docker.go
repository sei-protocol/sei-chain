package evmtest

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type DockerTxConfig struct {
	Container string
	Password  string
	From      string
	Recipient string
	ChainID   string
	EVMRPCURL string
}

func DefaultDockerTxConfig() DockerTxConfig {
	return DockerTxConfig{
		Container: "sei-node-0",
		Password:  "12345678",
		From:      "admin",
		Recipient: "0xF87A299e6bC7bEba58dbBe5a5Aa21d49bCD16D52",
		ChainID:   "sei",
		EVMRPCURL: "http://localhost:8545",
	}
}

func ConfigFromEnv(prefix string) DockerTxConfig {
	cfg := DefaultDockerTxConfig()
	if v := os.Getenv(prefix + "CONTAINER"); v != "" {
		cfg.Container = v
	}
	if v := os.Getenv(prefix + "PASSWORD"); v != "" {
		cfg.Password = v
	}
	if v := os.Getenv(prefix + "FROM"); v != "" {
		cfg.From = v
	}
	if v := os.Getenv(prefix + "RECIPIENT"); v != "" {
		cfg.Recipient = v
	}
	if v := os.Getenv(prefix + "CHAIN_ID"); v != "" {
		cfg.ChainID = v
	}
	if v := os.Getenv(prefix + "EVM_RPC_URL"); v != "" {
		cfg.EVMRPCURL = v
	}
	return cfg
}

// SendTinyEvmTx submits a dust EVM transfer whose only purpose is to force one
// real block under allow_empty_blocks=false. Callers use it as a liveness/head
// trigger, not to validate transfer semantics.
func SendTinyEvmTx(ctx context.Context, cfg DockerTxConfig) (string, error) {
	cmd := exec.CommandContext( //nolint:gosec // test-only helper invokes docker with fixed argv shape
		ctx,
		"docker", "exec",
		"--env", fmt.Sprintf("SEI_EVM_PASSWORD=%s", cfg.Password),
		cfg.Container,
		"/bin/bash", "-c",
		`export PATH=$PATH:/root/go/bin && printf "%s\n" "$SEI_EVM_PASSWORD" | "$@"`,
		"bash",
		"seid", "tx", "evm", "send", cfg.Recipient, "1",
		"--from", cfg.From,
		"--chain-id", cfg.ChainID,
		"--evm-rpc", cfg.EVMRPCURL,
		"-b", "sync",
		"-y",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("send tiny evm tx: %w\n%s", err, out)
	}
	return parseTxHash(string(out))
}

func parseTxHash(output string) (string, error) {
	for _, line := range strings.Split(output, "\n") {
		if hash, ok := strings.CutPrefix(strings.TrimSpace(line), "Transaction hash: "); ok {
			return strings.TrimSpace(hash), nil
		}
	}
	return "", fmt.Errorf("transaction hash not found in output:\n%s", output)
}
