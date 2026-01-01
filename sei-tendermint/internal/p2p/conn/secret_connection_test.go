package conn

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	mrand "math/rand"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/tendermint/tendermint/crypto/ed25519"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/require"
	"github.com/tendermint/tendermint/libs/utils/scope"
)

// Run go test -update from within this module
// to update the golden test vector file
var update = flag.Bool("update", false, "update .golden files")

type kvstoreConn struct {
	net.Conn
	reader *io.PipeReader
	writer *io.PipeWriter
}

func (drw kvstoreConn) Read(data []byte) (n int, err error)  { return drw.reader.Read(data) }
func (drw kvstoreConn) Write(data []byte) (n int, err error) { return drw.writer.Write(data) }

func (drw kvstoreConn) Close() (err error) {
	err2 := drw.writer.CloseWithError(io.EOF)
	err1 := drw.reader.Close()
	if err2 != nil {
		return err
	}
	return err1
}

func TestSecretConnectionHandshake(t *testing.T) {
	fooSecConn, barSecConn := makeSecretConnPair(t)
	require.NoError(t, fooSecConn.Close())
	require.NoError(t, barSecConn.Close())
}

func TestConcurrentReadWrite(t *testing.T) {
	ctx := t.Context()
	sc1, sc2 := makeSecretConnPair(t)
	rng := utils.TestRng()
	fooWriteText := utils.GenBytes(rng, dataMaxSize)
	n := 100 * dataMaxSize

	// read from two routines.
	// should be safe from race according to net.Conn:
	// https://golang.org/pkg/net/#Conn
	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error { return readLots(sc1, n) })
		s.Spawn(func() error { return readLots(sc1, n) })
		s.Spawn(func() error { return writeLots(sc2, fooWriteText, n) })
		s.Spawn(func() error { return writeLots(sc2, fooWriteText, n) })
		return nil
	})
	require.NoError(t, err)
	require.NoError(t, sc1.Close())
	require.NoError(t, sc2.Close())
}

func TestSecretConnectionReadWrite(t *testing.T) {
	ctx := t.Context()
	c1, c2 := makeKVStoreConnPair()
	writes := utils.NewMutex(map[ed25519.PublicKey][]byte{})
	reads := utils.NewMutex(map[ed25519.PublicKey][]byte{})
	rng := utils.TestRng()
	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		for _, c := range utils.Slice(c1, c2) {
			s.Spawn(func() error {
				k := ed25519.GenerateSecretKey()
				sc, err := MakeSecretConnection(ctx, c, k)
				if err != nil {
					return fmt.Errorf("MakeSecretConnection(): %w", err)
				}
				// In parallel, handle some reads and writes.
				s.Spawn(func() error {
					var ws []byte
					for range 100 {
						w := utils.GenBytes(rng, rng.Intn(dataMaxSize*5)+1)
						ws = append(ws, w...)
						n, err := sc.Write(w)
						if err != nil {
							return fmt.Errorf("failed to write to nodeSecretConn: %w", err)
						}
						if n != len(w) {
							return fmt.Errorf("failed to write all bytes. Expected %v, wrote %v", len(w), n)
						}
					}
					for writes := range writes.Lock() {
						writes[k.Public()] = ws
					}
					return c.writer.Close()
				})
				s.Spawn(func() error {
					var rs []byte
					readBuffer := make([]byte, dataMaxSize)
					for {
						n, err := sc.Read(readBuffer)
						if err != nil {
							if errors.Is(err, io.EOF) {
								for reads := range reads.Lock() {
									reads[sc.RemotePubKey()] = rs
								}
								return c.reader.Close()
							}
							return fmt.Errorf("failed to read from nodeSecretConn: %w", err)
						}
						if n == 0 {
							return fmt.Errorf("Read() is nonblocking")
						}
						rs = append(rs, readBuffer[:n]...)
					}
				})
				return nil
			})
		}
		return nil
	})
	require.NoError(t, err)
	for reads := range reads.Lock() {
		for writes := range writes.Lock() {
			for k, want := range writes {
				require.Equal(t, want, reads[k])
			}
		}
	}
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

func writeLots(sc *SecretConnection, data []byte, total int) error {
	for total > 0 {
		n := min(len(data), total)
		total -= n
		if _, err := sc.Write(data[:n]); err != nil {
			return err
		}
	}
	return nil
}

func readLots(sc *SecretConnection, total int) error {
	for total > 0 {
		readBuffer := make([]byte, min(dataMaxSize, total))
		n, err := sc.Read(readBuffer)
		if err != nil {
			return err
		}
		total -= n
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

// Each returned ReadWriteCloser is akin to a net.Connection
func makeKVStoreConnPair() (fooConn, barConn kvstoreConn) {
	barReader, fooWriter := io.Pipe()
	fooReader, barWriter := io.Pipe()
	return kvstoreConn{reader: fooReader, writer: fooWriter}, kvstoreConn{reader: barReader, writer: barWriter}
}

func makeSecretConnPair(tb testing.TB) (sc1 *SecretConnection, sc2 *SecretConnection) {
	ctx := tb.Context()
	c1, c2 := makeKVStoreConnPair()
	k1 := ed25519.GenerateSecretKey()
	k2 := ed25519.GenerateSecretKey()

	// Make connections from both sides in parallel.
	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error {
			var err error
			sc1, err = MakeSecretConnection(ctx, c1, k1)
			return err
		})
		s.Spawn(func() error {
			var err error
			sc2, err = MakeSecretConnection(ctx, c2, k2)
			return err
		})
		return nil
	})
	if err != nil {
		tb.Fatal(err)
	}
	require.Equal(tb, k1.Public(), sc2.RemotePubKey())
	require.Equal(tb, k2.Public(), sc1.RemotePubKey())
	return sc1, sc2
}

// Benchmarks

func BenchmarkWriteSecretConnection(b *testing.B) {
	b.StopTimer()
	b.ReportAllocs()
	fooSecConn, barSecConn := makeSecretConnPair(b)
	randomMsgSizes := []int{
		dataMaxSize / 10,
		dataMaxSize / 3,
		dataMaxSize / 2,
		dataMaxSize,
		dataMaxSize * 3 / 2,
		dataMaxSize * 2,
		dataMaxSize * 7 / 2,
	}
	fooWriteBytes := make([][]byte, 0, len(randomMsgSizes))
	for _, size := range randomMsgSizes {
		fooWriteBytes = append(fooWriteBytes, tmrand.Bytes(size))
	}
	// Consume reads from bar's reader
	go func() {
		readBuffer := make([]byte, dataMaxSize)
		for {
			_, err := barSecConn.Read(readBuffer)
			if err == io.EOF {
				return
			} else if err != nil {
				b.Errorf("failed to read from barSecConn: %v", err)
				return
			}
		}
	}()

	for b.Loop() {
		idx := mrand.Intn(len(fooWriteBytes))
		_, err := fooSecConn.Write(fooWriteBytes[idx])
		if err != nil {
			b.Errorf("failed to write to fooSecConn: %v", err)
			return
		}
	}

	if err := fooSecConn.Close(); err != nil {
		b.Error(err)
	}
	// barSecConn.Close() race condition
}

func BenchmarkReadSecretConnection(b *testing.B) {
	b.StopTimer()
	b.ReportAllocs()
	fooSecConn, barSecConn := makeSecretConnPair(b)
	randomMsgSizes := []int{
		dataMaxSize / 10,
		dataMaxSize / 3,
		dataMaxSize / 2,
		dataMaxSize,
		dataMaxSize * 3 / 2,
		dataMaxSize * 2,
		dataMaxSize * 7 / 2,
	}
	fooWriteBytes := make([][]byte, 0, len(randomMsgSizes))
	for _, size := range randomMsgSizes {
		fooWriteBytes = append(fooWriteBytes, tmrand.Bytes(size))
	}
	go func() {
		for i := range b.N {
			idx := mrand.Intn(len(fooWriteBytes))
			_, err := fooSecConn.Write(fooWriteBytes[idx])
			if err != nil {
				b.Errorf("failed to write to fooSecConn: %v, %v,%v", err, i, b.N)
				return
			}
		}
	}()

	for b.Loop() {
		readBuffer := make([]byte, dataMaxSize)
		_, err := barSecConn.Read(readBuffer)

		if err == io.EOF {
			return
		} else if err != nil {
			b.Fatalf("Failed to read from barSecConn: %v", err)
		}
	}
}
