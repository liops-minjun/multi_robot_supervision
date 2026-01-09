package grpc

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
)

// ============================================================
// QUIC-based gRPC Transport Implementation
// ============================================================

// QUICListener implements net.Listener for gRPC over QUIC
type QUICListener struct {
	quicListener *quic.Listener
	connChan     chan net.Conn
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
}

// NewQUICListener creates a QUIC listener that wraps connections as net.Conn
func NewQUICListener(addr string, tlsConfig *tls.Config, quicConfig *quic.Config) (*QUICListener, error) {
	// Ensure TLS config has NextProtos for gRPC
	if tlsConfig.NextProtos == nil {
		tlsConfig.NextProtos = []string{"grpc-quic", "h3"}
	}

	listener, err := quic.ListenAddr(addr, tlsConfig, quicConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create QUIC listener: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	ql := &QUICListener{
		quicListener: listener,
		connChan:     make(chan net.Conn, 100),
		ctx:          ctx,
		cancel:       cancel,
	}

	// Start accepting connections
	ql.wg.Add(1)
	go ql.acceptLoop()

	return ql, nil
}

func (l *QUICListener) acceptLoop() {
	defer l.wg.Done()

	for {
		select {
		case <-l.ctx.Done():
			return
		default:
			conn, err := l.quicListener.Accept(l.ctx)
			if err != nil {
				if l.ctx.Err() != nil {
					return
				}
				continue
			}

			// Wrap QUIC connection as net.Conn via stream
			l.wg.Add(1)
			go l.handleConnection(conn)
		}
	}
}

func (l *QUICListener) handleConnection(conn quic.Connection) {
	defer l.wg.Done()

	for {
		select {
		case <-l.ctx.Done():
			return
		case <-conn.Context().Done():
			return
		default:
			// Accept a bidirectional stream for each gRPC call
			stream, err := conn.AcceptStream(l.ctx)
			if err != nil {
				if l.ctx.Err() != nil || conn.Context().Err() != nil {
					return
				}
				continue
			}

			// Wrap stream as net.Conn
			wrappedConn := &QUICStreamConn{
				stream:     stream,
				quicConn:   conn,
				localAddr:  conn.LocalAddr(),
				remoteAddr: conn.RemoteAddr(),
			}

			select {
			case l.connChan <- wrappedConn:
			case <-l.ctx.Done():
				stream.Close()
				return
			}
		}
	}
}

// Accept implements net.Listener.Accept
func (l *QUICListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.connChan:
		return conn, nil
	case <-l.ctx.Done():
		return nil, l.ctx.Err()
	}
}

// Close implements net.Listener.Close
func (l *QUICListener) Close() error {
	l.cancel()
	err := l.quicListener.Close()
	l.wg.Wait()
	return err
}

// Addr implements net.Listener.Addr
func (l *QUICListener) Addr() net.Addr {
	return l.quicListener.Addr()
}

// ============================================================
// QUIC Stream Wrapper as net.Conn
// ============================================================

// QUICStreamConn wraps a QUIC stream to implement net.Conn
type QUICStreamConn struct {
	stream     quic.Stream
	quicConn   quic.Connection
	localAddr  net.Addr
	remoteAddr net.Addr
}

func (c *QUICStreamConn) Read(b []byte) (int, error) {
	return c.stream.Read(b)
}

func (c *QUICStreamConn) Write(b []byte) (int, error) {
	return c.stream.Write(b)
}

func (c *QUICStreamConn) Close() error {
	return c.stream.Close()
}

func (c *QUICStreamConn) LocalAddr() net.Addr {
	return c.localAddr
}

func (c *QUICStreamConn) RemoteAddr() net.Addr {
	return c.remoteAddr
}

func (c *QUICStreamConn) SetDeadline(t time.Time) error {
	if err := c.stream.SetReadDeadline(t); err != nil {
		return err
	}
	return c.stream.SetWriteDeadline(t)
}

func (c *QUICStreamConn) SetReadDeadline(t time.Time) error {
	return c.stream.SetReadDeadline(t)
}

func (c *QUICStreamConn) SetWriteDeadline(t time.Time) error {
	return c.stream.SetWriteDeadline(t)
}

// ============================================================
// QUIC Client Dialer for gRPC
// ============================================================

// QUICDialer provides gRPC client connections over QUIC
type QUICDialer struct {
	tlsConfig  *tls.Config
	quicConfig *quic.Config
	mu         sync.Mutex
	conns      map[string]quic.Connection
}

// NewQUICDialer creates a new QUIC dialer for gRPC clients
func NewQUICDialer(tlsConfig *tls.Config, quicConfig *quic.Config) *QUICDialer {
	if tlsConfig.NextProtos == nil {
		tlsConfig.NextProtos = []string{"grpc-quic", "h3"}
	}

	return &QUICDialer{
		tlsConfig:  tlsConfig,
		quicConfig: quicConfig,
		conns:      make(map[string]quic.Connection),
	}
}

// DialContext establishes a QUIC connection and returns a stream as net.Conn
func (d *QUICDialer) DialContext(ctx context.Context, addr string) (net.Conn, error) {
	d.mu.Lock()
	conn, exists := d.conns[addr]
	d.mu.Unlock()

	// Reuse existing connection if available
	if exists && conn.Context().Err() == nil {
		stream, err := conn.OpenStreamSync(ctx)
		if err == nil {
			return &QUICStreamConn{
				stream:     stream,
				quicConn:   conn,
				localAddr:  conn.LocalAddr(),
				remoteAddr: conn.RemoteAddr(),
			}, nil
		}
		// Connection is stale, remove it
		d.mu.Lock()
		delete(d.conns, addr)
		d.mu.Unlock()
	}

	// Create new QUIC connection
	newConn, err := quic.DialAddr(ctx, addr, d.tlsConfig, d.quicConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to dial QUIC: %w", err)
	}

	// Cache connection
	d.mu.Lock()
	d.conns[addr] = newConn
	d.mu.Unlock()

	// Open stream
	stream, err := newConn.OpenStreamSync(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to open stream: %w", err)
	}

	return &QUICStreamConn{
		stream:     stream,
		quicConn:   newConn,
		localAddr:  newConn.LocalAddr(),
		remoteAddr: newConn.RemoteAddr(),
	}, nil
}

// Close closes all cached connections
func (d *QUICDialer) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	var firstErr error
	for addr, conn := range d.conns {
		if err := conn.CloseWithError(0, "dialer closed"); err != nil && firstErr == nil {
			firstErr = err
		}
		delete(d.conns, addr)
	}
	return firstErr
}

// ============================================================
// Multiplexed QUIC Connection for Persistent Streams
// ============================================================

// MultiplexedQUICConn manages multiple gRPC streams over a single QUIC connection
type MultiplexedQUICConn struct {
	conn       quic.Connection
	mu         sync.Mutex
	streams    map[uint64]quic.Stream
	closeChan  chan struct{}
	onNewConn  func(net.Conn)
}

// NewMultiplexedQUICConn wraps a QUIC connection for multiplexed gRPC
func NewMultiplexedQUICConn(conn quic.Connection, onNewConn func(net.Conn)) *MultiplexedQUICConn {
	m := &MultiplexedQUICConn{
		conn:      conn,
		streams:   make(map[uint64]quic.Stream),
		closeChan: make(chan struct{}),
		onNewConn: onNewConn,
	}

	go m.acceptStreams()
	return m
}

func (m *MultiplexedQUICConn) acceptStreams() {
	ctx := m.conn.Context()

	for {
		select {
		case <-m.closeChan:
			return
		case <-ctx.Done():
			return
		default:
			stream, err := m.conn.AcceptStream(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				continue
			}

			m.mu.Lock()
			m.streams[uint64(stream.StreamID())] = stream
			m.mu.Unlock()

			// Notify callback
			if m.onNewConn != nil {
				wrappedConn := &QUICStreamConn{
					stream:     stream,
					quicConn:   m.conn,
					localAddr:  m.conn.LocalAddr(),
					remoteAddr: m.conn.RemoteAddr(),
				}
				m.onNewConn(wrappedConn)
			}
		}
	}
}

// OpenStream opens a new bidirectional stream
func (m *MultiplexedQUICConn) OpenStream(ctx context.Context) (net.Conn, error) {
	stream, err := m.conn.OpenStreamSync(ctx)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.streams[uint64(stream.StreamID())] = stream
	m.mu.Unlock()

	return &QUICStreamConn{
		stream:     stream,
		quicConn:   m.conn,
		localAddr:  m.conn.LocalAddr(),
		remoteAddr: m.conn.RemoteAddr(),
	}, nil
}

// Close closes all streams and the connection
func (m *MultiplexedQUICConn) Close() error {
	close(m.closeChan)

	m.mu.Lock()
	for _, stream := range m.streams {
		stream.Close()
	}
	m.streams = make(map[uint64]quic.Stream)
	m.mu.Unlock()

	return m.conn.CloseWithError(0, "connection closed")
}

// RemoteAddr returns the remote address
func (m *MultiplexedQUICConn) RemoteAddr() net.Addr {
	return m.conn.RemoteAddr()
}

// LocalAddr returns the local address
func (m *MultiplexedQUICConn) LocalAddr() net.Addr {
	return m.conn.LocalAddr()
}

// ============================================================
// Connection Migration Support
// ============================================================

// MigratableConn wraps a QUIC connection with migration support
type MigratableConn struct {
	*QUICStreamConn
	migrationHandler func(oldAddr, newAddr net.Addr)
}

// EnableMigrationNotification sets up connection migration notification
func (c *MigratableConn) EnableMigrationNotification(handler func(oldAddr, newAddr net.Addr)) {
	c.migrationHandler = handler

	// Note: quic-go handles migration automatically
	// This is for application-level notification
	go func() {
		initialAddr := c.quicConn.RemoteAddr()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-c.quicConn.Context().Done():
				return
			case <-ticker.C:
				currentAddr := c.quicConn.RemoteAddr()
				if initialAddr.String() != currentAddr.String() {
					if c.migrationHandler != nil {
						c.migrationHandler(initialAddr, currentAddr)
					}
					initialAddr = currentAddr
				}
			}
		}
	}()
}

// ============================================================
// 0-RTT Data Support
// ============================================================

// QUICEarlyConn wraps an early (0-RTT) QUIC connection
type QUICEarlyConn struct {
	conn quic.EarlyConnection
}

// Accept0RTTData reads 0-RTT data if available
func (c *QUICEarlyConn) Accept0RTTData() (io.Reader, error) {
	stream, err := c.conn.AcceptStream(context.Background())
	if err != nil {
		return nil, err
	}
	return stream, nil
}

// WaitForHandshake blocks until the TLS handshake completes
func (c *QUICEarlyConn) WaitForHandshake(ctx context.Context) error {
	select {
	case <-c.conn.HandshakeComplete():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
