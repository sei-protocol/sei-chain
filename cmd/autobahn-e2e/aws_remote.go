package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/sync/errgroup"
)

func (a *application) startRemoteCluster(ctx context.Context, state clusterState) error {
	if state.AWS.colocated() {
		return a.startRemoteColocatedCluster(ctx, state)
	}
	validators := state.AWS.validators()
	if len(validators) != awsValidatorCount {
		return fmt.Errorf("start remote cluster: expected %d validators, got %d", awsValidatorCount, len(validators))
	}
	load, ok := state.AWS.loadHost()
	if !ok {
		return fmt.Errorf("start remote cluster: load instance is missing")
	}

	_, _ = fmt.Fprintln(a.stdout, "Cloning, building, and initializing all validators in parallel.")
	if err := a.forEachHost(ctx, validators, func(ctx context.Context, host awsHost) error {
		if err := a.cloneRemoteRepo(ctx, state, host); err != nil {
			return err
		}
		return a.buildAndInitValidator(ctx, state, host)
	}); err != nil {
		return err
	}
	if err := a.collectValidatorArtifacts(ctx, state, validators); err != nil {
		return err
	}

	_, _ = fmt.Fprintln(a.stdout, "Generating genesis on validator 0.")
	if err := a.remoteStream(ctx, state, validators[0], remoteMake(state, "docker-aws-validator-genesis", nil)); err != nil {
		return fmt.Errorf("generate genesis: %w", err)
	}
	if err := a.distributeGenesis(ctx, state, validators); err != nil {
		return err
	}

	_, _ = fmt.Fprintln(a.stdout, "Starting one validator process on each instance.")
	if err := a.forEachHost(ctx, validators, func(ctx context.Context, host awsHost) error {
		return a.remoteStream(ctx, state, host, remoteMake(state, "docker-aws-validator-start", map[string]string{
			"ID":           strconv.Itoa(host.Index),
			"ADVERTISE_IP": host.PrivateIP,
		}))
	}); err != nil {
		return fmt.Errorf("start validators: %w", err)
	}

	_, _ = fmt.Fprintln(a.stdout, "Setting up Grafana and Prometheus on the load instance.")
	if err := a.cloneRemoteRepo(ctx, state, load); err != nil {
		return err
	}
	if err := a.startLoadHost(ctx, state, load, validators); err != nil {
		return err
	}
	return nil
}

func (a *application) startRemoteColocatedCluster(ctx context.Context, state clusterState) error {
	host, ok := state.AWS.loadHost()
	if !ok {
		return fmt.Errorf("start remote cluster: colocated instance is missing")
	}
	_, _ = fmt.Fprintln(a.stdout, "Cloning the repository and starting four Docker validators plus monitoring.")
	if err := a.cloneRemoteRepo(ctx, state, host); err != nil {
		return err
	}
	command := strings.Join([]string{
		"cd " + shellQuote(state.AWS.RemoteDir),
		"AUTOBAHN=true AUTOBAHN_EVMONLY=true DOCKER_DETACH=true make docker-cluster-start-monitoring",
	}, " && ")
	if err := a.remoteStream(ctx, state, host, command); err != nil {
		return fmt.Errorf("start colocated cluster: %w", err)
	}
	return nil
}

func (a *application) cloneRemoteRepo(ctx context.Context, state clusterState, host awsHost) error {
	command := strings.Join([]string{
		"git clone --filter=blob:none " + shellQuote(state.AWS.RepoURL) + " " + shellQuote(state.AWS.RemoteDir),
		"cd " + shellQuote(state.AWS.RemoteDir),
		"git checkout --detach " + shellQuote(state.AWS.Ref),
	}, " && ")
	if err := a.remoteStream(ctx, state, host, command); err != nil {
		return fmt.Errorf("clone repository on %s: %w", host.PublicIP, err)
	}
	return nil
}

func (a *application) buildAndInitValidator(ctx context.Context, state clusterState, host awsHost) error {
	command := strings.Join([]string{
		"cd " + shellQuote(state.AWS.RemoteDir),
		"make build-docker-node",
		remoteMake(state, "docker-aws-validator-init", map[string]string{
			"ID":           strconv.Itoa(host.Index),
			"ADVERTISE_IP": host.PrivateIP,
		}),
	}, " && ")
	if err := a.remoteStream(ctx, state, host, command); err != nil {
		return fmt.Errorf("build and init validator %d: %w", host.Index, err)
	}
	return nil
}

func (a *application) collectValidatorArtifacts(ctx context.Context, state clusterState, validators []awsHost) error {
	primary := validators[0]
	for _, host := range validators[1:] {
		bundle := fmt.Sprintf("/tmp/autobahn-e2e-node-%d.tgz", host.Index)
		create := strings.Join([]string{
			"tar -C " + shellQuote(filepath.Join(state.AWS.RemoteDir, "build/generated")) + " -czf " + shellQuote(bundle),
			fmt.Sprintf("node_%d gentx exported_keys genesis_accounts.txt persistent_peers.txt init.complete", host.Index),
		}, " ")
		if err := a.remoteStream(ctx, state, host, create); err != nil {
			return fmt.Errorf("bundle validator %d artifacts: %w", host.Index, err)
		}
		dest := fmt.Sprintf("/tmp/autobahn-e2e-node-%d.tgz", host.Index)
		if err := a.copyBetweenHosts(ctx, state, host, bundle, primary, dest); err != nil {
			return fmt.Errorf("copy validator %d artifacts: %w", host.Index, err)
		}
		merge := strings.Join([]string{
			"set -euo pipefail",
			fmt.Sprintf("mkdir -p /tmp/autobahn-e2e-merge-%d", host.Index),
			fmt.Sprintf("tar -C /tmp/autobahn-e2e-merge-%d -xzf %s", host.Index, dest),
			"GEN=" + shellQuote(filepath.Join(state.AWS.RemoteDir, "build/generated")),
			fmt.Sprintf("cp -a /tmp/autobahn-e2e-merge-%d/node_%d \"$GEN/\"", host.Index, host.Index),
			"cp /tmp/autobahn-e2e-merge-" + strconv.Itoa(host.Index) + "/gentx/* \"$GEN/gentx/\"",
			"cp /tmp/autobahn-e2e-merge-" + strconv.Itoa(host.Index) + "/exported_keys/* \"$GEN/exported_keys/\"",
			fmt.Sprintf("cat /tmp/autobahn-e2e-merge-%d/genesis_accounts.txt >> \"$GEN/genesis_accounts.txt\"", host.Index),
			fmt.Sprintf("cat /tmp/autobahn-e2e-merge-%d/persistent_peers.txt >> \"$GEN/persistent_peers.txt\"", host.Index),
			fmt.Sprintf("cat /tmp/autobahn-e2e-merge-%d/init.complete >> \"$GEN/init.complete\"", host.Index),
		}, "\n")
		if err := a.remoteStream(ctx, state, primary, merge); err != nil {
			return fmt.Errorf("merge validator %d artifacts: %w", host.Index, err)
		}
	}
	return nil
}

func (a *application) distributeGenesis(ctx context.Context, state clusterState, validators []awsHost) error {
	primary := validators[0]
	bundle := "/tmp/autobahn-e2e-genesis.tgz"
	nodeDirs := make([]string, 0, len(validators)+1)
	nodeDirs = append(nodeDirs, "genesis.json")
	for _, host := range validators {
		nodeDirs = append(nodeDirs, fmt.Sprintf("node_%d", host.Index))
	}
	create := "tar -C " + shellQuote(filepath.Join(state.AWS.RemoteDir, "build/generated")) + " -czf " + shellQuote(bundle) + " " + strings.Join(nodeDirs, " ")
	if err := a.remoteStream(ctx, state, primary, create); err != nil {
		return fmt.Errorf("bundle genesis: %w", err)
	}
	for _, host := range validators[1:] {
		if err := a.copyBetweenHosts(ctx, state, primary, bundle, host, bundle); err != nil {
			return fmt.Errorf("copy genesis to validator %d: %w", host.Index, err)
		}
		extract := "mkdir -p " + shellQuote(filepath.Join(state.AWS.RemoteDir, "build/generated")) +
			" && tar -C " + shellQuote(filepath.Join(state.AWS.RemoteDir, "build/generated")) + " -xzf " + shellQuote(bundle)
		if err := a.remoteStream(ctx, state, host, extract); err != nil {
			return fmt.Errorf("extract genesis on validator %d: %w", host.Index, err)
		}
	}
	return nil
}

func (a *application) startLoadHost(ctx context.Context, state clusterState, load awsHost, validators []awsHost) error {
	privateIPs := make([]string, len(validators))
	for i, host := range validators {
		privateIPs[i] = host.PrivateIP
	}
	prom := prometheusScrapeConfig(privateIPs)
	loadCfg, err := seiLoadAWSConfig(privateIPs)
	if err != nil {
		return err
	}
	write := strings.Join([]string{
		"set -euo pipefail",
		"mkdir -p " + shellQuote(filepath.Join(state.AWS.RemoteDir, "build/generated")),
		"cat > " + shellQuote(filepath.Join(state.AWS.RemoteDir, "build/generated/prometheus.yml")) + " <<'EOF'\n" + prom + "EOF",
		"cat > " + shellQuote(filepath.Join(state.AWS.RemoteDir, "integration_test/autobahn/sei-load.aws.json")) + " <<'EOF'\n" + loadCfg + "EOF",
	}, "\n")
	if err := a.remoteStream(ctx, state, load, write); err != nil {
		return fmt.Errorf("write load-host config: %w", err)
	}
	if err := a.remoteStream(ctx, state, load, remoteMake(state, "docker-aws-load-start", nil)); err != nil {
		return fmt.Errorf("start load-host monitoring: %w", err)
	}
	return nil
}

func remoteMake(state clusterState, target string, extra map[string]string) string {
	parts := []string{
		"cd " + shellQuote(state.AWS.RemoteDir),
		"AUTOBAHN=true AUTOBAHN_EVMONLY=true",
	}
	keys := make([]string, 0, len(extra))
	for key := range extra {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts[1] += " " + key + "=" + shellQuote(extra[key])
	}
	parts[1] += " make " + target
	return strings.Join(parts, " && ")
}

func prometheusScrapeConfig(privateIPs []string) string {
	var b strings.Builder
	b.WriteString("global:\n")
	b.WriteString("  scrape_interval: 15s\n")
	b.WriteString("  evaluation_interval: 15s\n\n")
	b.WriteString("scrape_configs:\n")
	b.WriteString("  - job_name: autobahn-e2e\n")
	b.WriteString("    metrics_path: /metrics\n")
	b.WriteString("    static_configs:\n")
	b.WriteString("      - targets:\n")
	for _, ip := range privateIPs {
		fmt.Fprintf(&b, "          - '%s:%d'\n", ip, awsMetricsPort)
	}
	b.WriteString("    scrape_interval: 5s\n")
	return b.String()
}

func seiLoadAWSConfig(privateIPs []string) (string, error) {
	endpoints := make([]string, len(privateIPs))
	for i, ip := range privateIPs {
		endpoints[i] = fmt.Sprintf("http://%s:%d", ip, awsEVMPort)
	}
	cfg := map[string]any{
		"chainId":    713715,
		"seiChainID": "autobahn-evmonly",
		"endpoints":  endpoints,
		"accounts": map[string]any{
			"count":          5000,
			"newAccountRate": 0,
		},
		"scenarios": []map[string]any{
			{"name": "EVMTransfer", "weight": 1},
		},
		"settings": map[string]any{
			"workers":               1,
			"tps":                   250,
			"statsInterval":         "5s",
			"bufferSize":            1000,
			"dryRun":                false,
			"debug":                 false,
			"trackReceipts":         false,
			"trackBlocks":           false,
			"trackUserLatency":      false,
			"prewarm":               false,
			"rampUp":                false,
			"postSummaryFlushDelay": "1s",
		},
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode sei-load config: %w", err)
	}
	return string(data) + "\n", nil
}

func (a *application) forEachHost(ctx context.Context, hosts []awsHost, fn func(context.Context, awsHost) error) error {
	g, ctx := errgroup.WithContext(ctx)
	for _, host := range hosts {
		host := host
		g.Go(func() error { return fn(ctx, host) })
	}
	return g.Wait()
}

func (a *application) remoteStream(ctx context.Context, state clusterState, host awsHost, command string) error {
	return a.runner.stream(ctx, sshCommandTo(state, host, command))
}

func (a *application) copyBetweenHosts(ctx context.Context, state clusterState, src awsHost, srcPath string, dst awsHost, dstPath string) error {
	args := append(scpBaseArgs(state), remoteSSHPath(state, src, srcPath), remoteSSHPath(state, dst, dstPath))
	return a.runner.stream(ctx, commandSpec{name: "scp", args: args})
}
