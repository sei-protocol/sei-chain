package clickhouse

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/ClickHouse/ch-go"
)

const (
	// testServerEnvAddr names a running server for tests to use instead of starting one: its native-protocol
	// address, host:port.
	testServerEnvAddr = "SEI_ARCHIVE_TEST_CLICKHOUSE"
	// testServerEnvPassword holds the password of that server's default user.
	testServerEnvPassword = "SEI_ARCHIVE_TEST_PASSWORD"
)

// testServerConfig is a test server's configuration: its data in a directory, its native protocol alone on a
// port of the loopback interface, a default user without a password, and memory limits suited to tests.
const testServerConfig = `<clickhouse>
    <logger><level>warning</level><console>true</console></logger>
    <listen_host>127.0.0.1</listen_host>
    <tcp_port>%d</tcp_port>
    <path>%s/</path>
    <max_server_memory_usage>1073741824</max_server_memory_usage>
    <mark_cache_size>67108864</mark_cache_size>
    <users>
        <default>
            <password></password>
            <networks><ip>127.0.0.1</ip></networks>
            <profile>default</profile>
            <quota>default</quota>
        </default>
    </users>
    <profiles><default/></profiles>
    <quotas><default/></quotas>
</clickhouse>
`

// testServerTimeout bounds how long a test server takes to answer once started, and to exit once stopped.
const testServerTimeout = time.Minute

// testServer is the server of the package under test, set by RunTests.
var testServer struct {
	addr, password string
	// err is why there is no server.
	err error
}

// RunTests runs a package's tests against a ClickHouse server and returns their exit code: the server
// SEI_ARCHIVE_TEST_CLICKHOUSE names or, when it is unset, one started from the clickhouse binary and stopped
// after the tests. A package calls it from its TestMain, as os.Exit(clickhouse.RunTests(m)).
func RunTests(m *testing.M) int {
	if addr := os.Getenv(testServerEnvAddr); addr != "" {
		testServer.addr, testServer.password = addr, os.Getenv(testServerEnvPassword)
		return m.Run()
	}
	addr, stop, err := startTestServer()
	if err != nil {
		testServer.err = err
		return m.Run()
	}
	defer stop()
	testServer.addr = addr
	return m.Run()
}

// TestServer returns the address and password of the ClickHouse server RunTests provides. Without one it skips
// the test, or fails it on CI.
func TestServer(t testing.TB) (addr, password string) {
	t.Helper()
	switch {
	case testServer.err != nil && os.Getenv("CI") != "":
		t.Fatalf("no ClickHouse server: %v", testServer.err)
	case testServer.err != nil:
		t.Skipf("no ClickHouse server: %v", testServer.err)
	case testServer.addr == "":
		t.Fatal("no ClickHouse server: the package's TestMain must call clickhouse.RunTests")
	}
	return testServer.addr, testServer.password
}

// startTestServer starts a ClickHouse server on a free loopback port, with its data in a temporary directory,
// and returns its address and a function that stops it and removes the directory.
func startTestServer() (addr string, stop func(), err error) {
	bin, err := exec.LookPath("clickhouse")
	if err != nil {
		return "", nil, fmt.Errorf("the clickhouse binary is not installed: %w", err)
	}
	port, err := freeTestPort()
	if err != nil {
		return "", nil, err
	}
	dir, err := os.MkdirTemp("", "clickhouse-test-")
	if err != nil {
		return "", nil, err
	}
	cfg, logPath := filepath.Join(dir, "config.xml"), filepath.Join(dir, "server.log")
	if err := os.WriteFile(cfg, fmt.Appendf(nil, testServerConfig, port, dir), 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return "", nil, err
	}
	log, err := os.Create(logPath) //nolint:gosec // a path in the directory created above
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", nil, err
	}
	cmd := exec.Command(bin, "server", "--config-file="+cfg) //nolint:gosec // the clickhouse binary on PATH
	// Without its watchdog the process is the server itself, so killing it stops the server.
	cmd.Env = append(os.Environ(), "CLICKHOUSE_WATCHDOG_ENABLE=0")
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		_ = log.Close()
		_ = os.RemoveAll(dir)
		return "", nil, fmt.Errorf("start clickhouse: %w", err)
	}
	// exited closes when the server exits, after waitErr holds why.
	exited := make(chan struct{})
	var waitErr error
	go func() {
		waitErr = cmd.Wait()
		close(exited)
	}()
	stop = func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-exited:
		case <-time.After(testServerTimeout):
			_ = cmd.Process.Kill()
			<-exited
		}
		_ = log.Close()
		_ = os.RemoveAll(dir)
	}
	addr = net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	if err := waitTestServer(addr, exited); err != nil {
		stop()
		out, _ := os.ReadFile(logPath) //nolint:gosec // the log written above
		if waitErr != nil {
			err = fmt.Errorf("%w (%w)", err, waitErr)
		}
		return "", nil, fmt.Errorf("%w; server log:\n%s", err, out)
	}
	return addr, stop, nil
}

// freeTestPort returns a loopback port that nothing listens on.
func freeTestPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("find a free port: %w", err)
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// waitTestServer waits until the server at addr answers a ping, failing when exited closes or testServerTimeout
// passes first.
func waitTestServer(addr string, exited <-chan struct{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), testServerTimeout)
	defer cancel()
	for {
		if c, err := ch.Dial(ctx, ch.Options{Address: addr}); err == nil {
			err = c.Ping(ctx)
			_ = c.Close()
			if err == nil {
				return nil
			}
		}
		select {
		case <-exited:
			return errors.New("clickhouse exited before answering")
		case <-ctx.Done():
			return fmt.Errorf("clickhouse did not answer within %s", testServerTimeout)
		case <-time.After(50 * time.Millisecond):
		}
	}
}
