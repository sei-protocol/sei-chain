package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

type listOptions struct {
	name string
	json bool
}

type nodeReport struct {
	Cluster    string `json:"cluster"`
	Target     string `json:"target"`
	Node       string `json:"node"`
	Status     string `json:"status"`
	Height     string `json:"height"`
	EVMTarget  string `json:"evm_target"`
	InstanceID string `json:"instance_id,omitempty"`
	PublicIP   string `json:"public_ip,omitempty"`
}

const autobahnNextExecutedBlockMetric = "tendermint_internal_autobahn_data_next_block"

func (a *application) newListCommand() *cobra.Command {
	options := listOptions{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List clusters and live node information",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.list(cmd.Context(), options)
		},
	}
	cmd.Flags().StringVar(&options.name, "name", "", "show only this cluster")
	cmd.Flags().BoolVar(&options.json, "json", false, "print machine-readable JSON")
	return cmd
}

func (a *application) list(ctx context.Context, options listOptions) error {
	var states []clusterState
	if options.name != "" {
		state, err := a.store().load(options.name)
		if err != nil {
			return err
		}
		states = []clusterState{state}
	} else {
		var err error
		states, err = a.store().loadAll()
		if err != nil {
			return err
		}
	}
	if len(states) == 0 {
		_, _ = fmt.Fprintln(a.stdout, "No Autobahn E2E clusters found.")
		return nil
	}

	reports := make([]nodeReport, 0, len(states)*dockerClusterSize)
	for _, state := range states {
		clusterReports, err := a.inspectCluster(ctx, state)
		if err != nil {
			return fmt.Errorf("inspect cluster %s: %w", state.Name, err)
		}
		reports = append(reports, clusterReports...)
	}
	if options.json {
		encoder := json.NewEncoder(a.stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(reports)
	}
	writer := tabwriter.NewWriter(a.stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, "CLUSTER\tTARGET\tNODE\tSTATUS\tHEIGHT\tEVM TARGET\tPUBLIC IP\tINSTANCE")
	for _, report := range reports {
		_, _ = fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			report.Cluster,
			report.Target,
			report.Node,
			report.Status,
			report.Height,
			report.EVMTarget,
			report.PublicIP,
			report.InstanceID,
		)
	}
	return writer.Flush()
}

func (a *application) inspectCluster(ctx context.Context, state clusterState) ([]nodeReport, error) {
	switch state.Target {
	case targetLocal:
		return a.inspectLocalCluster(ctx, state), nil
	case targetAWS:
		return a.inspectAWSCluster(ctx, state)
	default:
		return nil, fmt.Errorf("unsupported target %q", state.Target)
	}
}

func (a *application) inspectLocalCluster(ctx context.Context, state clusterState) []nodeReport {
	reports := make([]nodeReport, len(state.Nodes))
	for i, node := range state.Nodes {
		status, err := a.runner.output(ctx, commandSpec{
			name: "docker",
			args: []string{"inspect", "--format", "{{.State.Status}}", node.Container},
		})
		if err != nil {
			status = "missing"
		}
		status = strings.TrimSpace(status)
		height := "-"
		if status == "running" {
			metrics, metricsErr := a.runner.output(ctx, commandSpec{
				name: "docker",
				args: []string{"exec", node.Container, "curl", "-fsS", "http://127.0.0.1:26660/metrics"},
			})
			if metricsErr == nil {
				height = parseAutobahnExecutedHeight(metrics)
			}
		}
		reports[i] = nodeReport{
			Cluster:   state.Name,
			Target:    state.Target,
			Node:      node.Name,
			Status:    status,
			Height:    height,
			EVMTarget: fmt.Sprintf("127.0.0.1:%d", node.EVMHostPort),
		}
	}
	return reports
}

func (a *application) inspectAWSCluster(ctx context.Context, state clusterState) ([]nodeReport, error) {
	if state.AWS == nil {
		return nil, fmt.Errorf("aws metadata is missing")
	}
	if state.AWS.InstanceID == "" {
		reports := make([]nodeReport, len(state.Nodes))
		for i, node := range state.Nodes {
			reports[i] = nodeReport{
				Cluster:   state.Name,
				Target:    state.Target,
				Node:      node.Name,
				Status:    state.Status,
				Height:    "-",
				EVMTarget: fmt.Sprintf("SSH→127.0.0.1:%d", node.EVMHostPort),
			}
		}
		return reports, nil
	}
	client := awsClient{runner: a.runner, region: state.AWS.Region, profile: state.AWS.Profile}
	if err := a.ensureAWSCredentials(ctx, client); err != nil {
		return nil, err
	}
	instanceStatus, err := client.output(ctx,
		"ec2", "describe-instances",
		"--instance-ids", state.AWS.InstanceID,
		"--query", "Reservations[0].Instances[0].State.Name",
		"--output", "text",
	)
	if err != nil {
		return nil, err
	}
	instanceStatus = strings.TrimSpace(instanceStatus)
	reports := make([]nodeReport, len(state.Nodes))
	for i, node := range state.Nodes {
		status := instanceStatus
		height := "-"
		if instanceStatus == "running" && state.AWS.PublicIP != "" {
			value, inspectErr := a.runner.output(ctx, sshCommand(state,
				"docker inspect --format '{{.State.Status}}' "+shellQuote(node.Container)))
			if inspectErr == nil {
				status = strings.TrimSpace(value)
			}
			value, heightErr := a.runner.output(ctx, sshCommand(state,
				"docker exec "+shellQuote(node.Container)+" curl -fsS http://127.0.0.1:26660/metrics"))
			if heightErr == nil {
				height = parseAutobahnExecutedHeight(value)
			}
		}
		reports[i] = nodeReport{
			Cluster:    state.Name,
			Target:     state.Target,
			Node:       node.Name,
			Status:     status,
			Height:     height,
			EVMTarget:  fmt.Sprintf("SSH→127.0.0.1:%d", node.EVMHostPort),
			InstanceID: state.AWS.InstanceID,
			PublicIP:   state.AWS.PublicIP,
		}
	}
	return reports, nil
}

func parseAutobahnExecutedHeight(metrics string) string {
	scanner := bufio.NewScanner(strings.NewReader(metrics))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, autobahnNextExecutedBlockMetric+"{") || !strings.Contains(line, `stage="execute"`) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return "-"
		}
		next, err := strconv.ParseFloat(fields[1], 64)
		if err != nil || next < 1 {
			return "-"
		}
		return strconv.FormatInt(int64(next)-1, 10)
	}
	return "-"
}
