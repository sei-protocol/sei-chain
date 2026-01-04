// +build windows

package tcp

import (
	"context"
	"errors"
	"net"
	"net/netip"
)

func LocalAddr(conn *net.TCPConn) netip.AddrPort  { return netip.AddrPort{} }
func RemoteAddr(conn *net.TCPConn) netip.AddrPort { return netip.AddrPort{} }

func Dial(ctx context.Context, addr netip.AddrPort) (*net.TCPConn, error) {
	return nil, errors.New("Dial not implemented on Windows")
}

type Listener struct{}

func Listen(addr netip.AddrPort) (*Listener, error) { return nil, errors.New("Listen not implemented on Windows") }
func TestReserveAddr() netip.AddrPort               { return netip.AddrPort{} }
func TestReservePort(ip netip.Addr) netip.AddrPort  { return netip.AddrPort{} }
func TestPipe() (*net.TCPConn, *net.TCPConn)       { return nil, nil }
