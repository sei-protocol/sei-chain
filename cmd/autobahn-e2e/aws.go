package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	ubuntuARM64AMIParameter = "/aws/service/canonical/ubuntu/server/24.04/stable/current/arm64/hvm/ebs-gp3/ami-id"
	ubuntuAMD64AMIParameter = "/aws/service/canonical/ubuntu/server/24.04/stable/current/amd64/hvm/ebs-gp3/ami-id"
)

var sshUserPattern = regexp.MustCompile(`^[a-z_][a-z0-9_-]*$`)

type awsClient struct {
	runner  commandRunner
	region  string
	profile string
}

func (c awsClient) output(ctx context.Context, args ...string) (string, error) {
	return c.runner.output(ctx, commandSpec{name: "aws", args: args, env: c.environment()})
}

func (c awsClient) stream(ctx context.Context, args ...string) error {
	return c.runner.stream(ctx, commandSpec{name: "aws", args: args, env: c.environment()})
}

func (c awsClient) environment() []string {
	env := []string{"AWS_REGION=" + c.region, "AWS_DEFAULT_REGION=" + c.region}
	if c.profile != "" {
		env = append(env, "AWS_PROFILE="+c.profile)
	}
	return env
}

func (a *application) deployAWS(ctx context.Context, options deployOptions) error {
	if options.architecture == "" {
		options.architecture = "arm64"
	}
	if options.goGC == "" {
		options.goGC = "200"
	}
	if options.volumeSize < 20 {
		return fmt.Errorf("--volume-size must be at least 20 GiB")
	}
	if options.keyName != "" && options.sshKeyPath == "" {
		return fmt.Errorf("--ssh-key is required with --key-name")
	}
	if !sshUserPattern.MatchString(options.sshUser) {
		return fmt.Errorf("invalid --ssh-user %q", options.sshUser)
	}
	amiParameter, err := ubuntuAMIParameter(options.architecture)
	if err != nil {
		return err
	}
	if options.goMaxProcs < 0 {
		return fmt.Errorf("--gomaxprocs cannot be negative")
	}
	if err := validateGoGC(options.goGC); err != nil {
		return err
	}
	for _, name := range []string{"aws", "git", "scp", "ssh"} {
		if err := a.runner.lookPath(name); err != nil {
			return err
		}
	}
	client := awsClient{runner: a.runner, region: options.region, profile: options.profile}
	if err := a.ensureAWSCredentials(ctx, client); err != nil {
		return err
	}
	repoURL, ref, err := a.resolveRemoteSource(ctx, options)
	if err != nil {
		return err
	}
	amiID := options.amiID
	if amiID == "" {
		amiID, err = client.output(ctx,
			"ssm", "get-parameter",
			"--name", amiParameter,
			"--query", "Parameter.Value",
			"--output", "text",
		)
		if err != nil {
			return fmt.Errorf("resolve Ubuntu AMI: %w", err)
		}
		amiID = strings.TrimSpace(amiID)
	}
	vpcID, err := resolveVPC(ctx, client, options.subnetID)
	if err != nil {
		return err
	}
	sshCIDR, err := resolveSSHCIDR(ctx, options.sshCIDR)
	if err != nil {
		return err
	}

	state := clusterState{
		Version:   stateVersion,
		Name:      options.name,
		Target:    targetAWS,
		Status:    "provisioning",
		CreatedAt: time.Now().UTC(),
		Nodes:     nativeClusterNodes(awsClusterSize),
		AWS: &awsState{
			Region:       options.region,
			Profile:      options.profile,
			SSHUser:      options.sshUser,
			RemoteDir:    filepath.Join("/home", options.sshUser, "sei-chain-"+options.name),
			RepoURL:      repoURL,
			Ref:          ref,
			GoMaxProcs:   options.goMaxProcs,
			GoGC:         options.goGC,
			Architecture: options.architecture,
		},
	}
	fail := func(cause error) error {
		state.Status = "failed"
		if err := a.store().save(state); err != nil {
			return fmt.Errorf("%w; save failure state: %v", cause, err)
		}
		return cause
	}

	securityGroupName := fmt.Sprintf("sei-autobahn-e2e-%s-%d", options.name, time.Now().Unix())
	securityGroupID, err := client.output(ctx,
		"ec2", "create-security-group",
		"--group-name", securityGroupName,
		"--description", "SSH access for Sei Autobahn EVM-only E2E",
		"--vpc-id", vpcID,
		"--query", "GroupId",
		"--output", "text",
	)
	if err != nil {
		return err
	}
	state.AWS.SecurityGroupID = strings.TrimSpace(securityGroupID)
	if err := a.store().save(state); err != nil {
		return err
	}
	if _, err := client.output(ctx,
		"ec2", "create-tags",
		"--resources", state.AWS.SecurityGroupID,
		"--tags", "Key=sei-autobahn-e2e-cluster,Value="+options.name,
	); err != nil {
		return fail(err)
	}
	if _, err := client.output(ctx,
		"ec2", "authorize-security-group-ingress",
		"--group-id", state.AWS.SecurityGroupID,
		"--ip-permissions", fmt.Sprintf("IpProtocol=-1,UserIdGroupPairs=[{GroupId=%s}]", state.AWS.SecurityGroupID),
	); err != nil {
		return fail(err)
	}
	if _, err := client.output(ctx,
		"ec2", "authorize-security-group-ingress",
		"--group-id", state.AWS.SecurityGroupID,
		"--protocol", "tcp",
		"--port", "22",
		cidrFlag(sshCIDR), sshCIDR,
	); err != nil {
		return fail(err)
	}

	if options.keyName == "" {
		if err := os.MkdirAll(a.stateDir, 0o700); err != nil {
			return fail(fmt.Errorf("create state directory: %w", err))
		}
		state.AWS.KeyName = fmt.Sprintf("sei-autobahn-e2e-%s-%d", options.name, time.Now().Unix())
		state.AWS.SSHKeyPath = filepath.Join(a.stateDir, options.name+".pem")
		state.AWS.ManagedKey = true
		keyMaterial, err := client.output(ctx,
			"ec2", "create-key-pair",
			"--key-name", state.AWS.KeyName,
			"--key-type", "ed25519",
			"--query", "KeyMaterial",
			"--output", "text",
		)
		if err != nil {
			return fail(err)
		}
		if err := os.WriteFile(state.AWS.SSHKeyPath, []byte(keyMaterial), 0o600); err != nil {
			return fail(fmt.Errorf("write managed SSH key: %w", err))
		}
	} else {
		state.AWS.KeyName = options.keyName
		state.AWS.SSHKeyPath = expandHome(options.sshKeyPath)
		if _, err := os.Stat(state.AWS.SSHKeyPath); err != nil {
			return fail(fmt.Errorf("inspect SSH key: %w", err))
		}
	}
	if err := a.store().save(state); err != nil {
		return err
	}

	userDataPath, err := writeUserData(a.stateDir, options.name)
	if err != nil {
		return fail(err)
	}
	defer func() { _ = os.Remove(userDataPath) }()
	runArgs := []string{
		"ec2", "run-instances",
		"--image-id", amiID,
		"--instance-type", options.instanceType,
		"--key-name", state.AWS.KeyName,
		"--security-group-ids", state.AWS.SecurityGroupID,
		"--associate-public-ip-address",
		"--metadata-options", "HttpTokens=required,HttpEndpoint=enabled",
		"--block-device-mappings", fmt.Sprintf("DeviceName=/dev/sda1,Ebs={VolumeSize=%d,VolumeType=gp3,DeleteOnTermination=true}", options.volumeSize),
		"--user-data", "file://" + userDataPath,
		"--count", fmt.Sprint(awsClusterSize),
		"--tag-specifications", fmt.Sprintf("ResourceType=instance,Tags=[{Key=Name,Value=sei-autobahn-e2e-%s},{Key=sei-autobahn-e2e-cluster,Value=%s}]", options.name, options.name),
		"--query", "Instances[].InstanceId",
		"--output", "text",
	}
	if options.subnetID != "" {
		runArgs = append(runArgs, "--subnet-id", options.subnetID)
	}
	instanceIDsOutput, err := client.output(ctx, runArgs...)
	if err != nil {
		return fail(err)
	}
	instanceIDs := strings.Fields(instanceIDsOutput)
	if len(instanceIDs) != awsClusterSize {
		return fail(fmt.Errorf("AWS returned %d instances, expected %d", len(instanceIDs), awsClusterSize))
	}
	state.AWS.Instances = make([]awsInstanceState, len(instanceIDs))
	for nodeIndex, instanceID := range instanceIDs {
		state.AWS.Instances[nodeIndex] = awsInstanceState{NodeIndex: nodeIndex, InstanceID: instanceID}
		if _, err := client.output(ctx,
			"ec2", "create-tags",
			"--resources", instanceID,
			"--tags", fmt.Sprintf("Key=Name,Value=sei-autobahn-e2e-%s-node-%d", options.name, nodeIndex),
		); err != nil {
			return fail(err)
		}
	}
	if err := a.store().save(state); err != nil {
		return err
	}
	waitArgs := append([]string{"ec2", "wait", "instance-running", "--instance-ids"}, instanceIDs...)
	if err := client.stream(ctx, waitArgs...); err != nil {
		return fail(err)
	}
	waitArgs = append([]string{"ec2", "wait", "instance-status-ok", "--instance-ids"}, instanceIDs...)
	if err := client.stream(ctx, waitArgs...); err != nil {
		return fail(err)
	}
	instances, err := describeAWSInstances(ctx, client, instanceIDs)
	if err != nil {
		return fail(err)
	}
	for nodeIndex, instanceID := range instanceIDs {
		instance, ok := instances[instanceID]
		if !ok {
			return fail(fmt.Errorf("describe-instances omitted %s", instanceID))
		}
		if instance.PublicIP == "" {
			return fail(fmt.Errorf("ec2 instance %s has no public IP; choose a subnet that assigns public addresses", instanceID))
		}
		if instance.PrivateIP == "" {
			return fail(fmt.Errorf("ec2 instance %s has no private IP", instanceID))
		}
		instance.NodeIndex = nodeIndex
		state.AWS.Instances[nodeIndex] = instance
	}
	if err := a.store().save(state); err != nil {
		return err
	}

	readyCtx, cancel := context.WithTimeout(ctx, options.timeout)
	defer cancel()
	if err := a.waitForEC2Bootstrap(readyCtx, state); err != nil {
		return fail(err)
	}
	if err := a.startRemoteCluster(readyCtx, state); err != nil {
		return fail(err)
	}
	if err := a.waitForRemoteCluster(readyCtx, state); err != nil {
		return fail(err)
	}
	state.Status = "ready"
	if err := a.store().save(state); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(a.stdout, "Cluster %s is ready on %d native EC2 instances.\n", state.Name, len(state.AWS.Instances))
	return nil
}

func ubuntuAMIParameter(architecture string) (string, error) {
	switch architecture {
	case "arm64":
		return ubuntuARM64AMIParameter, nil
	case "amd64":
		return ubuntuAMD64AMIParameter, nil
	default:
		return "", fmt.Errorf("unsupported --architecture %q; use arm64 or amd64", architecture)
	}
}

func validateGoGC(value string) error {
	if value == "off" {
		return nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return fmt.Errorf("--gogc must be a non-negative integer or off")
	}
	return nil
}

func describeAWSInstances(ctx context.Context, client awsClient, instanceIDs []string) (map[string]awsInstanceState, error) {
	args := append([]string{"ec2", "describe-instances", "--instance-ids"}, instanceIDs...)
	args = append(args,
		"--query", "Reservations[].Instances[].{InstanceID:InstanceId,PublicIP:PublicIpAddress,PrivateIP:PrivateIpAddress}",
		"--output", "json",
	)
	value, err := client.output(ctx, args...)
	if err != nil {
		return nil, err
	}
	var instances []struct {
		InstanceID string  `json:"InstanceID"`
		PublicIP   *string `json:"PublicIP"`
		PrivateIP  *string `json:"PrivateIP"`
	}
	if err := json.Unmarshal([]byte(value), &instances); err != nil {
		return nil, fmt.Errorf("decode EC2 instance addresses: %w", err)
	}
	result := make(map[string]awsInstanceState, len(instances))
	for _, instance := range instances {
		result[instance.InstanceID] = awsInstanceState{
			InstanceID: instance.InstanceID,
			PublicIP:   optionalString(instance.PublicIP),
			PrivateIP:  optionalString(instance.PrivateIP),
		}
	}
	return result, nil
}

func optionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (a *application) ensureAWSCredentials(ctx context.Context, client awsClient) error {
	if _, err := client.output(ctx, "sts", "get-caller-identity", "--output", "json"); err == nil {
		return nil
	}
	stdinInfo, statErr := os.Stdin.Stat()
	if statErr != nil || stdinInfo.Mode()&os.ModeCharDevice == 0 {
		return fmt.Errorf("aws credentials are unavailable; configure AWS_PROFILE or run aws configure")
	}
	_, _ = fmt.Fprintln(a.stderr, "AWS credentials are unavailable; starting interactive aws configure.")
	args := []string{"configure"}
	if client.profile != "" {
		args = append(args, "--profile", client.profile)
	}
	if err := client.stream(ctx, args...); err != nil {
		return err
	}
	if _, err := client.output(ctx, "sts", "get-caller-identity", "--output", "json"); err != nil {
		return fmt.Errorf("validate configured AWS credentials: %w", err)
	}
	return nil
}

func (a *application) resolveRemoteSource(ctx context.Context, options deployOptions) (string, string, error) {
	repoURL := options.repoURL
	ref := options.ref
	root := ""
	var err error
	if repoURL == "" || ref == "" {
		root, err = findRepoRoot()
		if err != nil {
			return "", "", err
		}
	}
	if repoURL == "" {
		repoURL, err = a.runner.output(ctx, commandSpec{dir: root, name: "git", args: []string{"remote", "get-url", "origin"}})
		if err != nil {
			return "", "", err
		}
		repoURL = strings.TrimSpace(repoURL)
	}
	if strings.HasPrefix(repoURL, "git@github.com:") {
		repoURL = "https://github.com/" + strings.TrimPrefix(repoURL, "git@github.com:")
	}
	if ref == "" {
		ref, err = a.runner.output(ctx, commandSpec{dir: root, name: "git", args: []string{"rev-parse", "HEAD"}})
		if err != nil {
			return "", "", err
		}
		ref = strings.TrimSpace(ref)
	}
	return repoURL, ref, nil
}

func resolveVPC(ctx context.Context, client awsClient, subnetID string) (string, error) {
	args := []string{
		"ec2", "describe-vpcs",
		"--filters", "Name=is-default,Values=true",
		"--query", "Vpcs[0].VpcId",
		"--output", "text",
	}
	if subnetID != "" {
		args = []string{
			"ec2", "describe-subnets",
			"--subnet-ids", subnetID,
			"--query", "Subnets[0].VpcId",
			"--output", "text",
		}
	}
	value, err := client.output(ctx, args...)
	if err != nil {
		return "", err
	}
	vpcID := strings.TrimSpace(value)
	if vpcID == "" || vpcID == "None" {
		return "", fmt.Errorf("no VPC found; pass --subnet-id")
	}
	return vpcID, nil
}

func resolveSSHCIDR(ctx context.Context, configured string) (string, error) {
	if configured != "" {
		if _, _, err := net.ParseCIDR(configured); err != nil {
			return "", fmt.Errorf("invalid --ssh-cidr: %w", err)
		}
		return configured, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://checkip.amazonaws.com", nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("discover caller IP; pass --ssh-cidr explicitly: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("discover caller IP: HTTP %s; pass --ssh-cidr explicitly", response.Status)
	}
	value, err := io.ReadAll(io.LimitReader(response.Body, 128))
	if err != nil {
		return "", fmt.Errorf("read caller IP: %w", err)
	}
	ip := net.ParseIP(strings.TrimSpace(string(value)))
	if ip == nil {
		return "", fmt.Errorf("discover caller IP: invalid response %q", strings.TrimSpace(string(value)))
	}
	if ip.To4() != nil {
		return ip.String() + "/32", nil
	}
	return ip.String() + "/128", nil
}

func cidrFlag(cidr string) string {
	_, network, err := net.ParseCIDR(cidr)
	if err == nil && network.IP.To4() == nil {
		return "--cidr-ipv6"
	}
	return "--cidr"
}

func writeUserData(dir, clusterName string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create state directory: %w", err)
	}
	file, err := os.CreateTemp(dir, clusterName+"-user-data-*.sh")
	if err != nil {
		return "", fmt.Errorf("create EC2 user data: %w", err)
	}
	path := file.Name()
	defer func() { _ = file.Close() }()
	script := `#!/usr/bin/env bash
set -euxo pipefail
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y ca-certificates curl git jq make build-essential python3
case "$(uname -m)" in
  aarch64|arm64) go_arch=arm64 ;;
  x86_64|amd64) go_arch=amd64 ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac
curl -fsSL "https://go.dev/dl/go1.25.6.linux-${go_arch}.tar.gz" -o /tmp/go.tgz
rm -rf /usr/local/go
tar -C /usr/local -xzf /tmp/go.tgz
ln -sf /usr/local/go/bin/go /usr/local/bin/go
touch /var/lib/autobahn-e2e-ready
`
	if _, err := file.WriteString(script); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("write EC2 user data: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("close EC2 user data: %w", err)
	}
	return path, nil
}

func (a *application) waitForEC2Bootstrap(ctx context.Context, state clusterState) error {
	for _, instance := range state.AWS.Instances {
		if err := a.waitForRemoteCommand(ctx, state, instance, "test -f /var/lib/autobahn-e2e-ready"); err != nil {
			return fmt.Errorf("wait for EC2 bootstrap on node-%d: %w", instance.NodeIndex, err)
		}
	}
	return nil
}

func (a *application) startRemoteCluster(ctx context.Context, state clusterState) error {
	aws := state.AWS
	for _, instance := range aws.Instances {
		command := strings.Join([]string{
			"if test ! -d " + shellQuote(filepath.Join(aws.RemoteDir, ".git")) + "; then git clone --filter=blob:none " + shellQuote(aws.RepoURL) + " " + shellQuote(aws.RemoteDir) + "; fi",
			"cd " + shellQuote(aws.RemoteDir),
			"git fetch --depth=1 origin " + shellQuote(aws.Ref),
			"git checkout --detach FETCH_HEAD",
			"mkdir -p build",
			"rm -f build/autobahn-native-build.ready",
			"nohup integration_test/autobahn/scripts/build_native_node.sh " + shellQuote(aws.RemoteDir) + " </dev/null >build/autobahn-native-build.log 2>&1 &",
		}, " && ")
		if err := a.runner.stream(ctx, sshCommandForInstance(state, instance, command)); err != nil {
			return fmt.Errorf("start native build on node-%d: %w", instance.NodeIndex, err)
		}
	}
	for _, instance := range aws.Instances {
		if err := a.waitForRemoteCommand(ctx, state, instance, "test -f "+shellQuote(filepath.Join(aws.RemoteDir, "build", "autobahn-native-build.ready"))); err != nil {
			return fmt.Errorf("wait for native build on node-%d: %w", instance.NodeIndex, err)
		}
	}

	privateIPs := make([]string, len(aws.Instances))
	for _, instance := range aws.Instances {
		privateIPs[instance.NodeIndex] = instance.PrivateIP
	}
	coordinator := aws.Instances[0]
	prepareArgs := []string{
		"integration_test/autobahn/scripts/prepare_native_cluster.sh",
		shellQuote(filepath.Join("/home", aws.SSHUser)),
	}
	for _, privateIP := range privateIPs {
		prepareArgs = append(prepareArgs, shellQuote(privateIP))
	}
	prepareCommand := "cd " + shellQuote(aws.RemoteDir) + " && " + strings.Join(prepareArgs, " ")
	if err := a.runner.stream(ctx, sshCommandForInstance(state, coordinator, prepareCommand)); err != nil {
		return fmt.Errorf("prepare native cluster configuration: %w", err)
	}

	stagingDir, err := os.MkdirTemp("", "autobahn-e2e-native-*")
	if err != nil {
		return fmt.Errorf("create native cluster staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(stagingDir) }()
	for _, instance := range aws.Instances {
		archiveName := fmt.Sprintf("node-%d.tgz", instance.NodeIndex)
		localArchive := filepath.Join(stagingDir, archiveName)
		remoteArchive := filepath.Join(aws.RemoteDir, "build", "autobahn-native", archiveName)
		if err := a.runner.stream(ctx, scpDownloadCommand(state, coordinator, remoteArchive, localArchive)); err != nil {
			return fmt.Errorf("download configuration for node-%d: %w", instance.NodeIndex, err)
		}
		targetArchive := filepath.Join("/tmp", state.Name+"-"+archiveName)
		if err := a.runner.stream(ctx, scpUploadCommand(state, instance, localArchive, targetArchive)); err != nil {
			return fmt.Errorf("upload configuration for node-%d: %w", instance.NodeIndex, err)
		}
		installCommand := strings.Join([]string{
			"cd " + shellQuote(aws.RemoteDir),
			"integration_test/autobahn/scripts/install_native_node.sh " + shellQuote(targetArchive) + " " + shellQuote(aws.SSHUser) + " " + fmt.Sprint(aws.GoMaxProcs) + " " + shellQuote(aws.GoGC),
		}, " && ")
		if err := a.runner.stream(ctx, sshCommandForInstance(state, instance, installCommand)); err != nil {
			return fmt.Errorf("install native validator node-%d: %w", instance.NodeIndex, err)
		}
	}
	for _, instance := range aws.Instances {
		if err := a.runner.stream(ctx, sshCommandForInstance(state, instance, "sudo systemctl start seid.service")); err != nil {
			return fmt.Errorf("start native validator node-%d: %w", instance.NodeIndex, err)
		}
	}
	return nil
}

func (a *application) waitForRemoteCluster(ctx context.Context, state clusterState) error {
	command := "systemctl is-active --quiet seid.service && curl -fsS -X POST -H 'content-type: application/json' --data '{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"eth_sendRawTransaction\",\"params\":[\"0x01\"]}' http://127.0.0.1:8545 >/dev/null"
	for _, instance := range state.AWS.Instances {
		if err := a.waitForRemoteCommand(ctx, state, instance, command); err != nil {
			return fmt.Errorf("wait for native validator node-%d: %w", instance.NodeIndex, err)
		}
	}
	return nil
}

func (a *application) waitForRemoteCommand(ctx context.Context, state clusterState, instance awsInstanceState, command string) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if _, err := a.runner.output(ctx, sshCommandForInstance(state, instance, command)); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func sshCommandForInstance(state clusterState, instance awsInstanceState, remoteCommand string) commandSpec {
	return commandSpec{name: "ssh", args: append(sshBaseArgs(state, instance), remoteCommand)}
}

func sshBaseArgs(state clusterState, instance awsInstanceState) []string {
	aws := state.AWS
	return []string{
		"-i", expandHome(aws.SSHKeyPath),
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=10",
		"-o", "StrictHostKeyChecking=accept-new",
		aws.SSHUser + "@" + instance.PublicIP,
	}
}

func scpDownloadCommand(state clusterState, instance awsInstanceState, remotePath, localPath string) commandSpec {
	args := scpBaseArgs(state)
	args = append(args, state.AWS.SSHUser+"@"+instance.PublicIP+":"+remotePath, localPath)
	return commandSpec{name: "scp", args: args}
}

func scpUploadCommand(state clusterState, instance awsInstanceState, localPath, remotePath string) commandSpec {
	args := scpBaseArgs(state)
	args = append(args, localPath, state.AWS.SSHUser+"@"+instance.PublicIP+":"+remotePath)
	return commandSpec{name: "scp", args: args}
}

func scpBaseArgs(state clusterState) []string {
	return []string{
		"-i", expandHome(state.AWS.SSHKeyPath),
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=10",
		"-o", "StrictHostKeyChecking=accept-new",
	}
}

func awsInstanceForNode(state clusterState, node node) (awsInstanceState, error) {
	if state.AWS == nil {
		return awsInstanceState{}, fmt.Errorf("aws metadata is missing")
	}
	for _, instance := range state.AWS.Instances {
		if instance.NodeIndex == node.Index {
			return instance, nil
		}
	}
	if state.AWS.InstanceID != "" {
		return awsInstanceState{
			NodeIndex:  node.Index,
			InstanceID: state.AWS.InstanceID,
			PublicIP:   state.AWS.PublicIP,
		}, nil
	}
	return awsInstanceState{}, fmt.Errorf("AWS instance for %s is missing", node.Name)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
