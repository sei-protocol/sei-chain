package metrics

import (
	"context"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/shirou/gopsutil/v3/process"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/sys/unix"
)

// MonitoredDir describes a directory whose on-disk size should be tracked.
type MonitoredDir struct {
	// Name is the metric-name component, e.g. "data_dir" produces
	// "{prefix}_data_dir_size_bytes".
	Name string
	// Path is the filesystem path to monitor.
	Path string
	// TrackAvailableSpace also emits "{prefix}_{Name}_available_bytes".
	TrackAvailableSpace bool
}

// StartSystemMetrics creates OTel instruments for system-level metrics and
// spawns background goroutines that poll them periodically. All goroutines
// exit when ctx is cancelled. If intervalSeconds <= 0 the call is a no-op.
//
// Metrics created (where {p} = prefix, {d} = dir.Name):
//
//	{p}_{d}_size_bytes            for each dir
//	{p}_{d}_available_bytes       for dirs with TrackAvailableSpace
//	{p}_uptime_seconds
//	{p}_process_read_bytes_total  (Linux only)
//	{p}_process_write_bytes_total (Linux only)
//	{p}_process_read_count_total  (Linux only)
//	{p}_process_write_count_total (Linux only)
func StartSystemMetrics(ctx context.Context, prefix string, intervalSeconds int, dirs []MonitoredDir) {
	if intervalSeconds <= 0 {
		return
	}
	meter := otel.Meter(prefix)

	for _, d := range dirs {
		dir := d
		if dir.Path == "" {
			continue
		}

		sizeGauge, _ := meter.Int64Gauge(
			fmt.Sprintf("%s_%s_size_bytes", prefix, dir.Name),
			metric.WithDescription(fmt.Sprintf("Approximate size in bytes of the %s directory", dir.Name)),
			metric.WithUnit("By"),
		)
		startPeriodicSampling(ctx, intervalSeconds, func() {
			sizeGauge.Record(context.Background(), measureDirSize(dir.Path))
		})

		if dir.TrackAvailableSpace {
			availGauge, _ := meter.Int64Gauge(
				fmt.Sprintf("%s_%s_available_bytes", prefix, dir.Name),
				metric.WithDescription(fmt.Sprintf(
					"Available disk space in bytes on the filesystem containing the %s directory", dir.Name)),
				metric.WithUnit("By"),
			)
			startPeriodicSampling(ctx, intervalSeconds, func() {
				availGauge.Record(context.Background(), measureAvailableBytes(dir.Path))
			})
		}
	}

	uptimeGauge, _ := meter.Float64Gauge(
		fmt.Sprintf("%s_uptime_seconds", prefix),
		metric.WithDescription("Seconds since benchmark started. Resets to 0 on restart."),
		metric.WithUnit("s"),
	)
	startUptimeSampling(ctx, uptimeGauge)

	startProcessIOSampling(ctx, meter, prefix, intervalSeconds)
}

// startPeriodicSampling runs sampleFn immediately and then every intervalSeconds
// in a background goroutine. The goroutine exits when ctx is cancelled.
func startPeriodicSampling(ctx context.Context, intervalSeconds int, sampleFn func()) {
	if intervalSeconds <= 0 || sampleFn == nil {
		return
	}
	interval := time.Duration(intervalSeconds) * time.Second
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		sampleFn()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sampleFn()
			}
		}
	}()
}

// startUptimeSampling records elapsed seconds since now into gauge once per second.
func startUptimeSampling(ctx context.Context, gauge metric.Float64Gauge) {
	if gauge == nil {
		return
	}
	startTime := time.Now()
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		bg := context.Background()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				gauge.Record(bg, time.Since(startTime).Seconds())
			}
		}
	}()
}

// startProcessIOSampling tracks process-level I/O counters via gopsutil.
// Skipped on darwin where gopsutil does not implement IOCounters.
func startProcessIOSampling(ctx context.Context, meter metric.Meter, prefix string, intervalSeconds int) {
	if intervalSeconds <= 0 || runtime.GOOS == "darwin" {
		return
	}
	pid := os.Getpid()
	if pid < 0 || pid > math.MaxInt32 {
		return
	}
	proc, err := process.NewProcess(int32(pid)) //nolint:gosec
	if err != nil {
		return
	}

	readBytes, _ := meter.Int64Counter(
		fmt.Sprintf("%s_process_read_bytes_total", prefix),
		metric.WithDescription("Bytes read from storage by benchmark. Use rate() for throughput. Linux only."),
		metric.WithUnit("By"),
	)
	writeBytes, _ := meter.Int64Counter(
		fmt.Sprintf("%s_process_write_bytes_total", prefix),
		metric.WithDescription("Bytes written to storage by benchmark. Use rate() for throughput. Linux only."),
		metric.WithUnit("By"),
	)
	readCount, _ := meter.Int64Counter(
		fmt.Sprintf("%s_process_read_count_total", prefix),
		metric.WithDescription("Read I/O ops by benchmark. Use rate() for read IOPS. Linux only."),
		metric.WithUnit("{count}"),
	)
	writeCount, _ := meter.Int64Counter(
		fmt.Sprintf("%s_process_write_count_total", prefix),
		metric.WithDescription("Write I/O ops by benchmark. Use rate() for write IOPS. Linux only."),
		metric.WithUnit("{count}"),
	)

	interval := time.Duration(intervalSeconds) * time.Second
	var prevRB, prevWB, prevRC, prevWC uint64
	var initialized bool
	bg := context.Background()
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		sample := func() {
			io, ioErr := proc.IOCounters()
			if ioErr != nil || io == nil {
				return
			}
			if initialized {
				if io.ReadBytes >= prevRB {
					readBytes.Add(bg, uint64ToInt64Clamped(io.ReadBytes-prevRB))
				}
				if io.WriteBytes >= prevWB {
					writeBytes.Add(bg, uint64ToInt64Clamped(io.WriteBytes-prevWB))
				}
				if io.ReadCount >= prevRC {
					readCount.Add(bg, uint64ToInt64Clamped(io.ReadCount-prevRC))
				}
				if io.WriteCount >= prevWC {
					writeCount.Add(bg, uint64ToInt64Clamped(io.WriteCount-prevWC))
				}
			}
			prevRB, prevWB = io.ReadBytes, io.WriteBytes
			prevRC, prevWC = io.ReadCount, io.WriteCount
			initialized = true
		}
		sample()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sample()
			}
		}
	}()
}

// measureDirSize walks dir and returns the sum of all regular file sizes.
func measureDirSize(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return nil
		}
		total += info.Size()
		return nil
	})
	return total
}

// measureAvailableBytes returns the available bytes on the filesystem containing dir.
func measureAvailableBytes(dir string) int64 {
	var stat unix.Statfs_t
	if err := unix.Statfs(dir, &stat); err != nil {
		return 0
	}
	result := stat.Bavail * uint64(stat.Bsize) //nolint:gosec
	if result > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(result)
}
