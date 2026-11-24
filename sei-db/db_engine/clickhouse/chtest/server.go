// Package chtest gives tests a ClickHouse server to run against.
package chtest

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/ClickHouse/ch-go"
)

const (
	// AddrEnv names an existing server to use instead of starting one.
	AddrEnv = "SEI_ARCHIVE_TEST_CLICKHOUSE"
	// PasswordEnv is the default user's password on the server AddrEnv names.
	PasswordEnv = "SEI_ARCHIVE_TEST_PASSWORD"

	startTimeout = 60 * time.Second
	stopTimeout  = 30 * time.Second
)

// Server is a ClickHouse server reachable over the native protocol.
type Server struct {
	Addr     string
	User     string
	Password string
}

var (
	once    sync.Once
	shared  Server
	started *process
	errOnce error
)

// Start returns the package's ClickHouse server, starting a local one on first use. It
// uses the server AddrEnv names when set. Without either, it skips the test, or fails it
// when the CI environment variable is set.
func Start(t testing.TB) Server {
	t.Helper()
	if addr := os.Getenv(AddrEnv); addr != "" {
		return Server{Addr: addr, User: "default", Password: os.Getenv(PasswordEnv)}
	}
	bin, err := exec.LookPath("clickhouse")
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("clickhouse is not on PATH and %s is not set; CI runners must provide one", AddrEnv)
		}
		t.Skipf("clickhouse is not on PATH and %s is not set", AddrEnv)
	}
	once.Do(func() {
		started, errOnce = startLocal(bin)
		if errOnce == nil {
			shared = Server{Addr: started.addr, User: "default"}
		}
	})
	if errOnce != nil {
		t.Fatalf("start local clickhouse: %v", errOnce)
	}
	return shared
}

// Main runs the package's tests, then stops the local server Start started.
func Main(m *testing.M) {
	code := m.Run()
	if started != nil {
		if err := started.stop(); err != nil {
			fmt.Fprintf(os.Stderr, "chtest: stop local clickhouse: %v\n", err)
			if code == 0 {
				code = 1
			}
		}
	}
	os.Exit(code)
}

// process is a ClickHouse server Start launched, with its scratch directory.
type process struct {
	cmd    *exec.Cmd
	dir    string
	addr   string
	exited chan struct{}
	err    error // set before exited closes
}

// startLocal launches bin as a server in a fresh scratch directory and waits until it
// accepts queries.
func startLocal(bin string) (*process, error) {
	dir, err := os.MkdirTemp("", "chtest-")
	if err != nil {
		return nil, err
	}
	port, err := freePort()
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	config := filepath.Join(dir, "config.xml")
	if err := os.WriteFile(config, []byte(serverConfig(dir, port)), 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	output, err := os.Create(filepath.Clean(filepath.Join(dir, "stdout.log")))
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	defer func() { _ = output.Close() }()

	p := &process{
		cmd:    exec.Command(bin, "server", "--config-file", config), //nolint:gosec // bin comes from exec.LookPath
		dir:    dir,
		addr:   net.JoinHostPort("127.0.0.1", strconv.Itoa(port)),
		exited: make(chan struct{}),
	}
	p.cmd.Dir = dir
	p.cmd.Stdout = output
	p.cmd.Stderr = output
	if err := p.cmd.Start(); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	go func() {
		p.err = p.cmd.Wait()
		close(p.exited)
	}()
	if err := p.waitReady(); err != nil {
		_ = p.stop()
		return nil, fmt.Errorf("%w\n%s", err, p.logTail())
	}
	return p, nil
}

// waitReady polls the server with a ping until it answers, exits, or startTimeout passes.
func (p *process) waitReady() error {
	deadline := time.Now().Add(startTimeout)
	for {
		select {
		case <-p.exited:
			return fmt.Errorf("clickhouse exited during startup: %v", p.err)
		default:
		}
		if err := ping(p.addr); err == nil {
			return nil
		} else if time.Now().After(deadline) {
			return fmt.Errorf("clickhouse not ready after %s: %w", startTimeout, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// stop shuts the server down, killing it if it outlives stopTimeout, and removes its
// scratch directory.
func (p *process) stop() error {
	defer func() { _ = os.RemoveAll(p.dir) }()
	select {
	case <-p.exited:
		return nil
	default:
	}
	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	select {
	case <-p.exited:
		return nil
	case <-time.After(stopTimeout):
		_ = p.cmd.Process.Kill()
		<-p.exited
		return fmt.Errorf("clickhouse did not stop within %s and was killed", stopTimeout)
	}
}

// logTail returns the end of the server's logs, for startup failure messages.
func (p *process) logTail() string {
	var out string
	for _, name := range []string{"stdout.log", "clickhouse-server.err.log"} {
		b, err := os.ReadFile(filepath.Clean(filepath.Join(p.dir, name)))
		if err != nil || len(b) == 0 {
			continue
		}
		const limit = 4 << 10
		if len(b) > limit {
			b = b[len(b)-limit:]
		}
		out += fmt.Sprintf("--- %s ---\n%s\n", name, b)
	}
	return out
}

// ping dials addr as the default user and pings it.
func ping(addr string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client, err := ch.Dial(ctx, ch.Options{Address: addr})
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	return client.Ping(ctx)
}

// freePort returns a loopback TCP port that was free a moment ago.
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// serverConfig returns a server configuration that keeps all state under dir, listens
// only on port over the native protocol, and caps memory for a shared CI machine.
func serverConfig(dir string, port int) string {
	return fmt.Sprintf(`<clickhouse>
    <logger>
        <level>warning</level>
        <log>%[1]s/clickhouse-server.log</log>
        <errorlog>%[1]s/clickhouse-server.err.log</errorlog>
    </logger>
    <listen_host>127.0.0.1</listen_host>
    <tcp_port>%[2]d</tcp_port>
    <path>%[1]s/data/</path>
    <tmp_path>%[1]s/tmp/</tmp_path>
    <user_files_path>%[1]s/user_files/</user_files_path>
    <format_schema_path>%[1]s/format_schemas/</format_schema_path>
    <max_server_memory_usage>2147483648</max_server_memory_usage>
    <mark_cache_size>268435456</mark_cache_size>
    <shutdown_wait_unfinished>0</shutdown_wait_unfinished>
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
`, dir, port)
}
