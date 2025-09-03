package tcp

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/tendermint/tendermint/libs/utils"
)

var reservedAddrs = utils.NewMutex(map[netip.AddrPort]struct{}{})

// IPv4Loopback returns the IPv4 loopback address.
func IPv4Loopback() netip.Addr { return netip.AddrFrom4([4]byte{127, 0, 0, 1}) }

// Norm normalizes address by unmapping IPv4 -> IPv6 embedding.
func Norm(addr netip.AddrPort) netip.AddrPort {
	return netip.AddrPortFrom(addr.Addr().Unmap(), addr.Port())
}

// Listen opens a TCP listener on the given address.
// It takes into account the reserved addresses (in tests) and sets the SO_REUSEPORT.
// nolint: contextcheck
func Listen(addr netip.AddrPort) (net.Listener, error) {
	if addr.Port() == 0 {
		return nil, errors.New("listening on anyport (i.e. 0) is not allowed. If you are implementing a test use TestReserveAddr() instead") // nolint:lll
	}
	cfg := net.ListenConfig{}
	for addrs := range reservedAddrs.Lock() {
		if _, ok := addrs[addr]; ok {
			cfg.Control = func(network, address string, c syscall.RawConn) error {
				var errInner error
				if err := c.Control(func(fd uintptr) {
					errInner = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
				}); err != nil {
					return err
				}
				return errInner
			}
		}
	}
	// Passing the background context is ok, because Listen is
	// non-blocking if it doesn't need to resolve the address
	// against a DNS server.
	return cfg.Listen(context.Background(), "tcp", addr.String())
}

// TestReserveAddr (testonly) reserves a port in ephemeral range to open a TCP listener on it.
// Reservation prevents race conditions with other processes.
func TestReserveAddr() netip.AddrPort {
	// Bind a new socket to reserve a port,
	// Don't mark it as listening to avoid the kernel from queueing up connections
	// on that socket.
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM, 0)
	if err != nil {
		panic(err)
	}
	if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); err != nil {
		panic(err)
	}
	if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_REUSEPORT, 1); err != nil {
		panic(err)
	}
	ip := IPv4Loopback()
	addrAny := &unix.SockaddrInet4{Port: 0, Addr: ip.As4()}
	if err := unix.Bind(fd, addrAny); err != nil {
		panic(err)
	}

	addrRaw, err := unix.Getsockname(fd)
	if err != nil {
		panic(err)
	}
	port := uint16(addrRaw.(*unix.SockaddrInet4).Port)
	addr := netip.AddrPortFrom(ip, port)
	for addrs := range reservedAddrs.Lock() {
		addrs[addr] = struct{}{}
	}
	return addr
}
