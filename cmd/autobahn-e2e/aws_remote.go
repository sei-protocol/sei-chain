package main

import (
	"cmp"
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
	loads := state.AWS.loadHosts()
	if len(loads) == 0 {
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

	_, _ = fmt.Fprintln(a.stdout, "Preparing sei-load on every load instance, and Grafana and Prometheus on the first.")
	return a.forEachHost(ctx, loads, func(ctx context.Context, load awsHost) error {
		if err := a.cloneRemoteRepo(ctx, state, load); err != nil {
			return err
		}
		return a.startLoadHost(ctx, state, load, validators, loads)
	})
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
		"AUTOBAHN=true DOCKER_DETACH=true make docker-cluster-start-monitoring",
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
	nodeDirs := make([]string, 0, len(validators)+2)
	nodeDirs = append(nodeDirs, "genesis.json", "persistent_peers.txt")
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

// startLoadHost writes the sei-load config on load and installs sei-load there. The first load
// instance also runs Prometheus, scraping every validator and every sei-load, and Grafana.
func (a *application) startLoadHost(ctx context.Context, state clusterState, load awsHost, validators, loads []awsHost) error {
	privateIPs := make([]string, len(validators))
	for i, host := range validators {
		privateIPs[i] = host.PrivateIP
	}
	loadIPs := make([]string, len(loads))
	for i, host := range loads {
		loadIPs[i] = host.PrivateIP
	}
	loadCfg, err := seiLoadAWSConfigFor(privateIPs, state.AWS.Load)
	if err != nil {
		return err
	}
	steps := []string{
		"set -euo pipefail",
		"mkdir -p " + shellQuote(filepath.Join(state.AWS.RemoteDir, "build/generated")) + " " + shellQuote(filepath.Join(state.AWS.RemoteDir, "build/tools")),
		"cat > " + shellQuote(filepath.Join(state.AWS.RemoteDir, "build/generated/prometheus.yml")) + " <<'EOF'\n" + prometheusScrapeConfigFor(privateIPs, loadIPs) + "EOF",
		"cat > " + shellQuote(filepath.Join(state.AWS.RemoteDir, "integration_test/autobahn/sei-load.aws.json")) + " <<'EOF'\n" + loadCfg + "EOF",
	}
	if state.AWS.Load.Binary == "" {
		steps = append(steps,
			"cd "+shellQuote(state.AWS.RemoteDir),
			"GOBIN=\"$PWD/build/tools\" go install github.com/sei-protocol/sei-load@"+shellQuote(state.AWS.Load.version()),
		)
	}
	if err := a.remoteStream(ctx, state, load, strings.Join(steps, "\n")); err != nil {
		return fmt.Errorf("prepare load instance %d: %w", load.Index, err)
	}
	if binary := state.AWS.Load.Binary; binary != "" {
		if err := a.uploadToHost(ctx, state, load, expandHome(binary), filepath.Join(state.AWS.RemoteDir, "build/tools/sei-load")); err != nil {
			return fmt.Errorf("upload sei-load to load instance %d: %w", load.Index, err)
		}
	}
	if primary, ok := state.AWS.loadHost(); !ok || primary.InstanceID != load.InstanceID {
		return nil
	}
	if err := a.remoteStream(ctx, state, load, remoteMake(state, "docker-aws-load-start", nil)); err != nil {
		return fmt.Errorf("start load-host monitoring: %w", err)
	}
	return nil
}

// remoteMake runs a make target in the remote checkout with the cluster's node environment and
// extra, which wins on a shared key.
func remoteMake(state clusterState, target string, extra map[string]string) string {
	parts := []string{
		"cd " + shellQuote(state.AWS.RemoteDir),
		"AUTOBAHN=true",
	}
	env := make(map[string]string, len(state.AWS.NodeEnv)+len(extra))
	for key, value := range state.AWS.NodeEnv {
		env[key] = value
	}
	for key, value := range extra {
		env[key] = value
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts[1] += " " + key + "=" + shellQuote(env[key])
	}
	parts[1] += " make " + target
	return strings.Join(parts, " && ")
}

func prometheusScrapeConfig(privateIPs []string) string {
	return prometheusScrapeConfigFor(privateIPs, nil)
}

// prometheusScrapeConfigFor scrapes every validator and, when loadIPs is set, every sei-load.
func prometheusScrapeConfigFor(privateIPs, loadIPs []string) string {
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
	if len(loadIPs) > 0 {
		b.WriteString("  - job_name: sei-load\n")
		b.WriteString("    metrics_path: /metrics\n")
		b.WriteString("    static_configs:\n")
		b.WriteString("      - targets:\n")
		for _, ip := range loadIPs {
			fmt.Fprintf(&b, "          - '%s:%d'\n", ip, seiLoadMetricsPort)
		}
		b.WriteString("    scrape_interval: 5s\n")
	}
	return b.String()
}

func seiLoadAWSConfig(privateIPs []string) (string, error) {
	return seiLoadAWSConfigFor(privateIPs, seiLoadProfile{})
}

// seiLoadAWSConfigFor is the sei-load config that sends profile's traffic to every validator.
func seiLoadAWSConfigFor(privateIPs []string, profile seiLoadProfile) (string, error) {
	scheme, port := "http", awsEVMPort
	if profile.WebSocket {
		scheme, port = "ws", awsEVMWSPort
	}
	endpoints := make([]string, len(privateIPs))
	for i, ip := range privateIPs {
		endpoints[i] = fmt.Sprintf("%s://%s:%d", scheme, ip, port)
	}
	accounts := map[string]any{
		"count":          cmp.Or(profile.Accounts, defaultSeiLoadAccounts),
		"newAccountRate": 0,
	}
	if profile.Disjoint {
		accounts["disjoint"] = true
	}
	cfg := map[string]any{
		"chainId":    713715,
		"seiChainID": "autobahn-evmonly",
		"endpoints":  endpoints,
		"accounts":   accounts,
		"scenarios": []map[string]any{
			{"name": "EVMTransfer", "weight": 1, "gasPicker": map[string]any{"Name": "fixed", "Gas": 21000}},
		},
		"settings": map[string]any{
			"tps":                  cmp.Or(profile.TPS, defaultSeiLoadTPS),
			"gasFeeCapFallbackWei": "20000000000",
			"trackReceipts":        false,
			"trackBlocks":          false,
			"trackUserLatency":     false,
			"prewarm":              false,
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

// uploadToHost copies a local file to dst on host.
func (a *application) uploadToHost(ctx context.Context, state clusterState, host awsHost, localPath, dstPath string) error {
	args := []string{
		"-i", expandHome(state.AWS.SSHKeyPath),
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=10",
		"-o", "StrictHostKeyChecking=accept-new",
		localPath, remoteSSHPath(state, host, dstPath),
	}
	return a.runner.stream(ctx, commandSpec{name: "scp", args: args})
}
