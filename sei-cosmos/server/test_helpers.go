package server

import (
	"fmt"
	"math/rand"
	"net"
	"time"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// Get a free address for a test tendermint server
// protocol is either tcp, http, etc
func FreeTCPAddr() (addr, port string, err error) {
	// Add a small random delay to reduce port allocation race conditions
	// when multiple tests are allocating ports concurrently.
	// The delay helps stagger port allocations across concurrent test runs.
	time.Sleep(time.Duration(rand.Intn(50)) * time.Millisecond)

	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return "", "", err
	}

	if err := l.Close(); err != nil {
		return "", "", sdkerrors.Wrap(err, "couldn't close the listener")
	}

	portI := l.Addr().(*net.TCPAddr).Port
	port = fmt.Sprintf("%d", portI)
	addr = fmt.Sprintf("tcp://0.0.0.0:%s", port)
	return
}
