package main

import (
	"bufio"
	"fmt"
	"os"

	"github.com/Layr-Labs/eigenda/common/pprof"
	"github.com/Layr-Labs/eigensdk-go/logging"
	"github.com/urfave/cli/v2"
)

// TODO (cody.littley): convert all commands to use flags stored in these variables
var (
	srcFlag = &cli.StringSliceFlag{
		Name:     "src",
		Aliases:  []string{"s"},
		Usage:    "Source paths where the DB data is found, at least one is required.",
		Required: true,
	}
	forceFlag = &cli.BoolFlag{
		Name:    "force",
		Aliases: []string{"f"},
		Usage:   "Force the operation without prompting for confirmation.",
	}
	knownHostsFileFlag = &cli.StringFlag{
		Name:     "known-hosts",
		Aliases:  []string{"k"},
		Usage:    "Path to a file containing known hosts for SSH connections.",
		Required: false,
		Value:    "~/.ssh/known_hosts",
	}
)

// buildCliParser creates a command line parser for the LittDB CLI tool.
func buildCLIParser(logger logging.Logger) *cli.App {
	app := &cli.App{
		Name:  "litt",
		Usage: "LittDB command line interface",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "debug",
				Aliases: []string{"d"},
				Usage:   "Enable debug mode. Program will pause for a debugger to attach.",
			},
			&cli.BoolFlag{
				Name:    "pprof",
				Aliases: []string{"p"},
				Usage:   "Starts a pprof server for profiling.",
			},
			&cli.IntFlag{
				Name:    "pprof-port",
				Aliases: []string{"P"},
				Usage:   "Port for the pprof server.",
				Value:   6060,
			},
		},
		Before: buildBeforeAction(logger),
		Commands: []*cli.Command{
			{
				Name:      "ls",
				Usage:     "List tables in a LittDB instance",
				ArgsUsage: "--src <path1> ... --src <pathN>",
				Flags: []cli.Flag{
					&cli.StringSliceFlag{
						Name:     "src",
						Aliases:  []string{"s"},
						Usage:    "Source paths where the DB data is found, at least one is required.",
						Required: true,
					},
				},
				Action: lsCommand,
			},
			{
				Name: "table-info",
				Usage: "Get information about a LittDB table. " +
					"If the DB is spread across multiple paths, all paths must be provided.",
				ArgsUsage: "--src <path1> ... --src <pathN> <table-name>",
				Args:      true,
				Flags: []cli.Flag{
					&cli.StringSliceFlag{
						Name:     "src",
						Aliases:  []string{"s"},
						Usage:    "Source paths where the DB data is found, at least one is required.",
						Required: true,
					},
				},
				Action: tableInfoCommand,
			},
			{
				Name:  "rebase",
				Usage: "Restructure LittDB file system layout.",
				ArgsUsage: "--src <source-path1> ... --src <source-pathN> " +
					"--dest <destination-path1> ... --dest <destination-pathN> [--preserve] [--quiet]",
				Flags: []cli.Flag{
					&cli.StringSliceFlag{
						Name:     "src",
						Aliases:  []string{"s"},
						Usage:    "Source paths where the data is found, at least one is required.",
						Required: true,
					},
					&cli.StringSliceFlag{
						Name:     "dst",
						Aliases:  []string{"d"},
						Usage:    "Destination paths for the rebased LittDB, at least one is required.",
						Required: true,
					},
					&cli.BoolFlag{
						Name:    "preserve",
						Aliases: []string{"p"},
						Usage:   "If enabled, then the old files are not removed.",
					},
					&cli.BoolFlag{
						Name:    "quiet",
						Aliases: []string{"q"},
						Usage:   "Reduces the verbosity of the output.",
					},
				},
				Action: rebaseCommand,
			},
			{
				Name:      "benchmark",
				Usage:     "Run a LittDB benchmark.",
				ArgsUsage: "<path/to/benchmark/config.json>",
				Args:      true,
				Action:    benchmarkCommand,
			},
			{
				Name:  "prune",
				Usage: "Delete data from a LittDB database/snapshot.",
				ArgsUsage: "--src <path1> ... --src <pathN> --max-age <durationInSeconds> " +
					"[--table <table1> ... --table <tableN>]",
				Flags: []cli.Flag{
					&cli.StringSliceFlag{
						Name:     "src",
						Aliases:  []string{"s"},
						Usage:    "Source paths where the DB data is found, at least one is required.",
						Required: true,
					},
					&cli.StringSliceFlag{
						Name:    "table",
						Aliases: []string{"t"},
						Usage:   "Prune this table. If not specified, all tables will be pruned.",
					},
					&cli.Uint64Flag{
						Name:    "max-age",
						Aliases: []string{"a"},
						Usage: "Maximum age of segments to keep, in seconds. " +
							"Segments older than this will be deleted.",
						Required: true,
					},
				},
				Action: pruneCommand,
			},
			{
				Name:  "push",
				Usage: "Push data to a remote location using ssh and rsync.",
				ArgsUsage: "--src <source-path1> ... --src <source-pathN> " +
					"--dst <remote-path1> ... --dst <remote-pathN> " +
					"[-i path/to/key] [-p port] [--no-gc] [--quiet] [--threads <threadCount>] " +
					"[--throttle <maxMBPerSecond>] <user>@<host>",
				Args: true,
				Flags: []cli.Flag{
					&cli.StringSliceFlag{
						Name:     "src",
						Aliases:  []string{"s"},
						Usage:    "Source paths where the data is found, at least one is required.",
						Required: true,
					},
					&cli.StringSliceFlag{
						Name:     "dst",
						Aliases:  []string{"d"},
						Usage:    "Remote destination paths, at least one is required.",
						Required: true,
					},
					&cli.Uint64Flag{
						Name:    "port",
						Aliases: []string{"p"},
						Usage:   "SSH port to connect to the remote host.",
						Value:   22,
					},
					knownHostsFileFlag,
					&cli.StringFlag{
						Name:    "key",
						Aliases: []string{"i"},
						Usage:   "Path to the SSH private key file for authentication.",
						Value:   "~/.ssh/id_rsa",
					},
					&cli.BoolFlag{
						Name:    "no-gc",
						Aliases: []string{"n"},
						Usage:   "If true, do not delete files pushed to the remote host.",
					},
					&cli.BoolFlag{
						Name:    "quiet",
						Aliases: []string{"q"},
						Usage:   "Reduces the verbosity of the output.",
					},
					&cli.Uint64Flag{
						Name:    "threads",
						Aliases: []string{"t"},
						Usage:   "Number of parallel rsync operations.",
						Value:   8,
					},
					&cli.Float64Flag{
						Name:    "throttle",
						Aliases: []string{"T"},
						Usage:   "Max network utilization, in mb/s",
						Value:   0,
					},
				},
				Action: pushCommand,
			},
			{ // TODO (cody.littley) test in preprod
				Name: "sync",
				Usage: "Periodically run 'litt push' to keep a remote backup in sync with local data. " +
					"Optionally calls 'litt prune' remotely to manage data retention.",
				ArgsUsage: "--src <source-path1> ... --src <source-pathN> " +
					"--dst <remote-path1> ... --dst <remote-pathN> " +
					"[-i <pathToKey>] [-p <port>] [--no-gc] [--quiet] [--threads <threadCount>] " +
					"[--throttle <maxMBPerSecond>] [--max-age <maxAgeInSeconds>] [--litt-binary " +
					"</path/to/remote/bin/litt]> [--period <howOftenToPushInSeconds>]" +
					"<user>@<host>",
				Flags: []cli.Flag{
					&cli.StringSliceFlag{
						Name:     "src",
						Aliases:  []string{"s"},
						Usage:    "Source paths where the data is found, at least one is required.",
						Required: true,
					},
					&cli.StringSliceFlag{
						Name:     "dst",
						Aliases:  []string{"d"},
						Usage:    "Remote destination paths, at least one is required.",
						Required: true,
					},
					&cli.Uint64Flag{
						Name:    "port",
						Aliases: []string{"p"},
						Usage:   "SSH port to connect to the remote host.",
						Value:   22,
					},
					&cli.StringFlag{
						Name:    "key",
						Aliases: []string{"i"},
						Usage:   "Path to the SSH private key file for authentication.",
						Value:   "~/.ssh/id_rsa",
					},
					knownHostsFileFlag,
					&cli.BoolFlag{
						Name:    "no-gc",
						Aliases: []string{"n"},
						Usage:   "If true, do not delete files pushed to the remote host.",
					},
					&cli.BoolFlag{
						Name:    "quiet",
						Aliases: []string{"q"},
						Usage:   "Reduces the verbosity of the output.",
					},
					&cli.Uint64Flag{
						Name:    "threads",
						Aliases: []string{"t"},
						Usage:   "Number of parallel rsync operations.",
						Value:   8,
					},
					&cli.Float64Flag{
						Name:    "throttle",
						Aliases: []string{"T"},
						Usage:   "Max network utilization, in mb/s",
						Value:   0,
					},
					&cli.Uint64Flag{
						Name:    "max-age",
						Aliases: []string{"a"},
						Usage: "If non-zero, remotely run 'litt prune' to delete segments " +
							"older than this age in seconds.",
						Value: 0, // Default to 0, meaning no age limit
					},
					&cli.StringFlag{
						Name:    "litt-binary",
						Aliases: []string{"b"},
						Usage:   "The remote location of the 'litt' CLI binary to use for pruning.",
						Value:   "litt",
					},
					&cli.Uint64Flag{
						Name:    "period",
						Aliases: []string{"P"},
						Usage:   "The period in seconds between sync operations.",
						Value:   300,
					},
				},
				Action: syncCommand,
			},
			{
				Name:      "unlock",
				Usage:     "Manually delete LittDB lock files. Dangerous if used improperly, use with caution.",
				ArgsUsage: "--src <path1> ... --src <pathN> [--force]",
				Flags: []cli.Flag{
					srcFlag,
					forceFlag,
				},
				Action: unlockCommand,
			},
		},
	}
	return app
}

// Builds a function that is called before any command is executed.
func buildBeforeAction(logger logging.Logger) func(*cli.Context) error {
	return func(ctx *cli.Context) error {
		handleDebugMode(ctx, logger)

		err := handlePProfMode(ctx, logger)
		if err != nil {
			return fmt.Errorf("failed to start pprof: %w", err)
		}

		return nil
	}
}

// If debug mode is enabled, this function will block until the user presses Enter.
func handleDebugMode(ctx *cli.Context, logger logging.Logger) {
	debugModeEnabled := ctx.Bool("debug")
	if !debugModeEnabled {
		return
	}

	pid := os.Getpid()
	logger.Infof("Waiting for debugger to attach (pid: %d).\n", pid)

	logger.Infof("Press Enter to continue...")
	reader := bufio.NewReader(os.Stdin)
	_, _ = reader.ReadString('\n') // block until newline is read
}

// If pprof is enabled, this function starts the pprof server.
func handlePProfMode(ctx *cli.Context, logger logging.Logger) error {
	pprofEnabled := ctx.Bool("pprof")
	if !pprofEnabled {
		return nil
	}

	pprofPort := ctx.Int("pprof-port")
	if pprofPort <= 0 || pprofPort > 65535 {
		return fmt.Errorf("invalid pprof port: %d", pprofPort)
	}

	logger.Infof("pprof enabled on port %d", pprofPort)
	profiler := pprof.NewPprofProfiler(fmt.Sprintf("%d", pprofPort), logger)
	go profiler.Start()

	return nil
}
