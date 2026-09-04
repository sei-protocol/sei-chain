package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const dockerClusterSize = 4

type deployOptions struct {
	name         string
	target       string
	timeout      time.Duration
	region       string
	profile      string
	instanceType string
	amiID        string
	subnetID     string
	sshCIDR      string
	sshUser      string
	keyName      string
	sshKeyPath   string
	volumeSize   int
	repoURL      string
	ref          string
}

func (a *application) newDeployCommand() *cobra.Command {
	options := deployOptions{}
	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy the four-node Autobahn EVM-only cluster",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.deploy(cmd.Context(), options)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&options.name, "name", defaultClusterName, "cluster name")
	flags.StringVar(&options.target, "target", targetLocal, "deployment target: local or aws")
	flags.DurationVar(&options.timeout, "timeout", 20*time.Minute, "deployment readiness timeout")
	flags.StringVar(&options.region, "region", "us-west-2", "AWS region")
	flags.StringVar(&options.profile, "profile", "", "AWS CLI profile")
	flags.StringVar(&options.instanceType, "instance-type", "c7g.2xlarge", "EC2 instance type")
	flags.StringVar(&options.amiID, "ami-id", "", "EC2 AMI ID; defaults to Ubuntu 24.04 ARM64")
	flags.StringVar(&options.subnetID, "subnet-id", "", "EC2 subnet; defaults to a default VPC subnet")
	flags.StringVar(&options.sshCIDR, "ssh-cidr", "", "CIDR allowed to SSH; defaults to the caller's public IP")
	flags.StringVar(&options.sshUser, "ssh-user", "ubuntu", "EC2 SSH user")
	flags.StringVar(&options.keyName, "key-name", "", "existing EC2 key pair name; omitted creates a managed key")
	flags.StringVar(&options.sshKeyPath, "ssh-key", "", "private key for --key-name")
	flags.IntVar(&options.volumeSize, "volume-size", 100, "EC2 root volume size in GiB")
	flags.StringVar(&options.repoURL, "repo-url", "", "Git repository cloned on EC2; defaults to origin")
	flags.StringVar(&options.ref, "ref", "", "Git ref deployed on EC2; defaults to the current commit")
	return cmd
}

func (a *application) deploy(ctx context.Context, options deployOptions) error {
	if err := validateClusterName(options.name); err != nil {
		return err
	}
	if options.timeout <= 0 {
		return fmt.Errorf("--timeout must be positive")
	}
	exists, err := a.store().exists(options.name)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("cluster %q already exists; tear it down first", options.name)
	}

	switch options.target {
	case targetLocal:
		return a.deployLocal(ctx, options)
	case targetAWS:
		return a.deployAWS(ctx, options)
	default:
		return fmt.Errorf("unsupported deployment target %q; use local or aws", options.target)
	}
}

func (a *application) deployLocal(ctx context.Context, options deployOptions) error {
	for _, name := range []string{commandDocker, commandMake} {
		if err := a.runner.lookPath(name); err != nil {
			return err
		}
	}
	repoRoot, err := findRepoRoot()
	if err != nil {
		return err
	}
	existing, err := a.runner.output(ctx, commandSpec{
		name: commandDocker,
		args: []string{"ps", "-a", "--filter", "name=sei-node-", "--format", "{{.Names}}"},
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(existing) != "" {
		return fmt.Errorf("sei-node containers already exist; tear down the existing Docker cluster first")
	}

	state := clusterState{
		Version:   stateVersion,
		Name:      options.name,
		Target:    targetLocal,
		Status:    "provisioning",
		CreatedAt: time.Now().UTC(),
		RepoRoot:  repoRoot,
		Nodes:     clusterNodes(dockerClusterSize),
	}
	if err := a.store().save(state); err != nil {
		return err
	}
	fail := func(cause error) error {
		state.Status = "failed"
		if err := a.store().save(state); err != nil {
			return fmt.Errorf("%w; save failure state: %v", cause, err)
		}
		return cause
	}

	if err := a.runner.stream(ctx, commandSpec{
		dir: repoRoot,
		env: []string{
			"AUTOBAHN=true",
			"AUTOBAHN_EVMONLY_IN_MEMORY=true",
			"DOCKER_DETACH=true",
		},
		name: commandMake,
		args: []string{"docker-cluster-start"},
	}); err != nil {
		return fail(err)
	}
	readyCtx, cancel := context.WithTimeout(ctx, options.timeout)
	defer cancel()
	if err := waitForLaunchFile(readyCtx, filepath.Join(repoRoot, "build", "generated", "launch.complete"), dockerClusterSize); err != nil {
		return fail(err)
	}
	state.Status = "ready"
	if err := a.store().save(state); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(a.stdout, "Cluster %s is ready with %d nodes.\n", state.Name, len(state.Nodes))
	return nil
}

func waitForLaunchFile(ctx context.Context, path string, count int) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		file, err := os.Open(path) //nolint:gosec // path is the fixed launch marker under the resolved repository root.
		if err == nil {
			scanner := bufio.NewScanner(file)
			lines := 0
			for scanner.Scan() {
				lines++
			}
			closeErr := file.Close()
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("read cluster launch status: %w", err)
			}
			if closeErr != nil {
				return fmt.Errorf("close cluster launch status: %w", closeErr)
			}
			if lines >= count {
				return nil
			}
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("open cluster launch status: %w", err)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for %d nodes: %w", count, ctx.Err())
		case <-ticker.C:
		}
	}
}
