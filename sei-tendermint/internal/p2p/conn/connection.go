package conn

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"reflect"
	"sync/atomic"
	"time"

	"github.com/gogo/protobuf/proto"
	"golang.org/x/time/rate"

	"github.com/tendermint/tendermint/internal/libs/protoio"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/scope"
	"github.com/tendermint/tendermint/proto/tendermint/p2p"
)

func IsDisconnect(err error) bool {
	return errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) || utils.ErrorAs[*net.OpError](err).IsPresent()
}

// ChannelID is an arbitrary channel ID.
type ChannelID uint16

type ChannelDescriptor struct {
	ID       ChannelID
	Priority int

	MessageType proto.Message

	// TODO: Remove once p2p refactor is complete.
	SendQueueCapacity   int
	RecvMessageCapacity int

	// RecvBufferCapacity defines the max buffer size of inbound messages for a
	// given p2p Channel queue.
	RecvBufferCapacity int

	// Human readable name of the channel, used in logging and
	// diagnostics.
	Name string
}

func (chDesc ChannelDescriptor) withDefaults() ChannelDescriptor {
	if chDesc.Priority <= 0 {
		chDesc.Priority = 1
	}
	if chDesc.SendQueueCapacity == 0 {
		chDesc.SendQueueCapacity = 1
	}
	if chDesc.RecvBufferCapacity == 0 {
		chDesc.RecvBufferCapacity = 4096
	}
	if chDesc.RecvMessageCapacity == 0 {
		chDesc.RecvMessageCapacity = 22020096 // 21MB
	}
	return chDesc
}

var errPongTimeout = errors.New("pong timeout")

type errBadEncoding struct{ error }
type errBadChannel struct{ error }

// mConnMessage passes MConnection messages through internal channels.
type mConnMessage struct {
	channelID ChannelID
	payload   []byte
}

/*
Each peer has one `MConnection` (multiplex connection) instance.

__multiplex__ *noun* a system or signal involving simultaneous transmission of
several messages along a single channel of communication.

Each `MConnection` handles message transmission on multiple abstract communication
`Channel`s.  Each channel has a globally unique byte id.
The byte id and the relative priorities of each `Channel` are configured upon
initialization of the connection.
*/
type MConnection struct {
	logger log.Logger

	conn      net.Conn
	sendQueue utils.Watch[*sendQueue]
	recvPong  utils.Mutex[*utils.AtomicSend[bool]]
	recvCh    chan mConnMessage
	config    MConnConfig
}

// MConnConfig is a MConnection configuration.
type MConnConfig struct {
	SendRate                int64         // B/s
	RecvRate                int64         // B/s
	MaxPacketMsgPayloadSize int           // Maximum payload size
	FlushThrottle           time.Duration // Interval to flush writes (throttled)
	PingInterval            time.Duration // Interval to send pings
	PongTimeout             time.Duration // Time to wait for a pong
}

func (c *MConnConfig) getSendRateLimit() rate.Limit {
	if c.SendRate <= 0 {
		return rate.Inf
	}
	return rate.Limit(c.SendRate)
}

func (c *MConnConfig) getRecvRateLimit() rate.Limit {
	if c.RecvRate <= 0 {
		return rate.Inf
	}
	return rate.Limit(c.RecvRate)
}

// DefaultMConnConfig returns the default config.
func DefaultMConnConfig() MConnConfig {
	return MConnConfig{
		// TODO(gprusak): RecvRate should be strictly larger than SendRate,
		// so that under maximal load the backpressure is at the sender.
		SendRate:                512000, // 500KB/s
		RecvRate:                512000, // 500KB/s
		MaxPacketMsgPayloadSize: 1400,   // mirrors MaxPacketMsgPayloadSize from config/config.go
		FlushThrottle:           100 * time.Millisecond,
		PingInterval:            10 * time.Second,
		PongTimeout:             10 * time.Second,
	}
}

type sendQueue struct {
	ping  bool
	pong  bool
	flush utils.Option[time.Time]
	// TODO(gprusak): restrict to channels that peer knows about
	channels map[ChannelID]*sendChannel
}

func newSendQueue(chDescs []*ChannelDescriptor) *sendQueue {
	q := &sendQueue{
		channels: map[ChannelID]*sendChannel{},
	}
	for _, desc := range chDescs {
		desc := desc.withDefaults()
		q.channels[desc.ID] = &sendChannel{
			desc:  desc,
			queue: utils.NewRingBuf[*[]byte](desc.SendQueueCapacity),
		}
	}
	return q
}

func (q *sendQueue) setFlush(t time.Time) {
	if old, ok := q.flush.Get(); ok && old.Before(t) {
		return
	}
	q.flush = utils.Some(t)
}

// NewMConnection wraps net.Conn and creates multiplex connection with a config
func NewMConnection(
	logger log.Logger,
	conn net.Conn,
	chDescs []*ChannelDescriptor,
	config MConnConfig,
) *MConnection {
	return &MConnection{
		logger:    logger,
		conn:      conn,
		sendQueue: utils.NewWatch(newSendQueue(chDescs)),
		recvCh:    make(chan mConnMessage),
		recvPong:  utils.NewMutex(utils.Alloc(utils.NewAtomicSend(false))),
		config:    config,
	}
}

func (c *MConnection) Run(ctx context.Context) error {
	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnNamed("pingRoutine", func() error { return c.pingRoutine(ctx) })
		s.SpawnNamed("sendRoutine", func() error { return c.sendRoutine(ctx) })
		s.SpawnNamed("recvRoutine", func() error { return c.recvRoutine(ctx) })
		s.SpawnNamed("statsRoutine", func() error { return c.statsRoutine(ctx) })
		// Unfortunately golang std IO operations do not support cancellation via context.
		// Instead, we trigger cancellation by closing the underlying connection.
		// Alternatively, we could utilise net.Conn.Set[Read|Write]Deadline() methods
		// for precise cancellation, but we don't have a need for that here.
		<-ctx.Done()
		s.Cancel(ctx.Err())
		c.conn.Close()
		return nil
	})
}

func (c *MConnection) String() string {
	return fmt.Sprintf("MConn{%v}", c.conn.RemoteAddr())
}

// Queues a message to be sent.
// WARNING: takes ownership of msgBytes
// TODO(gprusak): fix the ownership
func (c *MConnection) Send(ctx context.Context, chID ChannelID, msgBytes []byte) error {
	c.logger.Debug("Send", "channel", chID, "conn", c, "msgBytes", msgBytes)
	for q, ctrl := range c.sendQueue.Lock() {
		ch, ok := q.channels[chID]
		if !ok {
			return errBadChannel{fmt.Errorf("unknown channel %X", chID)}
		}
		if err := ctrl.WaitUntil(ctx, func() bool { return !ch.queue.Full() }); err != nil {
			return err
		}
		ch.queue.PushBack(&msgBytes)
		ctrl.Updated()
	}
	return nil
}

// Recv .
func (c *MConnection) Recv(ctx context.Context) (ChannelID, []byte, error) {
	m, err := utils.Recv(ctx, c.recvCh)
	return m.channelID, m.payload, err
}

func (c *MConnection) recvPongSubscribe() utils.AtomicRecv[bool] {
	for recvPong := range c.recvPong.Lock() {
		return recvPong.Subscribe()
	}
	panic("unreachable")
}

func (c *MConnection) pingRoutine(ctx context.Context) error {
	for {
		// Send ping.
		for q, ctrl := range c.sendQueue.Lock() {
			q.ping = true
			ctrl.Updated()
		}
		// Wait for pong.
		if err := utils.WithTimeout(ctx, c.config.PongTimeout, func(ctx context.Context) error {
			_, err := c.recvPongSubscribe().Wait(ctx, func(gotPong bool) bool { return gotPong })
			return err
		}); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return errPongTimeout
		}
		for recvPong := range c.recvPong.Lock() {
			recvPong.Store(false)
		}
		// Sleep.
		if err := utils.Sleep(ctx, c.config.PingInterval); err != nil {
			return err
		}
	}
}

func (c *MConnection) statsRoutine(ctx context.Context) error {
	const updateStats = 2 * time.Second
	for {
		if err := utils.Sleep(ctx, updateStats); err != nil {
			return err
		}
		for q := range c.sendQueue.Lock() {
			for _, ch := range q.channels {
				// Exponential decay of stats.
				// TODO(gprusak): This is not atomic at all.
				ch.recentlySent.Store(uint64(float64(ch.recentlySent.Load()) * 0.8))
			}
		}
	}
}

// popSendQueue pops a message from the send queue.
// Returns nil,nil if the connection should be flushed.
func (c *MConnection) popSendQueue(ctx context.Context) (*p2p.Packet, error) {
	for q, ctrl := range c.sendQueue.Lock() {
		for {
			if q.ping {
				q.ping = false
				q.setFlush(time.Now())
				return &p2p.Packet{
					Sum: &p2p.Packet_PacketPing{
						PacketPing: &p2p.PacketPing{},
					},
				}, nil
			}
			if q.pong {
				q.pong = false
				q.setFlush(time.Now())
				return &p2p.Packet{
					Sum: &p2p.Packet_PacketPong{
						PacketPong: &p2p.PacketPong{},
					},
				}, nil
			}
			// Choose a channel to create a PacketMsg from.
			// The chosen channel will be the one whose recentlySent/priority is the least.
			leastRatio := float32(math.Inf(1))
			var leastChannel *sendChannel
			for _, channel := range q.channels {
				if channel.queue.Len() == 0 {
					continue
				}
				if ratio := channel.ratio(); ratio < leastRatio {
					leastRatio = ratio
					leastChannel = channel
				}
			}
			if leastChannel != nil {
				q.setFlush(time.Now().Add(c.config.FlushThrottle))
				msg := leastChannel.popMsg(c.config.MaxPacketMsgPayloadSize)
				ctrl.Updated()
				leastChannel.recentlySent.Add(uint64(len(msg.Data)))
				return &p2p.Packet{
					Sum: &p2p.Packet_PacketMsg{
						PacketMsg: msg,
					},
				}, nil
			}
			if err := utils.WithDeadline(ctx, q.flush, func(ctx context.Context) error {
				return ctrl.Wait(ctx)
			}); err != nil {
				if ctx.Err() == nil {
					// It is flush time!
					q.flush = utils.None[time.Time]()
					return nil, nil
				}
				return nil, err
			}
		}
	}
	panic("unreachable")
}

// sendRoutine polls for packets to send from channels.
func (c *MConnection) sendRoutine(ctx context.Context) (err error) {
	// TODO(gprusak): This doesn't make sense - TCP package is 1.5kB anyway (unless we match the encryption frame here)
	// In fact, buffering should be just moved to the encryption layer.
	const minWriteBufferSize = 65536
	maxPacketMsgSize := c.maxPacketMsgSize()
	limiter := rate.NewLimiter(c.config.getSendRateLimit(), max(maxPacketMsgSize, int(c.config.SendRate)))
	bufWriter := bufio.NewWriterSize(c.conn, minWriteBufferSize)
	protoWriter := protoio.NewDelimitedWriter(bufWriter)
	for {
		msg, err := c.popSendQueue(ctx)
		if err != nil {
			return fmt.Errorf("popSendQueue(): %w", err)
		}
		if msg != nil {
			n, err := protoWriter.WriteMsg(msg)
			if err != nil {
				return fmt.Errorf("protoWriter.WriteMsg(): %w", err)
			}
			if err := limiter.WaitN(ctx, n); err != nil {
				return err
			}
		} else {
			c.logger.Debug("Flush", "conn", c)
			if err := bufWriter.Flush(); err != nil {
				return fmt.Errorf("bufWriter.Flush(): %w", err)
			}
		}
	}
}

// recvRoutine receives messages and pushes them to recvCh.
// It also handles ping/pong messages.
func (c *MConnection) recvRoutine(ctx context.Context) (err error) {
	const readBufferSize = 1024
	maxPacketMsgSize := c.maxPacketMsgSize()
	limiter := rate.NewLimiter(c.config.getRecvRateLimit(), max(int(c.config.RecvRate), maxPacketMsgSize))
	bufReader := bufio.NewReaderSize(c.conn, readBufferSize)
	protoReader := protoio.NewDelimitedReader(bufReader, maxPacketMsgSize)
	channels := map[ChannelID]*recvChannel{}
	for q := range c.sendQueue.Lock() {
		for _, ch := range q.channels {
			channels[ch.desc.ID] = newRecvChannel(ch.desc)
		}
	}

	for {
		packet := &p2p.Packet{}
		n, err := protoReader.ReadMsg(packet)
		if err != nil {
			if !IsDisconnect(err) {
				err = errBadEncoding{err}
			}
			return fmt.Errorf("protoReader.ReadMsg(): %w", err)
		}
		if err := limiter.WaitN(ctx, n); err != nil {
			return err
		}
		switch p := packet.Sum.(type) {
		case *p2p.Packet_PacketPing:
			for q, ctrl := range c.sendQueue.Lock() {
				q.pong = true
				ctrl.Updated()
			}
		case *p2p.Packet_PacketPong:
			for recvPong := range c.recvPong.Lock() {
				recvPong.Store(true)
			}
		case *p2p.Packet_PacketMsg:
			channelID, castOk := utils.SafeCast[ChannelID](p.PacketMsg.ChannelID)
			ch, ok := channels[channelID]
			if !castOk || !ok {
				return errBadChannel{fmt.Errorf("unknown channel %X", p.PacketMsg.ChannelID)}
			}
			c.logger.Debug("Read PacketMsg", "conn", c, "packet", packet)
			msgBytes, err := ch.pushMsg(p.PacketMsg)
			if err != nil {
				return fmt.Errorf("recvPacketMsg(): %v", err)
			}
			if msgBytes != nil {
				c.logger.Debug("Received bytes", "chID", channelID, "msgBytes", msgBytes)
				if err := utils.Send(ctx, c.recvCh, mConnMessage{
					channelID: channelID,
					payload:   msgBytes,
				}); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("unknown message type %v", reflect.TypeOf(packet))
		}
	}
}

// maxPacketMsgSize returns a maximum size of PacketMsg
func (c *MConnection) maxPacketMsgSize() int {
	bz, err := proto.Marshal(&p2p.Packet{
		Sum: &p2p.Packet_PacketMsg{
			PacketMsg: &p2p.PacketMsg{
				ChannelID: 0x01,
				EOF:       true,
				Data:      make([]byte, c.config.MaxPacketMsgPayloadSize),
			},
		},
	})
	if err != nil {
		panic(err)
	}
	return len(bz)
}

type sendChannel struct {
	desc         ChannelDescriptor
	recentlySent atomic.Uint64 // Exponential moving average.
	queue        utils.RingBuf[*[]byte]
}

func (ch *sendChannel) ratio() float32 {
	return float32(ch.recentlySent.Load()) / float32(ch.desc.Priority)
}

// Creates a new PacketMsg to send.
// Not goroutine-safe
func (ch *sendChannel) popMsg(maxPayload int) *p2p.PacketMsg {
	payload := ch.queue.Get(0)
	packet := &p2p.PacketMsg{ChannelID: int32(ch.desc.ID)}
	if len(*payload) <= maxPayload {
		packet.EOF = true
		packet.Data = *ch.queue.PopFront()
	} else {
		packet.EOF = false
		packet.Data = (*payload)[:maxPayload]
		*payload = (*payload)[maxPayload:]
	}
	return packet
}

type recvChannel struct {
	desc ChannelDescriptor
	buf  []byte
}

func newRecvChannel(desc ChannelDescriptor) *recvChannel {
	return &recvChannel{
		desc: desc.withDefaults(),
		buf:  make([]byte, 0, desc.RecvBufferCapacity),
	}
}

// Handles incoming PacketMsgs. It returns a message bytes if message is
// complete, which is owned by the caller and will not be modified.
// Not goroutine-safe
func (ch *recvChannel) pushMsg(packet *p2p.PacketMsg) ([]byte, error) {
	if got, wantMax := len(ch.buf)+len(packet.Data), ch.desc.RecvMessageCapacity; got > wantMax {
		return nil, fmt.Errorf("received message exceeds available capacity: %v < %v", wantMax, got)
	}
	ch.buf = append(ch.buf, packet.Data...)
	if packet.EOF {
		msgBytes := ch.buf
		ch.buf = make([]byte, 0, ch.desc.RecvBufferCapacity)
		return msgBytes, nil
	}
	return nil, nil
}
