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
		if err := a.runner.stream(ctx, commandSpec{dir: state.RepoRoot, name: commandMake, args: []string{"docker-cluster-stop"}}); err != nil {
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
	if state.AWS.PublicIP != "" && state.AWS.RemoteDir != "" {
		command := "cd " + shellQuote(state.AWS.RemoteDir) + " && make docker-cluster-stop"
		if err := a.runner.stream(ctx, sshCommand(state, command)); err != nil {
			_, _ = fmt.Fprintf(a.stderr, "warning: remote Docker teardown failed: %v\n", err)
		}
	}
	var errs []error
	if state.AWS.InstanceID != "" {
		if _, err := client.output(ctx, "ec2", "terminate-instances", "--instance-ids", state.AWS.InstanceID); err != nil {
			errs = append(errs, err)
		} else if err := client.stream(ctx, "ec2", "wait", "instance-terminated", "--instance-ids", state.AWS.InstanceID); err != nil {
			errs = append(errs, err)
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
