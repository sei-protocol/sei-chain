package conn

import (
	"bufio"
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	mrand "math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tmrand "github.com/tendermint/tendermint/libs/rand"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/require"
	"github.com/tendermint/tendermint/libs/utils/scope"
	"github.com/tendermint/tendermint/libs/utils/tcp"
)

// Run go test -update from within this module
// to update the golden test vector file
var update = flag.Bool("update", false, "update .golden files")

func TestSecretConnectionHandshake(t *testing.T) {
	_, _ = makeSecretConnPair(t)
}

func TestConcurrentReadWrite(t *testing.T) {
	ctx := t.Context()
	sc1, sc2 := makeSecretConnPair(t)
	rng := utils.TestRng()
	fooWriteText := utils.GenBytes(rng, dataSizeMax)
	n := 100 * dataSizeMax

	// read from two routines.
	// should be safe from race according to net.Conn:
	// https://golang.org/pkg/net/#Conn
	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error { return readLots(ctx, sc1, n) })
		s.Spawn(func() error { return readLots(ctx, sc1, n) })
		s.Spawn(func() error { return writeLots(ctx, sc2, fooWriteText, n) })
		s.Spawn(func() error { return writeLots(ctx, sc2, fooWriteText, n) })
		return nil
	})
	require.NoError(t, err)
}

func TestSecretConnectionReadWrite(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	data := utils.Slice(
		utils.GenBytes(rng, dataSizeMax*100),
		utils.GenBytes(rng, dataSizeMax*100),
	)
	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		c1, c2 := tcp.TestPipe()
		s.SpawnBg(func() error { return utils.IgnoreCancel(c1.Run(ctx)) })
		s.SpawnBg(func() error { return utils.IgnoreCancel(c2.Run(ctx)) })
		for id, c := range utils.Slice(c1, c2) {
			rng := rng.Split()
			s.Spawn(func() error {
				sc, err := MakeSecretConnection(ctx, c)
				if err != nil {
					return fmt.Errorf("MakeSecretConnection(): %w", err)
				}
				writeRng := rng.Split()
				s.Spawn(func() error {
					toWrite := data[id]
					for len(toWrite) > 0 {
						n := min(writeRng.Intn(dataSizeMax*5)+1, len(toWrite))
						if err := sc.Write(ctx, toWrite[:n]); err != nil {
							return fmt.Errorf("failed to write to nodeSecretConn: %w", err)
						}
						toWrite = toWrite[n:]
						if err := sc.Flush(ctx); err != nil {
							return fmt.Errorf("sc.Flush(): %w", err)
						}
					}
					return nil
				})
				readRng := rng.Split()
				s.Spawn(func() error {
					toRead := data[1-id]
					for len(toRead) > 0 {
						n := min(readRng.Intn(dataSizeMax*5)+1, len(toRead))
						buf := make([]byte, n)
						if err := sc.Read(ctx, buf); err != nil {
							return fmt.Errorf("failed to read from nodeSecretConn: %w", err)
						}
						if err := utils.TestDiff(buf, toRead[:n]); err != nil {
							return err
						}
						toRead = toRead[n:]
					}
					return nil
				})
				return nil
			})
		}
		return nil
	})
	require.NoError(t, err)
}

func TestDeriveSecretsAndChallengeGolden(t *testing.T) {
	goldenFilepath := filepath.Join("testdata", t.Name()+".golden")
	if *update {
		t.Logf("Updating golden test vector file %s", goldenFilepath)
		data := createGoldenTestVectors()
		require.NoError(t, os.WriteFile(goldenFilepath, []byte(data), 0644))
	}
	f, err := os.Open(goldenFilepath)
	require.NoError(t, err)
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		params := strings.Split(line, ",")
		dh, err := hex.DecodeString(params[0])
		require.NoError(t, err)
		locIsLeast, err := strconv.ParseBool(params[1])
		require.NoError(t, err)
		expectedRecvSecret, err := hex.DecodeString(params[2])
		require.NoError(t, err)
		expectedSendSecret, err := hex.DecodeString(params[3])
		require.NoError(t, err)

		aead := dhSecret(dh).AeadSecrets(locIsLeast)
		require.Equal(t, aeadSecret(expectedRecvSecret), aead.recv, "Recv Secrets aren't equal")
		require.Equal(t, aeadSecret(expectedSendSecret), aead.send, "Send Secrets aren't equal")
	}
}

func writeLots(ctx context.Context, sc *SecretConnection, data []byte, total int) error {
	for total > 0 {
		n := min(len(data), total)
		total -= n
		if err := sc.Write(ctx, data[:n]); err != nil {
			return err
		}
		if err := sc.Flush(ctx); err != nil {
			return err
		}
	}
	return nil
}

func readLots(ctx context.Context, sc *SecretConnection, total int) error {
	for total > 0 {
		readBuffer := make([]byte, min(dataSizeMax, total))
		err := sc.Read(ctx, readBuffer)
		if err != nil {
			return err
		}
		total -= len(readBuffer)
	}
	return nil
}

// Creates the data for a test vector file.
// The file format is:
// Hex(diffie_hellman_secret), loc_is_least, Hex(recvSecret), Hex(sendSecret), Hex(challenge)
func createGoldenTestVectors() string {
	data := ""
	for range 32 {
		dh := dhSecret(tmrand.Bytes(len(dhSecret{})))
		data += hex.EncodeToString(dh[:]) + ","
		locIsLeast := mrand.Int63()%2 == 0
		data += strconv.FormatBool(locIsLeast) + ","
		aead := dh.AeadSecrets(locIsLeast)
		data += hex.EncodeToString(aead.recv[:]) + ","
		data += hex.EncodeToString(aead.send[:]) + ","
	}
	return data
}

func spawnBgForTest(t testing.TB, task func(context.Context) error) {
	go func() {
		if err := task(t.Context()); t.Context().Err() == nil {
			utils.OrPanic(err)
		}
	}()
}

func makeSecretConnPair(tb testing.TB) (sc1 *SecretConnection, sc2 *SecretConnection) {
	ctx := tb.Context()
	c1, c2 := tcp.TestPipe()
	spawnBgForTest(tb, c1.Run)
	spawnBgForTest(tb, c2.Run)
	// Make connections from both sides in parallel.
	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error {
			var err error
			sc1, err = MakeSecretConnection(ctx, c1)
			return err
		})
		s.Spawn(func() error {
			var err error
			sc2, err = MakeSecretConnection(ctx, c2)
			return err
		})
		return nil
	})
	if err != nil {
		tb.Fatal(err)
	}
	return sc1, sc2
}

// Benchmarks

func BenchmarkWriteSecretConnection(b *testing.B) {
	b.StopTimer()
	b.ReportAllocs()
	fooSecConn, barSecConn := makeSecretConnPair(b)
	randomMsgSizes := []int{
		dataSizeMax / 10,
		dataSizeMax / 3,
		dataSizeMax / 2,
		dataSizeMax,
		dataSizeMax * 3 / 2,
		dataSizeMax * 2,
		dataSizeMax * 7 / 2,
	}
	fooWriteBytes := make([][]byte, 0, len(randomMsgSizes))
	for _, size := range randomMsgSizes {
		fooWriteBytes = append(fooWriteBytes, tmrand.Bytes(size))
	}
	// Consume reads from bar's reader
	spawnBgForTest(b, func(ctx context.Context) error {
		readBuffer := make([]byte, dataSizeMax)
		for {
			if err := barSecConn.Read(b.Context(), readBuffer); err != nil {
				return err
			}
		}
	})

	ctx := b.Context()
	for b.Loop() {
		idx := mrand.Intn(len(fooWriteBytes))
		if err := fooSecConn.Write(ctx, fooWriteBytes[idx]); err != nil {
			b.Errorf("failed to write to fooSecConn: %v", err)
			return
		}
	}
}

func BenchmarkReadSecretConnection(b *testing.B) {
	b.StopTimer()
	b.ReportAllocs()
	fooSecConn, barSecConn := makeSecretConnPair(b)
	randomMsgSizes := []int{
		dataSizeMax / 10,
		dataSizeMax / 3,
		dataSizeMax / 2,
		dataSizeMax,
		dataSizeMax * 3 / 2,
		dataSizeMax * 2,
		dataSizeMax * 7 / 2,
	}
	fooWriteBytes := make([][]byte, 0, len(randomMsgSizes))
	for _, size := range randomMsgSizes {
		fooWriteBytes = append(fooWriteBytes, tmrand.Bytes(size))
	}
	spawnBgForTest(b, func(ctx context.Context) error {
		for {
			idx := mrand.Intn(len(fooWriteBytes))
			if err := fooSecConn.Write(ctx, fooWriteBytes[idx]); err != nil {
				return fmt.Errorf("failed to write to fooSecConn: %w", err)
			}
		}
	})

	ctx := b.Context()
	for b.Loop() {
		readBuffer := make([]byte, dataSizeMax)
		if err := barSecConn.Read(ctx, readBuffer); err != nil {
			b.Fatalf("Failed to read from barSecConn: %v", err)
		}
	}
}
