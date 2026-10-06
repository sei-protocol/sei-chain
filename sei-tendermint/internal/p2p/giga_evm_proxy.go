package p2p

// The EVM proxy forwards EVM RPC requests (eth_sendRawTransaction and pending
// eth_getTransactionCount) to the committee member that owns the sender's
// shard. Each owner gets its own keep-alive HTTP/1.1 connection pool, a
// bounded per-request timeout, and per-owner metrics.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	ethrpc "github.com/ethereum/go-ethereum/rpc"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

// DefaultEvmProxyMaxConnsPerOwner is the connection cap per shard owner when
// GigaRouterCommonConfig.EvmProxyMaxConnsPerOwner is absent.
const DefaultEvmProxyMaxConnsPerOwner = 512

// maxEvmProxyConnsPerOwner caps GigaRouterCommonConfig.EvmProxyMaxConnsPerOwner.
const maxEvmProxyConnsPerOwner = 4096

// DefaultEvmProxyTimeout bounds one forward when
// GigaRouterCommonConfig.EvmProxyTimeout is absent.
const DefaultEvmProxyTimeout = 10 * time.Second

const (
	evmProxyDialTimeout         = 5 * time.Second
	evmProxyTCPKeepAlive        = 30 * time.Second
	evmProxyTLSHandshakeTimeout = 5 * time.Second
	// evmProxyIdleConnTimeout closes an idle connection before an NLB or NAT
	// gateway drops it silently (350s on AWS).
	evmProxyIdleConnTimeout = 90 * time.Second
	// evmProxyMaxResponseBytes bounds the response bytes kept to classify a
	// forward and drained on close so that the connection is reused.
	evmProxyMaxResponseBytes = 64 << 10
)

// Values of the evm_proxy_requests outcome label.
const (
	evmProxyOutcomeOK             = "ok"
	evmProxyOutcomeRPCError       = "rpc_error"
	evmProxyOutcomeTimeout        = "timeout"
	evmProxyOutcomeTransportError = "transport_error"
	evmProxyOutcomeCanceled       = "canceled"
)

// ErrEvmProxyTimeout is returned when a shard owner does not answer a
// forwarded request within the configured timeout. The caller can retry.
var ErrEvmProxyTimeout = errors.New("evm proxy: shard owner did not answer in time")

// evmProxyConfig is the resolved connection policy of one shard owner proxy.
type evmProxyConfig struct {
	maxConns int
	timeout  time.Duration
}

func (c *GigaRouterCommonConfig) evmProxyConfig() evmProxyConfig {
	return evmProxyConfig{
		maxConns: int(c.EvmProxyMaxConnsPerOwner.Or(DefaultEvmProxyMaxConnsPerOwner)), //nolint:gosec // BuildDataState bounds it.
		timeout:  c.EvmProxyTimeout.Or(DefaultEvmProxyTimeout),
	}
}

// validateEvmProxy checks the optional EVM proxy settings.
func (c *GigaRouterCommonConfig) validateEvmProxy() error {
	if v, ok := c.EvmProxyMaxConnsPerOwner.Get(); ok && (v == 0 || v > maxEvmProxyConnsPerOwner) {
		return fmt.Errorf("GigaRouterCommonConfig.EvmProxyMaxConnsPerOwner = %v, want 1..%v", v, maxEvmProxyConnsPerOwner)
	}
	if v, ok := c.EvmProxyTimeout.Get(); ok && v <= 0 {
		return fmt.Errorf("GigaRouterCommonConfig.EvmProxyTimeout = %v, want > 0", v)
	}
	return nil
}

// evmProxyTransport is the http.RoundTripper of one shard owner's EVM RPC
// client. It bounds each request with cfg.timeout and records its outcome.
type evmProxyTransport struct {
	owner      string
	base       *http.Transport
	timeoutErr error
	cfg        evmProxyConfig
	metrics    *Metrics
}

func newEvmProxyTransport(owner atypes.PublicKey, cfg evmProxyConfig, metrics *Metrics) *evmProxyTransport {
	t := &evmProxyTransport{
		owner:      owner.String(),
		timeoutErr: fmt.Errorf("%w (%v); retry the request", ErrEvmProxyTimeout, cfg.timeout),
		cfg:        cfg,
		metrics:    metrics,
	}
	dialer := &net.Dialer{Timeout: evmProxyDialTimeout, KeepAlive: evmProxyTCPKeepAlive}
	t.base = &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := dialer.DialContext(ctx, network, addr)
			if err == nil {
				t.metrics.evmProxyNewConnsAt(t.owner).Add(1)
			}
			return conn, err
		},
		TLSHandshakeTimeout: evmProxyTLSHandshakeTimeout,
		MaxIdleConns:        cfg.maxConns,
		MaxIdleConnsPerHost: cfg.maxConns,
		MaxConnsPerHost:     cfg.maxConns,
		IdleConnTimeout:     evmProxyIdleConnTimeout,
	}
	return t
}

// dialEvmProxy returns an EVM RPC client of the shard owner at url over a
// dedicated transport. The caller closes both.
func dialEvmProxy(ctx context.Context, owner atypes.PublicKey, url string, cfg evmProxyConfig, metrics *Metrics) (*ethrpc.Client, *evmProxyTransport, error) {
	t := newEvmProxyTransport(owner, cfg, metrics)
	client, err := ethrpc.DialOptions(ctx, url, ethrpc.WithHTTPClient(&http.Client{Transport: t}))
	if err != nil {
		t.close()
		return nil, nil, err
	}
	return client, t, nil
}

// close releases the idle connections. In-flight requests finish normally.
func (t *evmProxyTransport) close() { t.base.CloseIdleConnections() }

// RoundTrip sends req with a deadline of cfg.timeout. The deadline also
// covers the wait for a free connection and the read of the response body.
func (t *evmProxyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	parent := req.Context()
	ctx, cancel := context.WithTimeoutCause(parent, t.cfg.timeout, t.timeoutErr)
	resp, err := t.base.RoundTrip(req.WithContext(ctx))
	if err != nil {
		outcome := failureOutcome(parent, ctx)
		err = t.timeoutOr(ctx, err)
		cancel()
		t.observe(start, outcome)
		return nil, err
	}
	resp.Body = &evmProxyBody{
		ReadCloser: resp.Body,
		t:          t,
		parent:     parent,
		ctx:        ctx,
		cancel:     cancel,
		start:      start,
		success:    resp.StatusCode >= 200 && resp.StatusCode < 300,
	}
	return resp, nil
}

// timeoutOr returns the timeout error if the transport deadline ended the
// request, and err otherwise.
func (t *evmProxyTransport) timeoutOr(ctx context.Context, err error) error {
	if context.Cause(ctx) == t.timeoutErr {
		return t.timeoutErr
	}
	return err
}

func (t *evmProxyTransport) observe(start time.Time, outcome string) {
	t.metrics.evmProxyRequestSecondsAt(t.owner).Observe(time.Since(start).Seconds())
	t.metrics.evmProxyRequestsAt(t.owner, outcome).Add(1)
}

// failureOutcome classifies a failed request. Call it before ctx is canceled.
func failureOutcome(parent, ctx context.Context) string {
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return evmProxyOutcomeTimeout
	case parent.Err() != nil:
		return evmProxyOutcomeCanceled
	default:
		return evmProxyOutcomeTransportError
	}
}

// evmProxyBody keeps the head of a response to classify the forward, and
// records the outcome when it is closed.
type evmProxyBody struct {
	io.ReadCloser
	t       *evmProxyTransport
	parent  context.Context
	ctx     context.Context
	cancel  context.CancelFunc
	start   time.Time
	success bool
	head    []byte
	failure string
	closed  bool
}

func (b *evmProxyBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if room := evmProxyMaxResponseBytes - len(b.head); room > 0 {
		b.head = append(b.head, p[:min(n, room)]...)
	}
	if err != nil && !errors.Is(err, io.EOF) {
		if b.failure == "" {
			b.failure = failureOutcome(b.parent, b.ctx)
		}
		err = b.t.timeoutOr(b.ctx, err)
	}
	return n, err
}

// Close drains the rest of the response so that the transport can reuse the
// connection, then records the outcome.
func (b *evmProxyBody) Close() error {
	if b.closed {
		return nil
	}
	b.closed = true
	_, _ = io.Copy(io.Discard, io.LimitReader(b, evmProxyMaxResponseBytes))
	err := b.ReadCloser.Close()
	b.cancel()
	b.t.observe(b.start, b.outcome())
	return err
}

func (b *evmProxyBody) outcome() string {
	switch {
	case b.failure != "":
		return b.failure
	case !b.success:
		return evmProxyOutcomeTransportError
	case hasJSONRPCError(b.head):
		return evmProxyOutcomeRPCError
	default:
		return evmProxyOutcomeOK
	}
}

// hasJSONRPCError reports whether body is a single JSON-RPC response with an
// error. A batch or a truncated response counts as no error.
func hasJSONRPCError(body []byte) bool {
	var msg struct {
		Error json.RawMessage `json:"error"`
	}
	return json.Unmarshal(body, &msg) == nil && len(msg.Error) > 0 && string(msg.Error) != "null"
}
