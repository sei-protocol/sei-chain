package tcp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strconv"
	"sync/atomic"

	"golang.org/x/sys/unix"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

type call struct {
	data []byte
	done chan bool
}

type Conn struct {
	writes chan call
	reads  chan call
	errors chan error
	conn   *net.TCPConn
}

func (c Conn) Read(ctx context.Context, data []byte) error {
	done := make(chan bool)
	if err := utils.Send(ctx, c.reads, call{data, done}); err != nil {
		return err
	}
	ok, err := utils.Recv(ctx, done)
	if err != nil {
		_ = c.conn.CloseRead() // close the read half.
		<-done                 // wait for data ownership to be returned.
		return err
	}
	if !ok {
		<-ctx.Done() // wait for context to finish.
		return ctx.Err()
	}
	return nil
}

func (c Conn) Write(ctx context.Context, data []byte) error {
	done := make(chan bool)
	if err := utils.Send(ctx, c.writes, call{data, done}); err != nil {
		return err
	}
	ok, err := utils.Recv(ctx, done)
	if err != nil {
		_ = c.conn.CloseWrite() // close the write half.
		<-done                  // wait for data ownership to be returned.
		return err
	}
	if !ok {
		// wait for context to finish.
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}

func (c Conn) Flush(_ context.Context) error { return nil }
func (c Conn) Close()                        { _ = c.conn.Close() }

func (c Conn) Run(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error {
			for {
				call, err := utils.Recv(ctx, c.writes)
				if err != nil {
					return err
				}
				_, err = c.conn.Write(call.data)
				call.done <- err == nil
				if err != nil {
					return err
				}
			}
		})
		s.Spawn(func() error {
			for {
				call, err := utils.Recv(ctx, c.reads)
				if err != nil {
					return err
				}
				for len(call.data) > 0 {
					n, err := c.conn.Read(call.data)
					if err != nil {
						call.done <- false
						return err
					}
					call.data = call.data[n:]
				}
				call.done <- true
			}
		})
		<-ctx.Done()
		s.Cancel(ctx.Err())
		_ = c.conn.Close()
		return nil
	})
}

func (c Conn) LocalAddr() netip.AddrPort {
	return c.conn.LocalAddr().(*net.TCPAddr).AddrPort()
}

func (c Conn) RemoteAddr() netip.AddrPort {
	return c.conn.RemoteAddr().(*net.TCPAddr).AddrPort()
}

// reserverAddrs is a global register of reserved ports.
//   - Some(fd) indicates that the port is not currently in use.
//     fd is the socket bound to the addr, which guards the port from being allocated to different process.
//   - None indicates that the port is currently in use.
//     Calling Listen() for this addr will result in error, until the current listener closes.
var reservedAddrs = utils.NewMutex(map[netip.AddrPort]utils.Option[int]{})

// IPv4Loopback returns the IPv4 loopback address.
func IPv4Loopback() netip.Addr { return netip.AddrFrom4([4]byte{127, 0, 0, 1}) }

func Dial(ctx context.Context, addr netip.AddrPort) (Conn, error) {
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "tcp", addr.String())
	if err != nil {
		return Conn{}, err
	}
	return Conn{
		conn:   conn.(*net.TCPConn),
		writes: make(chan call),
		reads:  make(chan call),
	}, nil
}

type HostPort struct {
	Hostname string
	Port     uint16
}

func (hp HostPort) String() string {
	return net.JoinHostPort(hp.Hostname, strconv.FormatInt(int64(hp.Port), 10))
}

func ParseHostPort(hp string) (HostPort, error) {
	h, p, err := net.SplitHostPort(hp)
	if err != nil {
		return HostPort{}, err
	}
	port, err := strconv.ParseUint(p, 10, 16)
	if err != nil {
		return HostPort{}, err
	}
	return HostPort{h, uint16(port)}, nil
}

func (hp HostPort) Resolve(ctx context.Context) ([]netip.AddrPort, error) {
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", hp.Hostname)
	if err != nil {
		return nil, err
	}
	addrs := make([]netip.AddrPort, len(ips))
	for i, ip := range ips {
		ip, ok := netip.AddrFromSlice(ip)
		if !ok {
			return nil, fmt.Errorf("LookupIP() returned invalid ip address")
		}
		addrs[i] = netip.AddrPortFrom(ip, hp.Port)
	}
	return addrs, nil
}

type Listener struct {
	reserved atomic.Pointer[netip.AddrPort]
	inner    *net.TCPListener
}

func testBind(addr netip.AddrPort) int {
	var domain int
	if addr.Addr().Is4() {
		domain = unix.AF_INET
	} else {
		domain = unix.AF_INET6
	}
	// NONBLOCK and CLOEXEC for consistency with net.ListenConfig.Listen().
	fd, err := unix.Socket(domain, unix.SOCK_STREAM, 0)
	if err != nil {
		panic(err)
	}
	unix.CloseOnExec(fd)
	if err := unix.SetNonblock(fd, true); err != nil {
		panic(err)
	}
	if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); err != nil {
		panic(err)
	}
	if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_REUSEPORT, 1); err != nil {
		panic(err)
	}
	// NOTE: linux allows sharing REUSEPORT port across 0.0.0.0 and 127.0.0.1, macOS does not.
	// NOTE: linux distributes incoming connections across REUSEPORT listeners,
	// macOS doesn't care that the socket is not listening yet and doesn't even use round-robin.
	var addrAny unix.Sockaddr
	if addr.Addr().Is4() {
		addrAny = &unix.SockaddrInet4{Port: int(addr.Port()), Addr: addr.Addr().As4()}
	} else {
		addrAny = &unix.SockaddrInet6{Port: int(addr.Port()), Addr: addr.Addr().As16()}
	}
	if err := unix.Bind(fd, addrAny); err != nil {
		panic(err)
	}
	return fd
}

func (l *Listener) Close() error {
	// We use reserved to check if the listener is holding ownership of
	// a reserved port. Ownership is released with the first Close() call.
	addr := l.reserved.Swap(nil)
	if addr == nil {
		return l.inner.Close()
	}
	for addrs := range reservedAddrs.Lock() {
		addrs[*addr] = utils.Some(testBind(*addr))
		// We close under lock to avoid the following race scenario:
		// 1. old listener releases port
		// 2. new listener acquires port
		// 3. port is dialed (old listener still open)
		// 4. old listener closes.
		return l.inner.Close()
	}
	panic("unreachable")
}

// Accepts an incoming TCP connection.
// Closes the listener if ctx is done before a connection is accepted.
func (l *Listener) AcceptOrClose(ctx context.Context) (Conn, error) {
	var res atomic.Pointer[net.TCPConn]
	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error {
			// Early error check. Close listener to terminate Accept.
			// This task guarantees that either err of res are set (possibly both).
			<-ctx.Done()
			if res.Load() != nil {
				return nil
			}
			s.Cancel(ctx.Err())
			_ = l.Close()
			return nil
		})
		conn, err := l.inner.AcceptTCP()
		if err != nil {
			s.Cancel(err)
			return nil
		}
		res.Store(conn)
		return nil
	})
	// If there were no error, then res contains an open connection.
	if err == nil {
		return Conn{
			conn:   res.Load(),
			reads:  make(chan call),
			writes: make(chan call),
		}, nil
	}
	// Otherwise close the listener (for consistency), and close the connection (if established).
	_ = l.Close()
	if conn := res.Load(); conn != nil {
		_ = conn.Close()
	}
	return Conn{}, err
}

// Listen opens a TCP listener on the given address.
// It takes into account the reserved addresses (in tests) and sets the SO_REUSEPORT.
// nolint: contextcheck
func Listen(addr netip.AddrPort) (*Listener, error) {
	if addr.Port() == 0 {
		return nil, errors.New("listening on anyport (i.e. 0) is not allowed. If you are implementing a test use TestReserveAddr() instead") // nolint:lll
	}
	for addrs := range reservedAddrs.Lock() {
		if mfd, reserved := addrs[addr]; reserved {
			fd, ok := mfd.Get()
			if !ok {
				return nil, fmt.Errorf("port already in use")
			}
			addrs[addr] = utils.None[int]()
			// Backlog has to be large enough, so that test dials succeed on the first try.
			if err := unix.Listen(fd, 128); err != nil {
				return nil, fmt.Errorf("unix.Listen(): %w", err)
			}
			f := os.NewFile(uintptr(fd), "listener")
			fl, err := net.FileListener(f)
			if err != nil {
				return nil, fmt.Errorf("net.FileListener(): %w", err)
			}
			// net.FileListener duplicates fd.
			_ = f.Close()
			l := &Listener{inner: fl.(*net.TCPListener)}
			l.reserved.Store(&addr)
			return l, nil
		}
	}
	cfg := net.ListenConfig{}
	// Passing the background context is ok, because Listen is
	// non-blocking if it doesn't need to resolve the address
	// against a DNS server.
	l, err := cfg.Listen(context.Background(), "tcp", addr.String())
	if err != nil {
		return nil, err
	}
	return &Listener{inner: l.(*net.TCPListener)}, nil
}

// TestReserveAddr (testonly) reserves a localhost port in ephemeral range to open a TCP listener on it.
// Reservation prevents race conditions with other processes.
func TestReserveAddr() netip.AddrPort {
	return TestReservePort(IPv4Loopback())
}

// TestReservePort (testonly) reserves a port on the given ip in ephemeral range to open a TCP listener on it.
// Reservation prevents race conditions with other processes.
func TestReservePort(ip netip.Addr) netip.AddrPort {
	fd := testBind(netip.AddrPortFrom(ip, 0))
	addrRaw, err := unix.Getsockname(fd)
	if err != nil {
		panic(err)
	}
	var port uint16
	if ip.Is4() {
		port = uint16(addrRaw.(*unix.SockaddrInet4).Port) //nolint:gosec // OS-assigned port is always in valid uint16 range [0, 65535]
	} else {
		port = uint16(addrRaw.(*unix.SockaddrInet6).Port) //nolint:gosec // OS-assigned port is always in valid uint16 range [0, 65535]
	}
	addr := netip.AddrPortFrom(ip, port)
	for addrs := range reservedAddrs.Lock() {
		addrs[addr] = utils.Some(fd)
	}
	return addr
}

func TestPipe() (Conn, Conn) {
	addr := TestReserveAddr()
	listen, err := Listen(addr)
	if err != nil {
		panic(err)
	}
	defer func() { _ = listen.Close() }()
	var c1, c2 Conn
	ctx := context.Background()
	err = scope.Parallel(func(s scope.ParallelScope) error {
		s.Spawn(func() error {
			var err error
			if c1, err = listen.AcceptOrClose(ctx); err != nil {
				panic(err)
			}
			return nil
		})
		s.Spawn(func() error {
			var err error
			if c2, err = Dial(ctx, addr); err != nil {
				panic(err)
			}
			return nil
		})
		return nil
	})
	if err != nil {
		panic(err)
	}
	return c1, c2
}
