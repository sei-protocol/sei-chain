package conn

import (
	"context"
	"errors"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"io"
	"net"
	"net/netip"
)

type Conn interface {
	LocalAddr() netip.AddrPort
	RemoteAddr() netip.AddrPort
	Read(ctx context.Context, data []byte) error
	Write(ctx context.Context, data []byte) error
	Flush(ctx context.Context) error
	Close()
}

func IsDisconnect(err error) bool {
	return errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) || utils.ErrorAs[*net.OpError](err).IsPresent()
}
