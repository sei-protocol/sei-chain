package cmd

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/admin"
	"github.com/sei-protocol/sei-chain/admin/types"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func LogLevelCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "log",
		Short: "Runtime log management",
	}
	cmd.AddCommand(logLevelSubCmd())
	return cmd
}

func logLevelSubCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "level",
		Short: "Runtime log level management",
		Long:  "Query and modify logger levels on a running seid node via the admin gRPC service.",
	}

	cmd.PersistentFlags().String("admin-addr", admin.DefaultAddress, "admin gRPC server address")
	cmd.AddCommand(
		logLevelSetCmd(),
		logLevelGetCmd(),
		logLevelListCmd(),
	)
	return cmd
}

func logLevelSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <pattern> <level>",
		Short: "Set log level for loggers matching a pattern",
		Long: `Set the log level for loggers matching the given pattern.
 
Pattern can be an exact logger name (e.g. "x/evm/state"), a glob
pattern (e.g. "x/evm/*"), or "*" to set all loggers.
 
Valid levels: debug, info, warn, error`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, conn, err := adminClient(cmd)
			if err != nil {
				return err
			}
			defer func() { _ = conn.Close() }()

			resp, err := client.SetLogLevel(cmd.Context(), &types.SetLogLevelRequest{
				Pattern: args[0],
				Level:   args[1],
			})
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Set %d logger(s) matching %q to %s\n",
				resp.Affected, resp.Pattern, resp.Level)
			return nil
		},
	}
}

func logLevelGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <logger>",
		Short: "Get the current log level for a logger",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, conn, err := adminClient(cmd)
			if err != nil {
				return err
			}
			defer func() { _ = conn.Close() }()

			resp, err := client.GetLogLevel(cmd.Context(), &types.GetLogLevelRequest{
				Logger: args[0],
			})
			if err != nil {
				return err
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", resp.Logger, resp.Level)
			return nil
		},
	}
}

func logLevelListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list [prefix]",
		Short: "List all registered loggers and their levels",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, conn, err := adminClient(cmd)
			if err != nil {
				return err
			}
			defer func() { _ = conn.Close() }()

			req := &types.ListLoggersRequest{}
			if len(args) > 0 {
				req.Prefix = args[0]
			}

			resp, err := client.ListLoggers(cmd.Context(), req)
			if err != nil {
				return err
			}

			for _, l := range resp.Loggers {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%-40s %s\n", l.Name, l.Level)
			}
			return nil
		},
	}
}

func adminClient(cmd *cobra.Command) (types.AdminServiceClient, *grpc.ClientConn, error) {
	addr, _ := cmd.Flags().GetString("admin-addr")
	conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to admin gRPC server at %s: %w", addr, err)
	}
	return types.NewAdminServiceClient(conn), conn, nil
}
