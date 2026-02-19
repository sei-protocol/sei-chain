package wal

import (
	"crypto/rand"
	"fmt"
	"testing"
	"time"

	"github.com/tidwall/wal"

	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
)

func makePayload(size int) []byte {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return buf
}

func BenchmarkTidwallWALWrite(b *testing.B) {
	entrySizes := []int{64, 128, 1024, 4096, 16384, 65536}
	fsyncModes := []struct {
		name   string
		noSync bool
	}{
		{"fsync", false},
		{"no-fsync", true},
	}

	for _, es := range entrySizes {
		for _, fm := range fsyncModes {
			name := fmt.Sprintf("entry=%dB/%s", es, fm.name)
			noSync := fm.noSync
			payload := makePayload(es)

			b.Run(name, func(b *testing.B) {
				dir := b.TempDir()
				log, err := wal.Open(dir, &wal.Options{
					NoSync: noSync,
					NoCopy: true,
				})
				if err != nil {
					b.Fatal(err)
				}
				b.Cleanup(func() { _ = log.Close() })

				b.ResetTimer()
				start := time.Now()

				for i := 0; i < b.N; i++ {
					if err := log.Write(uint64(i+1), payload); err != nil {
						b.Fatal(err)
					}
				}

				elapsed := time.Since(start)
				totalBytes := float64(b.N) * float64(es)

				b.ReportMetric(totalBytes/elapsed.Seconds(), "bytes/s")
				b.ReportMetric(elapsed.Seconds()/float64(b.N)*1e6, "us/write")
			})
		}
	}
}

func BenchmarkWALWrapperWrite(b *testing.B) {
	entrySizes := []int{64, 128, 1024, 4096, 16384, 65536}
	writeModes := []struct {
		name       string
		bufferSize int
	}{
		{"buffer-0", 0},
		{"buffer-256", 256},
	}

	marshal := func(entry []byte) ([]byte, error) { return entry, nil }
	unmarshal := func(data []byte) ([]byte, error) { return data, nil }

	for _, es := range entrySizes {
		for _, wm := range writeModes {
			name := fmt.Sprintf("entry=%dB/%s", es, wm.name)
			bufSize := wm.bufferSize
			payload := makePayload(es)

			b.Run(name, func(b *testing.B) {
				dir := b.TempDir()
				w, err := NewWAL(marshal, unmarshal, logger.NewNopLogger(), dir, Config{
					WriteBufferSize: bufSize,
				})
				if err != nil {
					b.Fatal(err)
				}

				b.ResetTimer()
				start := time.Now()

				for i := 0; i < b.N; i++ {
					if err := w.Write(payload); err != nil {
						b.Fatal(err)
					}
				}

				if err := w.Close(); err != nil {
					b.Fatal(err)
				}
				b.StopTimer()

				elapsed := time.Since(start)
				totalBytes := float64(b.N) * float64(es)

				b.ReportMetric(totalBytes/elapsed.Seconds(), "bytes/s")
				b.ReportMetric(elapsed.Seconds()/float64(b.N)*1e6, "us/write")
			})
		}
	}
}
