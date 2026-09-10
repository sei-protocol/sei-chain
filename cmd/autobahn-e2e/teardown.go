package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

type teardownOptions struct {
	name string
}

func (a *application) newTeardownCommand() *cobra.Command {
	options := teardownOptions{}
	cmd := &cobra.Command{
		Use:   "teardown",
		Short: "Stop a cluster and remove its managed resources",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.teardown(cmd.Context(), options)
		},
	}
	cmd.Flags().StringVar(&options.name, "name", defaultClusterName, "cluster name")
	return cmd
}

func (a *application) teardown(ctx context.Context, options teardownOptions) error {
	state, err := a.store().load(options.name)
	if err != nil {
		return err
	}
	switch state.Target {
	case targetLocal:
		if err := a.runner.stream(ctx, commandSpec{dir: state.RepoRoot, name: "make", args: []string{"docker-cluster-stop"}}); err != nil {
			return err
		}
	case targetAWS:
		if err := a.teardownAWS(ctx, state); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported target %q", state.Target)
	}
	if err := a.store().remove(state.Name); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(a.stdout, "Cluster %s was removed.\n", state.Name)
	return nil
}

func (a *application) teardownAWS(ctx context.Context, state clusterState) error {
	if state.AWS == nil {
		return fmt.Errorf("aws metadata is missing")
	}
	client := awsClient{runner: a.runner, region: state.AWS.Region, profile: state.AWS.Profile}
	if err := a.ensureAWSCredentials(ctx, client); err != nil {
		return err
	}
	if len(state.AWS.Instances) == 0 && state.AWS.PublicIP != "" && state.AWS.RemoteDir != "" {
		command := "cd " + shellQuote(state.AWS.RemoteDir) + " && make docker-cluster-stop"
		instance := awsInstanceState{InstanceID: state.AWS.InstanceID, PublicIP: state.AWS.PublicIP}
		if err := a.runner.stream(ctx, sshCommandForInstance(state, instance, command)); err != nil {
			_, _ = fmt.Fprintf(a.stderr, "warning: remote Docker teardown failed: %v\n", err)
		}
	}
	var errs []error
	instanceIDs := make([]string, 0, len(state.AWS.Instances)+1)
	for _, instance := range state.AWS.Instances {
		if instance.PublicIP != "" {
			if err := a.runner.stream(ctx, sshCommandForInstance(state, instance, "sudo systemctl stop seid.service")); err != nil {
				_, _ = fmt.Fprintf(a.stderr, "warning: stop native validator node-%d failed: %v\n", instance.NodeIndex, err)
			}
		}
		if instance.InstanceID != "" {
			instanceIDs = append(instanceIDs, instance.InstanceID)
		}
	}
	if len(instanceIDs) == 0 && state.AWS.InstanceID != "" {
		instanceIDs = append(instanceIDs, state.AWS.InstanceID)
	}
	if len(instanceIDs) > 0 {
		terminateArgs := append([]string{"ec2", "terminate-instances", "--instance-ids"}, instanceIDs...)
		if _, err := client.output(ctx, terminateArgs...); err != nil {
			errs = append(errs, err)
		} else {
			waitArgs := append([]string{"ec2", "wait", "instance-terminated", "--instance-ids"}, instanceIDs...)
			if err := client.stream(ctx, waitArgs...); err != nil {
				errs = append(errs, err)
			}
		}
	}
	if state.AWS.SecurityGroupID != "" {
		if _, err := client.output(ctx, "ec2", "delete-security-group", "--group-id", state.AWS.SecurityGroupID); err != nil && !awsResourceMissing(err) {
			errs = append(errs, err)
		}
	}
	if state.AWS.ManagedKey && state.AWS.KeyName != "" {
		if _, err := client.output(ctx, "ec2", "delete-key-pair", "--key-name", state.AWS.KeyName); err != nil && !awsResourceMissing(err) {
			errs = append(errs, err)
		}
		if err := os.Remove(state.AWS.SSHKeyPath); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("remove managed SSH key: %w", err))
		}
	}
	if len(errs) > 0 {
		messages := make([]string, len(errs))
		for i, err := range errs {
			messages[i] = err.Error()
		}
		return errors.New(strings.Join(messages, "; "))
	}
	return nil
}

func awsResourceMissing(err error) bool {
	return strings.Contains(err.Error(), "NotFound")
}
