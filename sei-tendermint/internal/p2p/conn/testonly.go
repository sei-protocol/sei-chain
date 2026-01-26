package conn

import (
	"context"
	"net/netip"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/tcp"
)

type buf struct {
	addr netip.AddrPort
	data [1500]byte
	begin,end,flushed uint64
}

func (b *buf) capacity() int {
	return len(b.data)-int(b.end-b.begin)
}

func (b *buf) push(data []byte) int {
	n := min(len(data),b.capacity())
	for i := range uint64(n) {
		b.data[(b.end+i)%uint64(len(b.data))] = data[i]
	}
	b.end += uint64(n)
	return n
}

func (b *buf) pop(data []byte) int {
	n := min(int(b.flushed-b.begin),len(data))
	for i := range uint64(n) {
		data[i] = b.data[(b.begin+i)%uint64(len(b.data))]
	}
	b.begin += uint64(n)
	return n
}

var _ Conn = (*TestConn)(nil)

type TestConn struct {
	write *utils.Watch[*buf]
	read *utils.Watch[*buf]
}

func (c *TestConn) Read(ctx context.Context, data []byte) error {
	for read,ctrl := range c.read.Lock() {
		for {
			n := read.pop(data)
			if n>0 { ctrl.Updated() }
			data = data[n:]
			if len(data)==0 { break }
			if err:=ctrl.Wait(ctx); err!=nil { return err }
		}
	}
	return nil
}

func (c *TestConn) Write(ctx context.Context, data []byte) error {
	for write,ctrl := range c.write.Lock() {
		for {
			n := write.push(data)
			data = data[n:]
			if len(data)==0 { break }
			write.flushed = write.end
			ctrl.Updated()
			if err:=ctrl.WaitUntil(ctx,func() bool { return write.capacity()>0 }); err!=nil { return err }
		}
	}
	return nil
}

func (c *TestConn) Flush(_ context.Context) error {
	for write,ctrl := range c.write.Lock() {
		write.flushed = write.end
		ctrl.Updated()
	}
	return nil
}

func (c *TestConn) Close() {}	

func (c *TestConn) LocalAddr() netip.AddrPort {
	for write := range c.write.Lock() {
		return write.addr
	}
	panic("unreachable")
}

func (c *TestConn) RemoteAddr() netip.AddrPort {
	for read := range c.read.Lock() {
		return read.addr
	}
	panic("unreachable")
}

func NewTestConn() (*TestConn,*TestConn) {
	b1 := utils.NewWatch(&buf{addr:tcp.TestReserveAddr()})
	b2 := utils.NewWatch(&buf{addr:tcp.TestReserveAddr()})
	return &TestConn{&b1,&b2},&TestConn{&b2,&b1}
}

