package tcp

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sync/atomic"
	"os"

	"golang.org/x/sys/unix"

	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/scope"
)

var reservedAddrs = utils.NewMutex(map[netip.AddrPort]struct{}{})

// IPv4Loopback returns the IPv4 loopback address.
func IPv4Loopback() netip.Addr { return netip.AddrFrom4([4]byte{127, 0, 0, 1}) }

func Dial(ctx context.Context, addr netip.AddrPort) (*net.TCPConn, error) {
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "tcp", addr.String())
	if err != nil {
		return nil, err
	}
	return conn.(*net.TCPConn), nil
}

// Accepts an incoming TCP connection.
// Closes the listener if ctx is done before a connection is accepted.
func AcceptOrClose(ctx context.Context, l *net.TCPListener) (*net.TCPConn, error) {
	var res atomic.Pointer[net.TCPConn]
	err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error {
			// Early error check. Close listener to terminate Accept.
			// This task guarantees that either err of res are set (possibly both).
			<-ctx.Done()
			if res.Load() != nil {
				return nil
			}
			l.Close()
			return ctx.Err()
		})
		conn, err := l.AcceptTCP()
		if err != nil {
			l.Close()
			return err
		}
		res.Store(conn)
		return nil
	})
	// At this point err!=nil => l is closed.
	conn := res.Load()
	// Handle the race condition, where conn is accepted, but listener gets closed anyway.
	// We close the conn to adhere to the function contract.
	if conn != nil && err != nil {
		conn.Close()
		conn = nil
	}
	return conn, err
}

// Listen opens a TCP listener on the given address.
// It takes into account the reserved addresses (in tests) and sets the SO_REUSEPORT.
// nolint: contextcheck
func Listen(addr netip.AddrPort) (*net.TCPListener, error) {
	if addr.Port() == 0 {
		return nil, errors.New("listening on anyport (i.e. 0) is not allowed. If you are implementing a test use TestReserveAddr() instead") // nolint:lll
	}
	var domain int
	if addr.Addr().Is4() {
		domain = unix.AF_INET
	} else {
		domain = unix.AF_INET6
	}
	fd, err := unix.Socket(domain, unix.SOCK_STREAM, 0)
	if err != nil {
		panic(err)
	}
	if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); err != nil {
		panic(err)
	}
	if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_REUSEPORT, 1); err != nil {
		panic(err)
	}
	// NOTE
	// linux allows sharing REUSEPORT port across various local IPs.
	// macOS does not.
	var addrAny unix.Sockaddr
	if addr.Addr().Is4() {
		addrAny = &unix.SockaddrInet4{Port: 0, Addr: addr.Addr().As4()}
	} else {
		addrAny = &unix.SockaddrInet6{Port: 0, Addr: addr.Addr().As16()}
	}
	if err := unix.Bind(fd, addrAny); err != nil {
		panic(err)
	}
	if err:=unix.Listen(fd, 5); err!=nil {
		panic(err)
	}
	f := os.NewFile(uintptr(fd), "listener")
	l, err := net.FileListener(f)
	if err != nil {
		panic(err)
	}
	return l.(*net.TCPListener), nil
}

// TestReserveAddr (testonly) reserves a localhost port in ephemeral range to open a TCP listener on it.
// Reservation prevents race conditions with other processes.
func TestReserveAddr() netip.AddrPort {
	return TestReservePort(IPv4Loopback())
}

// TestReservePort (testonly) reserves a port on the given ip in ephemeral range to open a TCP listener on it.
// Reservation prevents race conditions with other processes.
func TestReservePort(ip netip.Addr) netip.AddrPort {
	// Bind a new socket to reserve a port,
	// Don't mark it as listening to avoid the kernel from queueing up connections
	// on that socket.

	var domain int
	if ip.Is4() {
		domain = unix.AF_INET
	} else {
		domain = unix.AF_INET6
	}
	fd, err := unix.Socket(domain, unix.SOCK_STREAM, 0)
	if err != nil {
		panic(err)
	}
	if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); err != nil {
		panic(err)
	}
	if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_REUSEPORT, 1); err != nil {
		panic(err)
	}

	// NOTE
	// linux allows sharing REUSEPORT port across various local IPs.
	// macOS does not.
	var addrAny unix.Sockaddr
	if ip.Is4() {
		addrAny = &unix.SockaddrInet4{Port: 0, Addr: ip.As4()}
	} else {
		addrAny = &unix.SockaddrInet6{Port: 0, Addr: ip.As16()}
	}
	if err := unix.Bind(fd, addrAny); err != nil {
		panic(err)
	}

	addrRaw, err := unix.Getsockname(fd)
	if err != nil {
		panic(err)
	}
	var port uint16
	if ip.Is4() {
		port = uint16(addrRaw.(*unix.SockaddrInet4).Port)
	} else {
		port = uint16(addrRaw.(*unix.SockaddrInet6).Port)
	}
	addr := netip.AddrPortFrom(ip, port)
	for addrs := range reservedAddrs.Lock() {
		addrs[addr] = struct{}{}
	}
	return addr
}

func TestPipe() (*net.TCPConn, *net.TCPConn) {
	addr := TestReserveAddr()
	listen, err := Listen(addr)
	if err != nil {
		panic(err)
	}
	defer listen.Close()
	var c1, c2 *net.TCPConn
	ctx := context.Background()
	scope.Parallel(func(s scope.ParallelScope) error {
		s.Spawn(func() error {
			var err error
			if c1, err = AcceptOrClose(ctx, listen); err != nil {
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
	return c1, c2
}
