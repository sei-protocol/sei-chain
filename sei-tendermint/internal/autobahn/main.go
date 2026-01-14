package stream

import (
	"context"
	"fmt"
	_ "net/http/pprof" // necessary for profiling
	"path/filepath"
	"time"

	_ "github.com/grafana/pyroscope-go/godeltaprof/http/pprof" //  necessary for continuous profiling
	cli2 "github.com/urfave/cli/v3"

	"github.com/sei-protocol/sei-stream/config"
	"github.com/sei-protocol/sei-stream/pkg/logger"
	"github.com/sei-protocol/sei-stream/pkg/metrics"
	"github.com/sei-protocol/sei-stream/pkg/opt"
	"github.com/sei-protocol/sei-stream/pkg/service"
	"github.com/sei-protocol/sei-stream/pkg/trace"
	"github.com/sei-protocol/sei-stream/pkg/version"
	"github.com/tendermint/tendermint/internal/autobahn/types"
)

// AppName defines the application name.
const AppName = "stream"

// ServiceName defines the service name.
const ServiceName = "sei_" + AppName

// Main is the entrypoint of the stream application.
func Main(ctx context.Context, command *cli2.Command) error {
	if command.IsSet("pprof-cpu-file") {
		pprofCPUFile := filepath.Clean(command.String("pprof-cpu-file"))
		stop, err := opt.StartCPUPProf(pprofCPUFile)
		if err != nil {
			return err
		}
		defer stop()
	}

	cfgFile := filepath.Clean(command.String("config"))

	cfg, err := config.ReadConfigJSON(cfgFile)
	if err != nil {
		return err
	}

	streamCfg := cfg.StreamConfig

	log := logger.Get(ctx)
	log.Info().
		Any("version", version.NewInfo()).
		Any("config", streamCfg).
		Str("config_file", cfgFile).
		Msg("starting")

	trace.StartTracer(ctx, trace.Config{
		ServiceName:  fmt.Sprintf("%s_%s", ServiceName, streamCfg.Environment),
		ExporterType: streamCfg.TraceExporter,
		Endpoint:     streamCfg.TraceEndpoint,
		TraceRatio:   streamCfg.TraceSampleRatio,
	})

	reg := metrics.NewRegistry(ServiceName)
	return service.Run(ctx, func(ctx context.Context, s service.Scope) error {
		s.SpawnNamed("metrics.RunServer()", func() error {
			return metrics.RunServer(ctx, streamCfg.GetMetricsEndpoint(), reg)
		})
		if addr, ok := streamCfg.ProfilingEndpoint.Get(); ok {
			s.SpawnNamed("RunProfilingServer()", func() error {
				return opt.RunProfilingServer(ctx, addr)
			})
		}
		viewTimeout := streamCfg.GetViewTimeout()
		viewTimeoutF := func(view types.View) time.Duration { return viewTimeout }
		key := types.TestSecretKey(streamCfg.Server)
		return Run(ctx, cfg, key, viewTimeoutF, reg)
	})
}
