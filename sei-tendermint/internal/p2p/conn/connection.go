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
	"sync"
	"sync/atomic"
	"time"

	"github.com/gogo/protobuf/proto"

	"github.com/tendermint/tendermint/internal/libs/flowrate"
	"github.com/tendermint/tendermint/internal/libs/protoio"
	"github.com/tendermint/tendermint/internal/libs/timer"
	"github.com/tendermint/tendermint/libs/log"
	tmmath "github.com/tendermint/tendermint/libs/math"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/scope"
	tmp2p "github.com/tendermint/tendermint/proto/tendermint/p2p"
)

var errPongTimeout = errors.New("pong timeout")

type errBadEncoding struct{ error }
type errBadChannel struct{ error }

const (
	// mirrors MaxPacketMsgPayloadSize from config/config.go
	defaultMaxPacketMsgPayloadSize = 1400

	numBatchPacketMsgs = 10
	minReadBufferSize  = 1024
	minWriteBufferSize = 65536
	updateStats        = 2 * time.Second

	// some of these defaults are written in the user config
	// flushThrottle, sendRate, recvRate
	// TODO: remove values present in config
	defaultFlushThrottle = 100 * time.Millisecond

	defaultSendQueueCapacity   = 1
	defaultRecvBufferCapacity  = 4096
	defaultRecvMessageCapacity = 22020096      // 21MB
	defaultSendRate            = int64(512000) // 500KB/s
	defaultRecvRate            = int64(512000) // 500KB/s
	defaultSendTimeout         = 10 * time.Second
	defaultPingInterval        = 60 * time.Second
	defaultPongTimeout         = 90 * time.Second
)

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

There are two methods for sending messages:

	func (m MConnection) Send(chID byte, msgBytes []byte) bool {}

`Send(chID, msgBytes)` is a blocking call that waits until `msg` is
successfully queued for the channel with the given id byte `chID`, or until the
request times out.  The message `msg` is serialized using Protobuf.

Inbound message bytes are handled with an onReceive callback function.
*/
type MConnection struct {
	logger log.Logger

	conn          net.Conn
	bufConnReader *bufio.Reader
	bufConnWriter *bufio.Writer
	sendMonitor   *flowrate.Monitor
	recvMonitor   *flowrate.Monitor
	send          chan struct{}
	pong          chan struct{}
	channels      []*channel
	channelsIdx   map[ChannelID]*channel
	receiveCh     chan mConnMessage
	config        MConnConfig
	handle        *scope.GlobalHandle

	flushTimer *timer.ThrottleTimer // flush writes as necessary but throttled.
	pingTimer  *time.Ticker         // send pings periodically

	// close conn if pong is not received in pongTimeout
	lastMsgRecv struct {
		sync.Mutex
		at time.Time
	}

	chStatsTimer *time.Ticker // update channel stats periodically

	created time.Time // time of creation

	_maxPacketMsgSize int
}

// MConnConfig is a MConnection configuration.
type MConnConfig struct {
	SendRate int64 `mapstructure:"send_rate"`
	RecvRate int64 `mapstructure:"recv_rate"`

	// Maximum payload size
	MaxPacketMsgPayloadSize int `mapstructure:"max_packet_msg_payload_size"`

	// Interval to flush writes (throttled)
	FlushThrottle time.Duration `mapstructure:"flush_throttle"`

	// Interval to send pings
	PingInterval time.Duration `mapstructure:"ping_interval"`

	// Maximum wait time for pongs
	PongTimeout time.Duration `mapstructure:"pong_timeout"`

	// Process/Transport Start time
	StartTime time.Time `mapstructure:",omitempty"`
}

// DefaultMConnConfig returns the default config.
func DefaultMConnConfig() MConnConfig {
	return MConnConfig{
		SendRate:                defaultSendRate,
		RecvRate:                defaultRecvRate,
		MaxPacketMsgPayloadSize: defaultMaxPacketMsgPayloadSize,
		FlushThrottle:           defaultFlushThrottle,
		PingInterval:            defaultPingInterval,
		PongTimeout:             defaultPongTimeout,
		StartTime:               time.Now(),
	}
}

// NewMConnection wraps net.Conn and creates multiplex connection with a config
func SpawnMConnection(
	logger log.Logger,
	conn net.Conn,
	chDescs []*ChannelDescriptor,
	config MConnConfig,
) *MConnection {
	c := &MConnection{
		logger:        logger,
		conn:          conn,
		bufConnReader: bufio.NewReaderSize(conn, minReadBufferSize),
		bufConnWriter: bufio.NewWriterSize(conn, minWriteBufferSize),
		sendMonitor:   flowrate.New(config.StartTime, 0, 0),
		recvMonitor:   flowrate.New(config.StartTime, 0, 0),
		send:          make(chan struct{}, 1),
		pong:          make(chan struct{}, 1),
		receiveCh:     make(chan mConnMessage),
		config:        config,
		created:       time.Now(),
		flushTimer:    timer.NewThrottleTimer("flush", config.FlushThrottle),
		pingTimer:     time.NewTicker(config.PingInterval),
		chStatsTimer:  time.NewTicker(updateStats),
	}

	// Create channels
	var channelsIdx = map[ChannelID]*channel{}
	var channels = []*channel{}

	for _, desc := range chDescs {
		channel := newChannel(c, *desc)
		channelsIdx[channel.desc.ID] = channel
		channels = append(channels, channel)
	}
	c.channels = channels
	c.channelsIdx = channelsIdx

	// maxPacketMsgSize() is a bit heavy, so call just once
	c._maxPacketMsgSize = c.maxPacketMsgSize()

	c.handle = scope.SpawnGlobal(func(ctx context.Context) error {
		c.setRecvLastMsgAt(time.Now())
		return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
			s.Spawn(func() error { return c.sendRoutine(ctx) })
			s.Spawn(func() error { return c.recvRoutine(ctx) })
			<-ctx.Done()
			c.conn.Close()
			// Guarantees that an error is ALWAYS returned.
			return ctx.Err()
		})
	})
	return c
}

func (c *MConnection) setRecvLastMsgAt(t time.Time) {
	c.lastMsgRecv.Lock()
	defer c.lastMsgRecv.Unlock()
	c.lastMsgRecv.at = t
}

func (c *MConnection) getLastMessageAt() time.Time {
	c.lastMsgRecv.Lock()
	defer c.lastMsgRecv.Unlock()
	return c.lastMsgRecv.at
}

func (c *MConnection) Close() error {
	return c.handle.Close()
}

func (c *MConnection) String() string {
	return fmt.Sprintf("MConn{%v}", c.conn.RemoteAddr())
}

func (c *MConnection) flush() {
	c.logger.Debug("Flush", "conn", c)
	err := c.bufConnWriter.Flush()
	if err != nil {
		c.logger.Debug("MConnection flush failed", "err", err)
	}
}

// Catch panics, usually caused by remote disconnects.
func _recover(err *error) {
	if r := recover(); r != nil {
		*err = fmt.Errorf("recovered from panic: %v", r)
	}
}

// Queues a message to be sent to channel.
func (c *MConnection) Send(ctx context.Context, chID ChannelID, msgBytes []byte) error {
	c.logger.Debug("Send", "channel", chID, "conn", c, "msgBytes", msgBytes)

	// Send message to channel.
	channel, ok := c.channelsIdx[chID]
	if !ok {
		return errBadChannel{fmt.Errorf("Cannot send bytes, unknown channel %X", chID)}
	}

	if err := c.sendBytes(ctx, channel, msgBytes); err != nil {
		return fmt.Errorf("sendBytes(): %w", err)
	}
	// Wake up sendRoutine if necessary
	select {
	case c.send <- struct{}{}:
	default:
	}
	return nil
}

func (c *MConnection) Recv(ctx context.Context) (ChannelID, []byte, error) {
	// select is nondeterministic and the code currently requires operations on the closed
	// connection to ALWAYS fail immediately.
	if err := c.handle.Err(); err != nil {
		return 0, nil, err
	}
	select {
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	case <-c.handle.Done():
		return 0, nil, c.handle.Err()
	case m := <-c.receiveCh:
		return m.channelID, m.payload, nil
	}
}

// sendRoutine polls for packets to send from channels.
func (c *MConnection) sendRoutine(ctx context.Context) (err error) {
	defer _recover(&err)
	protoWriter := protoio.NewDelimitedWriter(c.bufConnWriter)
	pongTimeout := time.NewTicker(c.config.PongTimeout)
	for {
	SELECTION:
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.flushTimer.Ch:
			// NOTE: flushTimer.Set() must be called every time
			// something is written to .bufConnWriter.
			c.flush()
		case <-c.chStatsTimer.C:
			for _, channel := range c.channels {
				channel.updateStats()
			}
		case <-c.pingTimer.C:
			n, err := protoWriter.WriteMsg(mustWrapPacket(&tmp2p.PacketPing{}))
			if err != nil {
				return fmt.Errorf("Failed to send PacketPing: %w", err)
			}
			c.sendMonitor.Update(n)
			c.flush()
		case <-c.pong:
			n, err := protoWriter.WriteMsg(mustWrapPacket(&tmp2p.PacketPong{}))
			if err != nil {
				return fmt.Errorf("Failed to send PacketPong: %w", err)
			}
			c.sendMonitor.Update(n)
			c.flush()
		case <-pongTimeout.C:
			// the point of the pong timer is to check to
			// see if we've seen a message recently, so we
			// want to make sure that we escape this
			// select statement on an interval to ensure
			// that we avoid hanging on to dead
			// connections for too long.
			break SELECTION
		case <-c.send:
			// Send some PacketMsgs
			eof, err := c.sendSomePacketMsgs()
			if err != nil {
				return fmt.Errorf("sendSomePacketMsgs(): %w", err)
			}
			if !eof {
				// Keep sendRoutine awake.
				select {
				case c.send <- struct{}{}:
				default:
				}
			}
		}

		if time.Since(c.getLastMessageAt()) > c.config.PongTimeout {
			return errPongTimeout
		}
	}
}

// Returns true if messages from channels were exhausted.
// Blocks in accordance to .sendMonitor throttling.
func (c *MConnection) sendSomePacketMsgs() (bool, error) {
	// Block until .sendMonitor says we can write.
	// Once we're ready we send more than we asked for,
	// but amortized it should even out.
	c.sendMonitor.Limit(c._maxPacketMsgSize, c.config.SendRate, true)

	// Now send some PacketMsgs.
	for i := 0; i < numBatchPacketMsgs; i++ {
		if done, err := c.sendPacketMsg(); done || err != nil {
			return done, err
		}
	}
	return false, nil
}

// Returns true if messages from channels were exhausted.
func (c *MConnection) sendPacketMsg() (bool, error) {
	// Choose a channel to create a PacketMsg from.
	// The chosen channel will be the one whose recentlySent/priority is the least.
	var leastRatio float32 = math.MaxFloat32
	var leastChannel *channel
	for _, channel := range c.channels {
		// If nothing to send, skip this channel
		if !channel.isSendPending() {
			continue
		}
		// Get ratio, and keep track of lowest ratio.
		ratio := float32(channel.recentlySent) / float32(channel.desc.Priority)
		if ratio < leastRatio {
			leastRatio = ratio
			leastChannel = channel
		}
	}

	// Nothing to send?
	if leastChannel == nil {
		return true, nil
	}

	// Make & send a PacketMsg from this channel
	_n, err := leastChannel.writePacketMsgTo(c.bufConnWriter)
	if err != nil {
		return false, err
	}
	c.sendMonitor.Update(_n)
	c.flushTimer.Set()
	return false, nil
}

// recvRoutine reads PacketMsgs and reconstructs the message using the channels' "recving" buffer.
// After a whole message has been assembled, it's pushed to onReceive().
// Blocks depending on how the connection is throttled.
// Otherwise, it never blocks.
func (c *MConnection) recvRoutine(ctx context.Context) (err error) {
	defer _recover(&err)

	protoReader := protoio.NewDelimitedReader(c.bufConnReader, c._maxPacketMsgSize)
	for ctx.Err() == nil {
		// Block until .recvMonitor says we can read.
		c.recvMonitor.Limit(c._maxPacketMsgSize, c.config.RecvRate, true)

		// Read packet type
		var packet tmp2p.Packet

		_n, err := protoReader.ReadMsg(&packet)
		c.recvMonitor.Update(_n)
		if err != nil {
			return errBadEncoding{err}
		}

		// record for pong/heartbeat
		c.setRecvLastMsgAt(time.Now())

		// Read more depending on packet type.
		switch pkt := packet.Sum.(type) {
		case *tmp2p.Packet_PacketPing:
			// TODO: prevent abuse, as they cause flush()'s.
			// https://github.com/tendermint/tendermint/issues/1190
			select {
			case c.pong <- struct{}{}:
			default:
				// never block
			}
		case *tmp2p.Packet_PacketPong:
			// do nothing, we updated the "last message
			// received" timestamp above, so we can ignore
			// this message
		case *tmp2p.Packet_PacketMsg:
			channelID := ChannelID(pkt.PacketMsg.ChannelID)
			channel, ok := c.channelsIdx[channelID]
			if pkt.PacketMsg.ChannelID < 0 || pkt.PacketMsg.ChannelID > math.MaxUint8 || !ok || channel == nil {
				return errBadChannel{fmt.Errorf("unknown channel %X", pkt.PacketMsg.ChannelID)}
			}

			msgBytes, err := channel.recvPacketMsg(*pkt.PacketMsg)
			if err != nil {
				return err
			}
			if msgBytes != nil {
				c.logger.Debug("Received bytes", "chID", channelID, "msgBytes", msgBytes)
				if err := utils.Send(ctx, c.receiveCh, mConnMessage{channelID: channelID, payload: msgBytes}); err != nil {
					return nil
				}
			}
		default:
			return errBadEncoding{fmt.Errorf("unknown message type %v", reflect.TypeOf(packet))}
		}
	}
	return nil
}

// maxPacketMsgSize returns a maximum size of PacketMsg
func (c *MConnection) maxPacketMsgSize() int {
	bz, err := proto.Marshal(mustWrapPacket(&tmp2p.PacketMsg{
		ChannelID: 0x01,
		EOF:       true,
		Data:      make([]byte, c.config.MaxPacketMsgPayloadSize),
	}))
	if err != nil {
		panic(err)
	}
	return len(bz)
}

type ChannelStatus struct {
	ID                byte
	SendQueueCapacity int
	SendQueueSize     int
	Priority          int
	RecentlySent      int64
}

// -----------------------------------------------------------------------------
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

func (chDesc ChannelDescriptor) FillDefaults() (filled ChannelDescriptor) {
	if chDesc.Priority <= 0 {
		chDesc.Priority = 1
	}
	if chDesc.SendQueueCapacity == 0 {
		chDesc.SendQueueCapacity = defaultSendQueueCapacity
	}
	if chDesc.RecvBufferCapacity == 0 {
		chDesc.RecvBufferCapacity = defaultRecvBufferCapacity
	}
	if chDesc.RecvMessageCapacity == 0 {
		chDesc.RecvMessageCapacity = defaultRecvMessageCapacity
	}
	filled = chDesc
	return
}

// NOTE: not goroutine-safe.
type channel struct {
	// Exponential moving average.
	// This field must be accessed atomically.
	// It is first in the struct to ensure correct alignment.
	// See https://github.com/tendermint/tendermint/issues/7000.
	recentlySent int64

	conn      *MConnection
	desc      ChannelDescriptor
	sendQueue chan []byte
	recving   []byte
	sending   []byte

	maxPacketMsgPayloadSize int

	logger log.Logger
}

func newChannel(conn *MConnection, desc ChannelDescriptor) *channel {
	desc = desc.FillDefaults()
	return &channel{
		conn:                    conn,
		desc:                    desc,
		sendQueue:               make(chan []byte, desc.SendQueueCapacity),
		recving:                 make([]byte, 0, desc.RecvBufferCapacity),
		maxPacketMsgPayloadSize: conn.config.MaxPacketMsgPayloadSize,
		logger:                  conn.logger,
	}
}

// Queues message to send to this channel.
// Goroutine-safe
func (c *MConnection) sendBytes(ctx context.Context, ch *channel, bytes []byte) error {
	// select is nondeterministic and the code currently requires operations on the closed
	// connection to ALWAYS fail immediately.
	if err := c.handle.Err(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.handle.Done():
		return c.handle.Err()
	case ch.sendQueue <- bytes:
		return nil
	}
}

// Returns true if any PacketMsgs are pending to be sent.
// Call before calling nextPacketMsg()
// Goroutine-safe
func (ch *channel) isSendPending() bool {
	if len(ch.sending) == 0 {
		if len(ch.sendQueue) == 0 {
			return false
		}
		ch.sending = <-ch.sendQueue
	}
	return true
}

// Creates a new PacketMsg to send.
// Not goroutine-safe
func (ch *channel) nextPacketMsg() tmp2p.PacketMsg {
	packet := tmp2p.PacketMsg{ChannelID: int32(ch.desc.ID)}
	maxSize := ch.maxPacketMsgPayloadSize
	packet.Data = ch.sending[:tmmath.MinInt(maxSize, len(ch.sending))]
	if len(ch.sending) <= maxSize {
		packet.EOF = true
		ch.sending = nil
	} else {
		packet.EOF = false
		ch.sending = ch.sending[tmmath.MinInt(maxSize, len(ch.sending)):]
	}
	return packet
}

// Writes next PacketMsg to w and updates c.recentlySent.
// Not goroutine-safe
func (ch *channel) writePacketMsgTo(w io.Writer) (n int, err error) {
	packet := ch.nextPacketMsg()
	n, err = protoio.NewDelimitedWriter(w).WriteMsg(mustWrapPacket(&packet))
	atomic.AddInt64(&ch.recentlySent, int64(n))
	return
}

// Handles incoming PacketMsgs. It returns a message bytes if message is
// complete, which is owned by the caller and will not be modified.
// Not goroutine-safe
func (ch *channel) recvPacketMsg(packet tmp2p.PacketMsg) ([]byte, error) {
	ch.logger.Debug("Read PacketMsg", "conn", ch.conn, "packet", packet)
	var recvCap, recvReceived = ch.desc.RecvMessageCapacity, len(ch.recving) + len(packet.Data)
	if recvCap < recvReceived {
		return nil, fmt.Errorf("received message exceeds available capacity: %v < %v", recvCap, recvReceived)
	}
	ch.recving = append(ch.recving, packet.Data...)
	if packet.EOF {
		msgBytes := ch.recving
		ch.recving = make([]byte, 0, ch.desc.RecvBufferCapacity)
		return msgBytes, nil
	}
	return nil, nil
}

// Call this periodically to update stats for throttling purposes.
// Not goroutine-safe
func (ch *channel) updateStats() {
	// Exponential decay of stats.
	// TODO: optimize.
	atomic.StoreInt64(&ch.recentlySent, int64(float64(atomic.LoadInt64(&ch.recentlySent))*0.8))
}

//----------------------------------------
// Packet

// mustWrapPacket takes a packet kind (oneof) and wraps it in a tmp2p.Packet message.
func mustWrapPacket(pb proto.Message) *tmp2p.Packet {
	var msg tmp2p.Packet

	switch pb := pb.(type) {
	case *tmp2p.Packet: // already a packet
		msg = *pb
	case *tmp2p.PacketPing:
		msg = tmp2p.Packet{
			Sum: &tmp2p.Packet_PacketPing{
				PacketPing: pb,
			},
		}
	case *tmp2p.PacketPong:
		msg = tmp2p.Packet{
			Sum: &tmp2p.Packet_PacketPong{
				PacketPong: pb,
			},
		}
	case *tmp2p.PacketMsg:
		msg = tmp2p.Packet{
			Sum: &tmp2p.Packet_PacketMsg{
				PacketMsg: pb,
			},
		}
	default:
		panic(fmt.Errorf("unknown packet type %T", pb))
	}

	return &msg
}
