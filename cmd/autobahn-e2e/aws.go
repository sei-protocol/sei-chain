package main

import (
	"context"
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

const ubuntuARM64AMIParameter = "/aws/service/canonical/ubuntu/server/24.04/stable/current/arm64/hvm/ebs-gp3/ami-id"

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
	if options.volumeSize < 20 {
		return fmt.Errorf("--volume-size must be at least 20 GiB")
	}
	if options.keyName != "" && options.sshKeyPath == "" {
		return fmt.Errorf("--ssh-key is required with --key-name")
	}
	if !sshUserPattern.MatchString(options.sshUser) {
		return fmt.Errorf("invalid --ssh-user %q", options.sshUser)
	}
	for _, name := range []string{"aws", "git", "ssh"} {
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
			"--name", ubuntuARM64AMIParameter,
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
		Nodes:     clusterNodes(dockerClusterSize),
		AWS: &awsState{
			Region:    options.region,
			Profile:   options.profile,
			SSHUser:   options.sshUser,
			RemoteDir: filepath.Join("/home", options.sshUser, "sei-chain-"+options.name),
			RepoURL:   repoURL,
			Ref:       ref,
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

	userDataPath, err := writeUserData(a.stateDir, options.name, options.sshUser)
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
		"--tag-specifications", fmt.Sprintf("ResourceType=instance,Tags=[{Key=Name,Value=sei-autobahn-e2e-%s},{Key=sei-autobahn-e2e-cluster,Value=%s}]", options.name, options.name),
		"--query", "Instances[0].InstanceId",
		"--output", "text",
	}
	if options.subnetID != "" {
		runArgs = append(runArgs, "--subnet-id", options.subnetID)
	}
	instanceID, err := client.output(ctx, runArgs...)
	if err != nil {
		return fail(err)
	}
	state.AWS.InstanceID = strings.TrimSpace(instanceID)
	if err := a.store().save(state); err != nil {
		return err
	}
	if err := client.stream(ctx, "ec2", "wait", "instance-running", "--instance-ids", state.AWS.InstanceID); err != nil {
		return fail(err)
	}
	if err := client.stream(ctx, "ec2", "wait", "instance-status-ok", "--instance-ids", state.AWS.InstanceID); err != nil {
		return fail(err)
	}
	publicIP, err := client.output(ctx,
		"ec2", "describe-instances",
		"--instance-ids", state.AWS.InstanceID,
		"--query", "Reservations[0].Instances[0].PublicIpAddress",
		"--output", "text",
	)
	if err != nil {
		return fail(err)
	}
	state.AWS.PublicIP = strings.TrimSpace(publicIP)
	if state.AWS.PublicIP == "" || state.AWS.PublicIP == "None" {
		return fail(fmt.Errorf("ec2 instance has no public IP; choose a subnet that assigns public addresses"))
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
	_, _ = fmt.Fprintf(a.stdout, "Cluster %s is ready on EC2 instance %s (%s).\n", state.Name, state.AWS.InstanceID, state.AWS.PublicIP)
	return nil
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

func writeUserData(dir, clusterName, sshUser string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create state directory: %w", err)
	}
	file, err := os.CreateTemp(dir, clusterName+"-user-data-*.sh")
	if err != nil {
		return "", fmt.Errorf("create EC2 user data: %w", err)
	}
	path := file.Name()
	defer func() { _ = file.Close() }()
	script := fmt.Sprintf(`#!/usr/bin/env bash
set -euxo pipefail
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y ca-certificates curl git jq make build-essential docker.io docker-compose-v2
systemctl enable --now docker
usermod -aG docker %s
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
`, sshUser)
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
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		_, err := a.runner.output(ctx, sshCommand(state, "test -f /var/lib/autobahn-e2e-ready"))
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for EC2 bootstrap: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (a *application) startRemoteCluster(ctx context.Context, state clusterState) error {
	aws := state.AWS
	command := strings.Join([]string{
		"git clone --filter=blob:none " + shellQuote(aws.RepoURL) + " " + shellQuote(aws.RemoteDir),
		"cd " + shellQuote(aws.RemoteDir),
		"git checkout --detach " + shellQuote(aws.Ref),
		"AUTOBAHN=true AUTOBAHN_EVMONLY_IN_MEMORY=true DOCKER_DETACH=true make docker-cluster-start",
	}, " && ")
	if err := a.runner.stream(ctx, sshCommand(state, command)); err != nil {
		return fmt.Errorf("start remote cluster: %w", err)
	}
	return nil
}

func (a *application) waitForRemoteCluster(ctx context.Context, state clusterState) error {
	command := "test \"$(wc -l < " + shellQuote(filepath.Join(state.AWS.RemoteDir, "build/generated/launch.complete")) + ")\" -ge " + strconv.Itoa(dockerClusterSize)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if _, err := a.runner.output(ctx, sshCommand(state, command)); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for remote cluster: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func sshCommand(state clusterState, remoteCommand string) commandSpec {
	return commandSpec{name: "ssh", args: append(sshBaseArgs(state), remoteCommand)}
}

func sshBaseArgs(state clusterState) []string {
	aws := state.AWS
	return []string{
		"-i", expandHome(aws.SSHKeyPath),
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=10",
		"-o", "StrictHostKeyChecking=accept-new",
		aws.SSHUser + "@" + aws.PublicIP,
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
