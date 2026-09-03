package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeRunner struct {
	commands []commandSpec
	outputFn func(commandSpec) (string, error)
	streamFn func(commandSpec) error
	paths    map[string]error
}

func (r *fakeRunner) output(_ context.Context, spec commandSpec) (string, error) {
	r.commands = append(r.commands, spec)
	if r.outputFn == nil {
		return "", nil
	}
	return r.outputFn(spec)
}

func (r *fakeRunner) stream(_ context.Context, spec commandSpec) error {
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

func TestNativeClusterNodesUseOneRPCPortPerHost(t *testing.T) {
	require.Equal(t, []node{
		{Index: 0, Name: "node-0", EVMHostPort: 8545},
		{Index: 1, Name: "node-1", EVMHostPort: 8545},
		{Index: 2, Name: "node-2", EVMHostPort: 8545},
		{Index: 3, Name: "node-3", EVMHostPort: 8545},
	}, nativeClusterNodes(4))
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
			return "i-100\ti-101\ti-102\ti-103\n", nil
		case strings.Contains(joined, "describe-instances"):
			return `[
  {"InstanceID":"i-100","PublicIP":"203.0.113.10","PrivateIP":"10.0.0.10"},
  {"InstanceID":"i-101","PublicIP":"203.0.113.11","PrivateIP":"10.0.0.11"},
  {"InstanceID":"i-102","PublicIP":"203.0.113.12","PrivateIP":"10.0.0.12"},
  {"InstanceID":"i-103","PublicIP":"203.0.113.13","PrivateIP":"10.0.0.13"}
]`, nil
		case spec.name == "ssh":
			return "", nil
		default:
			return "", nil
		}
	}
	var stdout, stderr bytes.Buffer
	app := &application{runner: runner, stdout: &stdout, stderr: &stderr, stateDir: stateDir}
	options := deployOptions{
		name:         "aws-test",
		target:       "aws",
		timeout:      time.Minute,
		region:       "us-west-2",
		instanceType: "c7g.2xlarge",
		amiID:        "ami-123",
		sshCIDR:      "198.51.100.4/32",
		sshUser:      "ubuntu",
		volumeSize:   100,
		repoURL:      "https://github.com/sei-protocol/sei-chain.git",
		ref:          "deadbeef",
	}

	require.NoError(t, app.deploy(context.Background(), options))
	state, err := app.store().load(options.name)
	require.NoError(t, err)
	require.Equal(t, "ready", state.Status)
	require.Equal(t, []awsInstanceState{
		{NodeIndex: 0, InstanceID: "i-100", PublicIP: "203.0.113.10", PrivateIP: "10.0.0.10"},
		{NodeIndex: 1, InstanceID: "i-101", PublicIP: "203.0.113.11", PrivateIP: "10.0.0.11"},
		{NodeIndex: 2, InstanceID: "i-102", PublicIP: "203.0.113.12", PrivateIP: "10.0.0.12"},
		{NodeIndex: 3, InstanceID: "i-103", PublicIP: "203.0.113.13", PrivateIP: "10.0.0.13"},
	}, state.AWS.Instances)
	require.Equal(t, nativeClusterNodes(4), state.Nodes)
	require.True(t, state.AWS.ManagedKey)
	require.FileExists(t, state.AWS.SSHKeyPath)
	keyInfo, err := os.Stat(state.AWS.SSHKeyPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), keyInfo.Mode().Perm())
	require.Contains(t, stdout.String(), "Cluster aws-test is ready")

	commands := joinedCommands(runner.commands)
	require.Contains(t, commands, "authorize-security-group-ingress")
	require.Contains(t, commands, "--cidr 198.51.100.4/32")
	require.Contains(t, commands, "--count 4")
	require.Contains(t, commands, "UserIdGroupPairs=[{GroupId=sg-123}]")
	require.Contains(t, commands, "prepare_native_cluster.sh")
	require.Contains(t, commands, "install_native_node.sh")
	require.Contains(t, commands, "systemctl start seid.service")
	require.NotContains(t, commands, "docker")
	require.Contains(t, commands, "-o StrictHostKeyChecking=accept-new")
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
		name:         "failed-aws",
		target:       "aws",
		timeout:      time.Minute,
		region:       "us-west-2",
		instanceType: "c7g.2xlarge",
		amiID:        "ami-123",
		sshCIDR:      "198.51.100.4/32",
		sshUser:      "ubuntu",
		volumeSize:   100,
		repoURL:      "https://example.com/repo.git",
		ref:          "deadbeef",
	})
	require.Error(t, err)
	state, loadErr := app.store().load("failed-aws")
	require.NoError(t, loadErr)
	require.Equal(t, "failed", state.Status)
	require.Equal(t, "sg-123", state.AWS.SecurityGroupID)
}

func TestAWSRuntimeOptionValidation(t *testing.T) {
	parameter, err := ubuntuAMIParameter("amd64")
	require.NoError(t, err)
	require.Equal(t, ubuntuAMD64AMIParameter, parameter)
	parameter, err = ubuntuAMIParameter("arm64")
	require.NoError(t, err)
	require.Equal(t, ubuntuARM64AMIParameter, parameter)
	_, err = ubuntuAMIParameter("riscv64")
	require.Error(t, err)

	for _, value := range []string{"off", "0", "100", "200"} {
		require.NoError(t, validateGoGC(value))
	}
	for _, value := range []string{"", "-1", "invalid"} {
		require.Error(t, validateGoGC(value))
	}
}

func TestDescribeAWSInstancesHandlesMissingPublicIP(t *testing.T) {
	runner := &fakeRunner{outputFn: func(commandSpec) (string, error) {
		return `[{"InstanceID":"i-100","PublicIP":null,"PrivateIP":"10.0.0.10"}]`, nil
	}}
	instances, err := describeAWSInstances(t.Context(), awsClient{runner: runner}, []string{"i-100"})
	require.NoError(t, err)
	require.Equal(t, awsInstanceState{InstanceID: "i-100", PrivateIP: "10.0.0.10"}, instances["i-100"])
}

func TestAWSForwardUsesChosenNodePort(t *testing.T) {
	stateDir := t.TempDir()
	state := clusterState{
		Version: stateVersion,
		Name:    "forward-test",
		Target:  "aws",
		Status:  "ready",
		Nodes:   nativeClusterNodes(4),
		AWS: &awsState{
			SSHUser:    "ubuntu",
			SSHKeyPath: "/tmp/test.pem",
			Instances: []awsInstanceState{
				{NodeIndex: 0, InstanceID: "i-100", PublicIP: "203.0.113.10", PrivateIP: "10.0.0.10"},
				{NodeIndex: 1, InstanceID: "i-101", PublicIP: "203.0.113.11", PrivateIP: "10.0.0.11"},
				{NodeIndex: 2, InstanceID: "i-102", PublicIP: "203.0.113.12", PrivateIP: "10.0.0.12"},
				{NodeIndex: 3, InstanceID: "i-103", PublicIP: "203.0.113.13", PrivateIP: "10.0.0.13"},
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

func TestListInspectsEachNativeAWSInstance(t *testing.T) {
	stateDir := t.TempDir()
	state := clusterState{
		Version: stateVersion,
		Name:    "native-aws",
		Target:  targetAWS,
		Status:  "ready",
		Nodes:   nativeClusterNodes(4),
		AWS: &awsState{
			Region:     "us-west-2",
			SSHUser:    "ubuntu",
			SSHKeyPath: "/tmp/test.pem",
			Instances: []awsInstanceState{
				{NodeIndex: 0, InstanceID: "i-100", PublicIP: "203.0.113.10", PrivateIP: "10.0.0.10"},
				{NodeIndex: 1, InstanceID: "i-101", PublicIP: "203.0.113.11", PrivateIP: "10.0.0.11"},
				{NodeIndex: 2, InstanceID: "i-102", PublicIP: "203.0.113.12", PrivateIP: "10.0.0.12"},
				{NodeIndex: 3, InstanceID: "i-103", PublicIP: "203.0.113.13", PrivateIP: "10.0.0.13"},
			},
		},
	}
	require.NoError(t, newStateStore(stateDir).save(state))
	runner := &fakeRunner{outputFn: func(spec commandSpec) (string, error) {
		joined := strings.Join(spec.args, " ")
		switch {
		case strings.Contains(joined, "sts get-caller-identity"):
			return `{}`, nil
		case strings.Contains(joined, "describe-instances"):
			return "running\n", nil
		case spec.name == "ssh" && strings.Contains(joined, "systemctl is-active"):
			return "active\n", nil
		case spec.name == "ssh" && strings.Contains(joined, "26660/metrics"):
			return "tendermint_internal_autobahn_data_next_block{stage=\"execute\"} 43\n", nil
		default:
			return "", nil
		}
	}}
	var stdout bytes.Buffer
	app := &application{runner: runner, stdout: &stdout, stderr: &bytes.Buffer{}, stateDir: stateDir}

	require.NoError(t, app.list(context.Background(), listOptions{name: state.Name}))
	for nodeIndex := range 4 {
		require.Contains(t, stdout.String(), "node-"+fmt.Sprint(nodeIndex))
		require.Contains(t, stdout.String(), "i-10"+fmt.Sprint(nodeIndex))
		require.Contains(t, stdout.String(), "203.0.113.1"+fmt.Sprint(nodeIndex))
	}
	require.Equal(t, 4, strings.Count(stdout.String(), "active"))
	require.Equal(t, 4, strings.Count(stdout.String(), "42"))
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

func TestAWSTeardownStopsAndTerminatesNativeInstances(t *testing.T) {
	stateDir := t.TempDir()
	keyPath := filepath.Join(stateDir, "managed.pem")
	require.NoError(t, os.WriteFile(keyPath, []byte("key"), 0o600))
	state := clusterState{
		Version: stateVersion,
		Name:    "native-teardown",
		Target:  targetAWS,
		Status:  "ready",
		Nodes:   nativeClusterNodes(4),
		AWS: &awsState{
			Region:          "us-west-2",
			SSHUser:         "ubuntu",
			SSHKeyPath:      keyPath,
			SecurityGroupID: "sg-native",
			KeyName:         "key-native",
			ManagedKey:      true,
			Instances: []awsInstanceState{
				{NodeIndex: 0, InstanceID: "i-100", PublicIP: "203.0.113.10"},
				{NodeIndex: 1, InstanceID: "i-101", PublicIP: "203.0.113.11"},
				{NodeIndex: 2, InstanceID: "i-102", PublicIP: "203.0.113.12"},
				{NodeIndex: 3, InstanceID: "i-103", PublicIP: "203.0.113.13"},
			},
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
	commands := joinedCommands(runner.commands)
	require.Equal(t, 4, strings.Count(commands, "systemctl stop seid.service"))
	require.Contains(t, commands, "terminate-instances --instance-ids i-100 i-101 i-102 i-103")
	require.Contains(t, commands, "instance-terminated --instance-ids i-100 i-101 i-102 i-103")
	require.NotContains(t, commands, "docker-cluster-stop")
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

func TestWriteUserDataInstallsNativeBuildDependencies(t *testing.T) {
	path, err := writeUserData(t.TempDir(), "test")
	require.NoError(t, err)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(data), "build-essential python3")
	require.Contains(t, string(data), "go1.25.6")
	require.Contains(t, string(data), "/var/lib/autobahn-e2e-ready")
	require.NotContains(t, string(data), "docker")
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
