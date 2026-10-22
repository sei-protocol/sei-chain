package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type resetOptions struct {
	name    string
	timeout time.Duration
	nodeEnv []string
}

func (a *application) newResetCommand() *cobra.Command {
	options := resetOptions{}
	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Start a distributed AWS cluster's chain again from genesis on the instances and binaries it already has",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.reset(cmd.Context(), options)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&options.name, "name", defaultClusterName, "cluster name")
	flags.DurationVar(&options.timeout, "timeout", 20*time.Minute, "readiness timeout")
	flags.StringArrayVar(&options.nodeEnv, "node-env", nil, "KEY=VALUE replacing or adding to the cluster's node environment; repeatable")
	return cmd
}

// reset stops every validator, deletes its chain data, and runs init, genesis and start again with
// the seid each validator already built.
func (a *application) reset(ctx context.Context, options resetOptions) error {
	state, err := a.store().load(options.name)
	if err != nil {
		return err
	}
	if state.Target != targetAWS || state.AWS == nil || state.AWS.colocated() {
		return fmt.Errorf("reset supports distributed AWS clusters only")
	}
	overrides, err := parseNodeEnv(options.nodeEnv)
	if err != nil {
		return err
	}
	if len(overrides) > 0 && state.AWS.NodeEnv == nil {
		state.AWS.NodeEnv = map[string]string{}
	}
	for key, value := range overrides {
		state.AWS.NodeEnv[key] = value
	}
	if err := a.store().save(state); err != nil {
		return err
	}
	validators := state.AWS.validators()
	if len(validators) != awsValidatorCount {
		return fmt.Errorf("reset: expected %d validators, got %d", awsValidatorCount, len(validators))
	}
	readyCtx, cancel := context.WithTimeout(ctx, options.timeout)
	defer cancel()

	_, _ = fmt.Fprintln(a.stdout, "Stopping every validator and deleting its chain data.")
	generated := filepath.Join(state.AWS.RemoteDir, "build/generated")
	if err := a.forEachHost(readyCtx, validators, func(ctx context.Context, host awsHost) error {
		command := strings.Join([]string{
			remoteMake(state, "docker-aws-validator-stop", nil),
			"sudo rm -rf \"$HOME/.sei-autobahn-e2e-home\" " + shellQuote(generated),
		}, " && ")
		if err := a.remoteStream(ctx, state, host, command); err != nil {
			return fmt.Errorf("stop validator %d: %w", host.Index, err)
		}
		return a.remoteStream(ctx, state, host, remoteMake(state, "docker-aws-validator-init", map[string]string{
			"ID":           strconv.Itoa(host.Index),
			"ADVERTISE_IP": host.PrivateIP,
			"SKIP_BUILD":   "true",
		}))
	}); err != nil {
		return err
	}
	if err := a.collectValidatorArtifacts(readyCtx, state, validators); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(a.stdout, "Generating genesis on validator 0.")
	if err := a.remoteStream(readyCtx, state, validators[0], remoteMake(state, "docker-aws-validator-genesis", nil)); err != nil {
		return fmt.Errorf("generate genesis: %w", err)
	}
	if err := a.distributeGenesis(readyCtx, state, validators); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(a.stdout, "Starting every validator.")
	if err := a.forEachHost(readyCtx, validators, func(ctx context.Context, host awsHost) error {
		return a.remoteStream(ctx, state, host, remoteMake(state, "docker-aws-validator-start", map[string]string{
			"ID":           strconv.Itoa(host.Index),
			"ADVERTISE_IP": host.PrivateIP,
		}))
	}); err != nil {
		return fmt.Errorf("start validators: %w", err)
	}
	if err := a.waitForRemoteCluster(readyCtx, state); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(a.stdout, "Cluster %s restarted from genesis.\n", state.Name)
	return nil
}
