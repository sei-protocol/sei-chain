package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeRunner struct {
	mu       sync.Mutex
	commands []commandSpec
	outputFn func(commandSpec) (string, error)
	streamFn func(commandSpec) error
	paths    map[string]error
}

func (r *fakeRunner) output(_ context.Context, spec commandSpec) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commands = append(r.commands, spec)
	if r.outputFn == nil {
		return "", nil
	}
	return r.outputFn(spec)
}

func (r *fakeRunner) stream(_ context.Context, spec commandSpec) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commands = append(r.commands, spec)
	if r.streamFn == nil {
		return nil
	}
	return r.streamFn(spec)
}

func (r *fakeRunner) lookPath(name string) error {
	if err, ok := r.paths[name]; ok {
		return err
	}
	return nil
}

func TestStateStoreRoundTrip(t *testing.T) {
	store := newStateStore(t.TempDir())
	want := clusterState{
		Version:   stateVersion,
		Name:      "test-cluster",
		Target:    "local",
		Status:    "ready",
		CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
		RepoRoot:  "/tmp/sei-chain",
		Nodes:     clusterNodes(4),
	}

	require.NoError(t, store.save(want))
	got, err := store.load(want.Name)
	require.NoError(t, err)
	require.Equal(t, want, got)
	info, err := os.Stat(store.path(want.Name))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	states, err := store.loadAll()
	require.NoError(t, err)
	require.Equal(t, []clusterState{want}, states)
	require.NoError(t, store.remove(want.Name))
}

func TestClusterNodesMatchDockerComposePorts(t *testing.T) {
	require.Equal(t, []node{
		{Index: 0, Name: "node-0", Container: "sei-node-0", EVMHostPort: 8545},
		{Index: 1, Name: "node-1", Container: "sei-node-1", EVMHostPort: 8547},
		{Index: 2, Name: "node-2", Container: "sei-node-2", EVMHostPort: 8549},
		{Index: 3, Name: "node-3", Container: "sei-node-3", EVMHostPort: 8551},
	}, clusterNodes(4))
}

func TestFindNodeAcceptsNamesAndIndexes(t *testing.T) {
	nodes := clusterNodes(4)
	for _, selector := range []string{"2", "node-2", "sei-node-2"} {
		got, err := findNode(nodes, selector)
		require.NoError(t, err)
		require.Equal(t, nodes[2], got)
	}
	_, err := findNode(nodes, "4")
	require.Error(t, err)
}

func TestAWSDeployCreatesManagedResourcesAndReadyState(t *testing.T) {
	stateDir := t.TempDir()
	runner := &fakeRunner{}
	runner.outputFn = func(spec commandSpec) (string, error) {
		joined := strings.Join(spec.args, " ")
		switch {
		case strings.Contains(joined, "sts get-caller-identity"):
			return `{}`, nil
		case strings.Contains(joined, "describe-vpcs"):
			return "vpc-123\n", nil
		case strings.Contains(joined, "create-security-group"):
			return "sg-123\n", nil
		case strings.Contains(joined, "create-key-pair"):
			return "-----BEGIN OPENSSH PRIVATE KEY-----\ntest\n-----END OPENSSH PRIVATE KEY-----\n", nil
		case strings.Contains(joined, "run-instances"):
			if strings.Contains(joined, "Value=load") {
				return "i-load\n", nil
			}
			return "i-v0\ti-v1\ti-v2\ti-v3\n", nil
		case strings.Contains(joined, "describe-instances"):
			return "i-v0\t203.0.113.10\t10.0.0.10\ni-v1\t203.0.113.11\t10.0.0.11\ni-v2\t203.0.113.12\t10.0.0.12\ni-v3\t203.0.113.13\t10.0.0.13\ni-load\t203.0.113.20\t10.0.0.20\n", nil
		case spec.name == "ssh":
			return "", nil
		default:
			return "", nil
		}
	}
	var stdout, stderr bytes.Buffer
	app := &application{runner: runner, stdout: &stdout, stderr: &stderr, stateDir: stateDir}
	options := deployOptions{
		name:             "aws-test",
		target:           "aws",
		timeout:          time.Minute,
		region:           "us-west-2",
		instanceType:     "r7i.12xlarge",
		amiID:            "ami-123",
		sshCIDR:          "198.51.100.4/32",
		sshUser:          "ubuntu",
		volumeSize:       defaultVolumeSizeGiB,
		volumeIOPS:       defaultVolumeIOPS,
		volumeThroughput: defaultVolumeThroughputMB,
		repoURL:          "https://github.com/sei-protocol/sei-chain.git",
		ref:              "deadbeef",
		topology:         awsTopologyDistributed,
	}

	require.NoError(t, app.deploy(context.Background(), options))
	state, err := app.store().load(options.name)
	require.NoError(t, err)
	require.Equal(t, "ready", state.Status)
	require.Equal(t, "i-load", state.AWS.InstanceID)
	require.Equal(t, "203.0.113.20", state.AWS.PublicIP)
	require.Len(t, state.AWS.Hosts, 5)
	require.Len(t, state.AWS.validators(), 4)
	require.True(t, state.AWS.ManagedKey)
	require.FileExists(t, state.AWS.SSHKeyPath)
	keyInfo, err := os.Stat(state.AWS.SSHKeyPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), keyInfo.Mode().Perm())
	require.Contains(t, stdout.String(), "Cluster aws-test is ready")
	require.Contains(t, stdout.String(), "Grafana: http://203.0.113.20:3000")
	require.Contains(t, stdout.String(), "passed status checks")

	commands := joinedCommands(runner.commands)
	require.Contains(t, commands, "authorize-security-group-ingress")
	require.Contains(t, commands, "--cidr 198.51.100.4/32")
	require.Contains(t, commands, "--port 3000")
	require.Contains(t, commands, "--port 22")
	require.NotContains(t, commands, "--cidr 0.0.0.0/0")
	require.Contains(t, commands, "UserIdGroupPairs")
	require.Contains(t, commands, "autobahn-e2e-genesis.tgz' genesis.json persistent_peers.txt")
	require.Contains(t, commands, "--count 4")
	require.Contains(t, commands, "--count 1")
	require.Contains(t, commands, "docker-aws-validator-init")
	require.Contains(t, commands, "docker-aws-validator-genesis")
	require.Contains(t, commands, "docker-aws-validator-start")
	require.Contains(t, commands, "docker-aws-load-start")
	require.Contains(t, commands, "sei-load.aws.json")
	require.NotContains(t, commands, "metricsListenAddr")
	require.Contains(t, stdout.String(), "sei-load is not running")
	require.Contains(t, commands, ebsRootMapping(defaultVolumeSizeGiB, defaultVolumeIOPS, defaultVolumeThroughputMB))
	require.Contains(t, commands, ebsRootMapping(defaultLoadVolumeSizeGiB, defaultLoadVolumeIOPS, defaultLoadVolumeThroughputMB))
	require.Contains(t, commands, "AUTOBAHN_EVMONLY=true")
	require.Contains(t, commands, "-o StrictHostKeyChecking=accept-new")
	require.Contains(t, commands, "curl -fsS -o /dev/null http://127.0.0.1:3000/api/health")
	require.Equal(t, awsTopologyDistributed, state.AWS.Topology)
}

func TestAWSDeployColocatedUsesOneInstanceAndSharedCompose(t *testing.T) {
	stateDir := t.TempDir()
	runner := &fakeRunner{}
	runner.outputFn = func(spec commandSpec) (string, error) {
		joined := strings.Join(spec.args, " ")
		switch {
		case strings.Contains(joined, "sts get-caller-identity"):
			return `{}`, nil
		case strings.Contains(joined, "describe-vpcs"):
			return "vpc-123\n", nil
		case strings.Contains(joined, "create-security-group"):
			return "sg-123\n", nil
		case strings.Contains(joined, "create-key-pair"):
			return "-----BEGIN OPENSSH PRIVATE KEY-----\ntest\n-----END OPENSSH PRIVATE KEY-----\n", nil
		case strings.Contains(joined, "run-instances"):
			return "i-colo\n", nil
		case strings.Contains(joined, "describe-instances"):
			return "i-colo\t203.0.113.10\t10.0.0.10\n", nil
		case spec.name == "ssh":
			return "", nil
		default:
			return "", nil
		}
	}
	var stdout bytes.Buffer
	app := &application{runner: runner, stdout: &stdout, stderr: &bytes.Buffer{}, stateDir: stateDir}
	require.NoError(t, app.deploy(context.Background(), deployOptions{
		name:             "colo-test",
		target:           "aws",
		timeout:          time.Minute,
		region:           "us-west-2",
		instanceType:     "r7i.12xlarge",
		amiID:            "ami-123",
		sshCIDR:          "198.51.100.4/32",
		sshUser:          "ubuntu",
		volumeSize:       defaultVolumeSizeGiB,
		volumeIOPS:       defaultVolumeIOPS,
		volumeThroughput: defaultVolumeThroughputMB,
		repoURL:          "https://github.com/sei-protocol/sei-chain.git",
		ref:              "deadbeef",
		topology:         awsTopologyColocated,
	}))
	state, err := app.store().load("colo-test")
	require.NoError(t, err)
	require.Equal(t, "ready", state.Status)
	require.Equal(t, awsTopologyColocated, state.AWS.Topology)
	require.Equal(t, "i-colo", state.AWS.InstanceID)
	require.Equal(t, "203.0.113.10", state.AWS.PublicIP)
	require.Len(t, state.AWS.Hosts, 1)
	require.Empty(t, state.AWS.validators())
	require.Contains(t, stdout.String(), "four Docker validators")
	require.Contains(t, stdout.String(), "Grafana: http://203.0.113.10:3000")

	commands := joinedCommands(runner.commands)
	require.Contains(t, commands, "docker-cluster-start-monitoring")
	require.NotContains(t, commands, "docker-aws-validator-init")
	require.NotContains(t, commands, "UserIdGroupPairs")
	require.NotContains(t, commands, "--count 4")
}

func TestAWSDeployRetainsFailedState(t *testing.T) {
	stateDir := t.TempDir()
	runner := &fakeRunner{}
	runner.outputFn = func(spec commandSpec) (string, error) {
		joined := strings.Join(spec.args, " ")
		switch {
		case strings.Contains(joined, "sts get-caller-identity"):
			return `{}`, nil
		case strings.Contains(joined, "describe-vpcs"):
			return "vpc-123", nil
		case strings.Contains(joined, "create-security-group"):
			return "sg-123", nil
		case strings.Contains(joined, "create-key-pair"):
			return "key material", nil
		case strings.Contains(joined, "run-instances"):
			return "", errors.New("capacity unavailable")
		default:
			return "", nil
		}
	}
	app := &application{runner: runner, stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}, stateDir: stateDir}
	err := app.deploy(context.Background(), deployOptions{
		name:             "failed-aws",
		target:           "aws",
		timeout:          time.Minute,
		region:           "us-west-2",
		instanceType:     "r7i.12xlarge",
		amiID:            "ami-123",
		sshCIDR:          "198.51.100.4/32",
		sshUser:          "ubuntu",
		volumeSize:       defaultVolumeSizeGiB,
		volumeIOPS:       defaultVolumeIOPS,
		volumeThroughput: defaultVolumeThroughputMB,
		repoURL:          "https://example.com/repo.git",
		ref:              "deadbeef",
	})
	require.Error(t, err)
	state, loadErr := app.store().load("failed-aws")
	require.NoError(t, loadErr)
	require.Equal(t, "failed", state.Status)
	require.Equal(t, "sg-123", state.AWS.SecurityGroupID)
}

func TestAWSForwardUsesChosenNodePort(t *testing.T) {
	stateDir := t.TempDir()
	state := clusterState{
		Version: stateVersion,
		Name:    "forward-test",
		Target:  "aws",
		Status:  "ready",
		Nodes:   clusterNodes(4),
		AWS: &awsState{
			PublicIP:   "203.0.113.10",
			SSHUser:    "ubuntu",
			SSHKeyPath: "/tmp/test.pem",
			Hosts: []awsHost{
				{Role: awsRoleValidator, Index: 0, PublicIP: "203.0.113.10"},
				{Role: awsRoleValidator, Index: 1, PublicIP: "203.0.113.11"},
				{Role: awsRoleValidator, Index: 2, PublicIP: "203.0.113.12"},
				{Role: awsRoleValidator, Index: 3, PublicIP: "203.0.113.13"},
			},
		},
	}
	require.NoError(t, newStateStore(stateDir).save(state))
	runner := &fakeRunner{}
	app := &application{runner: runner, stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}, stateDir: stateDir}

	require.NoError(t, app.forward(context.Background(), forwardOptions{
		name:      state.Name,
		node:      "node-3",
		bind:      "127.0.0.1",
		localPort: 18545,
	}))

	require.Len(t, runner.commands, 1)
	require.Equal(t, "ssh", runner.commands[0].name)
	joined := strings.Join(runner.commands[0].args, " ")
	require.Contains(t, joined, "-L 127.0.0.1:18545:127.0.0.1:8545")
	require.True(t, strings.HasSuffix(joined, "ubuntu@203.0.113.13"))
}

func TestListShowsPartialAWSDeploymentWithoutCredentials(t *testing.T) {
	stateDir := t.TempDir()
	state := clusterState{
		Version: stateVersion,
		Name:    "partial-aws",
		Target:  targetAWS,
		Status:  "failed",
		Nodes:   clusterNodes(4),
		AWS: &awsState{
			Region: "us-west-2",
		},
	}
	require.NoError(t, newStateStore(stateDir).save(state))
	var stdout bytes.Buffer
	runner := &fakeRunner{outputFn: func(commandSpec) (string, error) {
		return "", errors.New("credentials should not be requested")
	}}
	app := &application{runner: runner, stdout: &stdout, stderr: &bytes.Buffer{}, stateDir: stateDir}

	require.NoError(t, app.list(context.Background(), listOptions{name: state.Name}))
	require.Contains(t, stdout.String(), "partial-aws")
	require.Contains(t, stdout.String(), "failed")
	require.Empty(t, runner.commands)
}

func TestAWSTeardownStopsMonitoringStack(t *testing.T) {
	stateDir := t.TempDir()
	state := clusterState{
		Version: stateVersion,
		Name:    "monitored-aws",
		Target:  targetAWS,
		Status:  "ready",
		Nodes:   clusterNodes(4),
		AWS: &awsState{
			Region:     "us-west-2",
			PublicIP:   "203.0.113.10",
			SSHUser:    "ubuntu",
			SSHKeyPath: "/tmp/test.pem",
			RemoteDir:  "/home/ubuntu/sei-chain-monitored-aws",
		},
	}
	store := newStateStore(stateDir)
	require.NoError(t, store.save(state))
	runner := &fakeRunner{outputFn: func(spec commandSpec) (string, error) {
		if strings.Contains(strings.Join(spec.args, " "), "sts get-caller-identity") {
			return `{}`, nil
		}
		return "", nil
	}}
	app := &application{runner: runner, stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}, stateDir: stateDir}

	require.NoError(t, app.teardown(context.Background(), teardownOptions{name: state.Name}))
	require.Contains(t, joinedCommands(runner.commands), "docker-cluster-stop-monitoring")
	require.Contains(t, joinedCommands(runner.commands), "if [ -d '/home/ubuntu/sei-chain-monitored-aws' ]")
}

func TestAWSTeardownToleratesAlreadyDeletedManagedResources(t *testing.T) {
	stateDir := t.TempDir()
	keyPath := filepath.Join(stateDir, "managed.pem")
	require.NoError(t, os.WriteFile(keyPath, []byte("key"), 0o600))
	state := clusterState{
		Version: stateVersion,
		Name:    "deleted-aws",
		Target:  targetAWS,
		Status:  "failed",
		Nodes:   clusterNodes(4),
		AWS: &awsState{
			Region:          "us-west-2",
			SecurityGroupID: "sg-deleted",
			KeyName:         "key-deleted",
			SSHKeyPath:      keyPath,
			ManagedKey:      true,
		},
	}
	store := newStateStore(stateDir)
	require.NoError(t, store.save(state))
	runner := &fakeRunner{outputFn: func(spec commandSpec) (string, error) {
		joined := strings.Join(spec.args, " ")
		if strings.Contains(joined, "sts get-caller-identity") {
			return `{}`, nil
		}
		return "", errors.New("InvalidResource.NotFound")
	}}
	app := &application{runner: runner, stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}, stateDir: stateDir}

	require.NoError(t, app.teardown(context.Background(), teardownOptions{name: state.Name}))
	require.NoFileExists(t, keyPath)
	require.NoFileExists(t, store.path(state.Name))
}

func TestLocalTeardownRemovesState(t *testing.T) {
	stateDir := t.TempDir()
	state := clusterState{
		Version:  stateVersion,
		Name:     "local-test",
		Target:   "local",
		Status:   "ready",
		RepoRoot: "/tmp/sei-chain",
		Nodes:    clusterNodes(4),
	}
	store := newStateStore(stateDir)
	require.NoError(t, store.save(state))
	runner := &fakeRunner{}
	app := &application{runner: runner, stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}, stateDir: stateDir}

	require.NoError(t, app.teardown(context.Background(), teardownOptions{name: state.Name}))
	require.Len(t, runner.commands, 1)
	require.Equal(t, "/tmp/sei-chain", runner.commands[0].dir)
	require.Equal(t, []string{"docker-cluster-stop"}, runner.commands[0].args)
	_, err := os.Stat(store.path(state.Name))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestParseAutobahnExecutedHeight(t *testing.T) {
	metrics := "# HELP tendermint_internal_autobahn_data_next_block Next block\n" +
		"tendermint_internal_autobahn_data_next_block{stage=\"execute\"} 43\n"
	require.Equal(t, "42", parseAutobahnExecutedHeight(metrics))
	require.Equal(t, "-", parseAutobahnExecutedHeight("not-prometheus"))
}

func TestWriteUserDataUsesSelectedSSHUser(t *testing.T) {
	path, err := writeUserData(t.TempDir(), "test", "ec2-user")
	require.NoError(t, err)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(data), "usermod -aG docker ec2-user")
	require.Contains(t, string(data), "go1.27.1")
	require.Contains(t, string(data), "/var/lib/autobahn-e2e-ready")
}

func TestEBSRootMapping(t *testing.T) {
	require.Equal(t,
		"DeviceName=/dev/sda1,Ebs={VolumeSize=1024,VolumeType=gp3,Iops=10000,Throughput=1000,DeleteOnTermination=true}",
		ebsRootMapping(1024, 10000, 1000),
	)
}

func TestAssignAWSHostsRequiresPublicAndPrivateIPs(t *testing.T) {
	_, err := assignAWSHosts([]string{"i-v0"}, []string{"i-load"}, map[string]instanceAddrs{
		"i-v0":   {publicIP: "203.0.113.10", privateIP: "10.0.0.10"},
		"i-load": {publicIP: "None", privateIP: "10.0.0.20"},
	})
	require.Error(t, err)

	hosts, err := assignAWSHosts([]string{"i-v0"}, []string{"i-load"}, map[string]instanceAddrs{
		"i-v0":   {publicIP: "203.0.113.10", privateIP: "10.0.0.10"},
		"i-load": {publicIP: "203.0.113.20", privateIP: "10.0.0.20"},
	})
	require.NoError(t, err)
	require.Equal(t, awsRoleValidator, hosts[0].Role)
	require.Equal(t, "10.0.0.10", hosts[0].PrivateIP)
	require.Equal(t, awsRoleLoad, hosts[1].Role)
	require.Equal(t, "203.0.113.20", hosts[1].PublicIP)
}

func TestPrometheusAndLoadConfigUsePrivateEVMEndpoints(t *testing.T) {
	prom := prometheusScrapeConfig([]string{"10.0.0.10", "10.0.0.11"})
	require.Contains(t, prom, "10.0.0.10:26660")
	require.Contains(t, prom, "10.0.0.11:26660")

	cfg, err := seiLoadAWSConfig([]string{"10.0.0.10", "10.0.0.11"})
	require.NoError(t, err)
	require.Contains(t, cfg, "http://10.0.0.10:8545")
	require.Contains(t, cfg, "http://10.0.0.11:8545")
}

func TestGrafanaPublicURL(t *testing.T) {
	require.Equal(t, "", grafanaPublicURL(""))
	require.Equal(t, "http://203.0.113.10:3000", grafanaPublicURL("203.0.113.10"))
}

func TestResolveGrafanaCIDR(t *testing.T) {
	got, err := resolveGrafanaCIDR("", "198.51.100.4/32")
	require.NoError(t, err)
	require.Equal(t, "198.51.100.4/32", got)

	got, err = resolveGrafanaCIDR("0.0.0.0/0", "198.51.100.4/32")
	require.NoError(t, err)
	require.Equal(t, "0.0.0.0/0", got)

	_, err = resolveGrafanaCIDR("not-a-cidr", "198.51.100.4/32")
	require.Error(t, err)
}

func TestAWSDeployGrafanaCIDRCanBeWidened(t *testing.T) {
	stateDir := t.TempDir()
	runner := &fakeRunner{}
	runner.outputFn = func(spec commandSpec) (string, error) {
		joined := strings.Join(spec.args, " ")
		switch {
		case strings.Contains(joined, "sts get-caller-identity"):
			return `{}`, nil
		case strings.Contains(joined, "describe-vpcs"):
			return "vpc-123\n", nil
		case strings.Contains(joined, "create-security-group"):
			return "sg-123\n", nil
		case strings.Contains(joined, "create-key-pair"):
			return "-----BEGIN OPENSSH PRIVATE KEY-----\ntest\n-----END OPENSSH PRIVATE KEY-----\n", nil
		case strings.Contains(joined, "run-instances"):
			if strings.Contains(joined, "Value=load") {
				return "i-load\n", nil
			}
			return "i-v0\ti-v1\ti-v2\ti-v3\n", nil
		case strings.Contains(joined, "describe-instances"):
			return "i-v0\t203.0.113.10\t10.0.0.10\ni-v1\t203.0.113.11\t10.0.0.11\ni-v2\t203.0.113.12\t10.0.0.12\ni-v3\t203.0.113.13\t10.0.0.13\ni-load\t203.0.113.20\t10.0.0.20\n", nil
		case spec.name == "ssh":
			return "", nil
		default:
			return "", nil
		}
	}
	app := &application{runner: runner, stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}, stateDir: stateDir}
	require.NoError(t, app.deploy(context.Background(), deployOptions{
		name:             "grafana-open",
		target:           "aws",
		timeout:          time.Minute,
		region:           "us-west-2",
		instanceType:     "r7i.12xlarge",
		amiID:            "ami-123",
		sshCIDR:          "198.51.100.4/32",
		grafanaCIDR:      "0.0.0.0/0",
		sshUser:          "ubuntu",
		volumeSize:       defaultVolumeSizeGiB,
		volumeIOPS:       defaultVolumeIOPS,
		volumeThroughput: defaultVolumeThroughputMB,
		repoURL:          "https://github.com/sei-protocol/sei-chain.git",
		ref:              "deadbeef",
		topology:         awsTopologyDistributed,
	}))
	require.Contains(t, joinedCommands(runner.commands), "--cidr 0.0.0.0/0")
	require.Contains(t, joinedCommands(runner.commands), "--port 3000")
}

func TestShellQuote(t *testing.T) {
	require.Equal(t, `'a'\''b'`, shellQuote("a'b"))
}

func TestStateStoreRejectsUnsafeClusterName(t *testing.T) {
	store := newStateStore(t.TempDir())
	require.Error(t, store.save(clusterState{Version: stateVersion, Name: "../escape"}))
	require.NoFileExists(t, filepath.Join(store.dir, "..", "escape.json"))
}

func joinedCommands(commands []commandSpec) string {
	parts := make([]string, len(commands))
	for i, command := range commands {
		parts[i] = command.name + " " + strings.Join(command.args, " ")
	}
	return strings.Join(parts, "\n")
}
