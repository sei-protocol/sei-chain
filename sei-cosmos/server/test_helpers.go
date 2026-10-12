package server

import (
	"errors"
	"fmt"
	"math/rand"
	"net"
	"os"
	"strings"
	"sync"
)

// Ports are handed out from below the kernel's ephemeral range. A port picked by binding
// ":0" is released before the caller binds it, and in that gap it is a candidate for every
// other ":0" listener and every outbound connection on the host, which is how parallel test
// packages end up on the same port. Nothing else allocates from below the range, so a port
// from there can only collide with another caller of FreeTCPAddr.
const (
	freePortFloor           = 10000
	defaultEphemeralFloor   = 32768
	ipLocalPortRangePath    = "/proc/sys/net/ipv4/ip_local_port_range"
	freePortAttemptsPerCall = 1000
)

var (
	freePortMu     sync.Mutex
	freePortIssued = map[int]struct{}{}
)

// Get a free address for a test tendermint server
// protocol is either tcp, http, etc
func FreeTCPAddr() (addr, port string, err error) {
	ceiling := ephemeralFloor()
	freePortMu.Lock()
	defer freePortMu.Unlock()
	for range freePortAttemptsPerCall {
		candidate := freePortFloor + rand.Intn(ceiling-freePortFloor) //nolint:gosec // G404: port selection, not security sensitive
		if _, issued := freePortIssued[candidate]; issued {
			continue
		}
		l, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", candidate))
		if err != nil {
			continue
		}
		if err := l.Close(); err != nil {
			return "", "", fmt.Errorf("couldn't close the listener: %w", err)
		}
		freePortIssued[candidate] = struct{}{}
		port = fmt.Sprintf("%d", candidate)
		addr = fmt.Sprintf("tcp://0.0.0.0:%s", port)
		return addr, port, nil
	}
	return "", "", errors.New("no free port found below the ephemeral range")
}

// ephemeralFloor returns the lowest port the kernel hands to ":0" listeners and outbound
// connections, falling back to the Linux default where it cannot be read.
func ephemeralFloor() int {
	raw, err := os.ReadFile(ipLocalPortRangePath)
	if err != nil {
		return defaultEphemeralFloor
	}
	var lower, upper int
	if _, err := fmt.Sscan(strings.TrimSpace(string(raw)), &lower, &upper); err != nil || lower <= freePortFloor {
		return defaultEphemeralFloor
	}
	return lower
}
