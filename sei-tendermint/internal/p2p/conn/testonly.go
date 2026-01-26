package conn

import (
	"context"
	"bytes"
	"github.com/tendermint/tendermint/libs/utils"
)

type buf struct {
	data [1024]byte
	begin,end uint64
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

func (b *buf) pop(data []byte) int {æ
	n := min(len(data),æ
}

type TestConn struct {
	send *utils.Watch[*buf]
	recv *utils.Watch[*buf]
}

func (c *TestConn) Recv(ctx context.Context) error {

}

func (c *TestConn) Send(ctx context.Context, data []byte) error {
	for send,ctrl := range c.send.Lock() {
		for {
			n := send.push(data)
			if n>0 { ctrl.Updated() }
			data = data[n:]
			if len(data)==0 { return nil } 
			if err:=ctrl.Wait(ctx); err!=nil { return err }
		}
	}
	return nil
}

func (c *TestConn) Close() {}

func NewTestConn() (*TestConn,*TestConn) {
	b1 := utils.NewWatch(&buf{})
	b2 := utils.NewWatch(&buf{})
	return &TestConn{&b1,&b2},&TestConn{&b2,&b1}
}

