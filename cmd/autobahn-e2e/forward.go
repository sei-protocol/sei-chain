package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"

	"github.com/spf13/cobra"
)

type forwardOptions struct {
	name      string
	node      string
	bind      string
	localPort int
}

func (a *application) newForwardCommand() *cobra.Command {
	options := forwardOptions{}
	cmd := &cobra.Command{
		Use:   "forward",
		Short: "Forward one node's EVM JSON-RPC port to localhost",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.forward(cmd.Context(), options)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&options.name, "name", defaultClusterName, "cluster name")
	flags.StringVar(&options.node, "node", "node-0", "node name or zero-based index")
	flags.StringVar(&options.bind, "bind", "127.0.0.1", "local bind address")
	flags.IntVar(&options.localPort, "local-port", 8545, "local listening port")
	return cmd
}

func (a *application) forward(ctx context.Context, options forwardOptions) error {
	if options.localPort < 1 || options.localPort > 65535 {
		return fmt.Errorf("--local-port must be between 1 and 65535")
	}
	state, err := a.store().load(options.name)
	if err != nil {
		return err
	}
	node, err := findNode(state.Nodes, options.node)
	if err != nil {
		return err
	}
	localAddress := net.JoinHostPort(options.bind, strconv.Itoa(options.localPort))
	switch state.Target {
	case targetLocal:
		remoteAddress := net.JoinHostPort("127.0.0.1", strconv.Itoa(node.EVMHostPort))
		if localAddress == remoteAddress {
			_, _ = fmt.Fprintf(a.stdout, "%s is already published by Docker for %s.\n", localAddress, node.Name)
			return nil
		}
		_, _ = fmt.Fprintf(a.stdout, "Forwarding %s to %s (%s). Press Ctrl-C to stop.\n", localAddress, remoteAddress, node.Name)
		return runTCPForward(ctx, localAddress, remoteAddress)
	case targetAWS:
		if state.AWS == nil {
			return fmt.Errorf("aws metadata is missing")
		}
		_, _ = fmt.Fprintf(a.stdout, "Forwarding %s to %s:8545 through %s. Press Ctrl-C to stop.\n", localAddress, node.Name, state.AWS.PublicIP)
		baseArgs := sshBaseArgs(state)
		destination := baseArgs[len(baseArgs)-1]
		args := append(baseArgs[:len(baseArgs)-1],
			"-o", "ExitOnForwardFailure=yes",
			"-N",
			"-L", fmt.Sprintf("%s:127.0.0.1:%d", localAddress, node.EVMHostPort),
			destination,
		)
		return a.runner.stream(ctx, commandSpec{name: commandSSH, args: args})
	default:
		return fmt.Errorf("unsupported target %q", state.Target)
	}
}

func runTCPForward(ctx context.Context, localAddress, remoteAddress string) error {
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", localAddress)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", localAddress, err)
	}
	defer func() { _ = listener.Close() }()
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	for {
		local, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("accept forwarded connection: %w", err)
		}
		go relayConnection(ctx, local, remoteAddress)
	}
}

func relayConnection(ctx context.Context, local net.Conn, remoteAddress string) {
	remote, err := (&net.Dialer{}).DialContext(ctx, "tcp", remoteAddress)
	if err != nil {
		_ = local.Close()
		return
	}
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(remote, local)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(local, remote)
		done <- struct{}{}
	}()
	<-done
	_ = local.Close()
	_ = remote.Close()
}
