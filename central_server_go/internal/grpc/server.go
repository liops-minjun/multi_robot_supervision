package grpc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"central_server_go/internal/config"
	"central_server_go/internal/state"

	"github.com/quic-go/quic-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
)

// ============================================================
// gRPC Server with QUIC Transport
// ============================================================

// TransportMode defines the transport protocol
type TransportMode int

const (
	TransportTCP  TransportMode = iota // Standard TCP (fallback)
	TransportQUIC                      // QUIC only
	TransportDual                      // Both TCP and QUIC
)

// Server represents the gRPC server with QUIC transport
type Server struct {
	cfg          *config.Config
	grpcServer   *grpc.Server
	quicServer   *grpc.Server // Separate server for QUIC
	stateManager *state.GlobalStateManager

	// Listeners
	tcpListener  net.Listener
	quicListener *QUICListener

	// Transport mode
	transportMode TransportMode

	// Agent stream management
	agentStreams map[string]*AgentStream
	streamMu     sync.RWMutex

	// Server state
	running bool
	mu      sync.Mutex
}

// AgentStream represents an active agent connection
type AgentStream struct {
	AgentID       string
	Stream        interface{} // FleetControl_CommandStreamServer
	SendChan      chan interface{}
	Done          chan struct{}
	TransportType string // "tcp" or "quic"
	RemoteAddr    net.Addr
	ConnectedAt   time.Time
}

// NewServer creates a new gRPC server with QUIC support
func NewServer(cfg *config.Config, stateManager *state.GlobalStateManager) (*Server, error) {
	// Configure TLS (required for QUIC)
	tlsConfig, err := loadTLSConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to load TLS config: %w", err)
	}

	// Determine transport mode
	transportMode := TransportDual
	if cfg.QUIC.QUICOnly {
		transportMode = TransportQUIC
	}

	// gRPC server options
	opts := buildGRPCServerOptions(cfg, tlsConfig)

	server := &Server{
		cfg:           cfg,
		stateManager:  stateManager,
		agentStreams:  make(map[string]*AgentStream),
		transportMode: transportMode,
	}

	// Create gRPC servers
	server.grpcServer = grpc.NewServer(opts...)

	// Separate QUIC server (same options but different instance for isolation)
	if transportMode != TransportTCP {
		server.quicServer = grpc.NewServer(opts...)
	}

	return server, nil
}

// buildGRPCServerOptions creates gRPC server options
func buildGRPCServerOptions(cfg *config.Config, tlsConfig *tls.Config) []grpc.ServerOption {
	opts := []grpc.ServerOption{
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    cfg.QUIC.KeepalivePeriod,
			Timeout: cfg.QUIC.MaxIdleTimeout,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             5 * time.Second,
			PermitWithoutStream: true,
		}),
		// Max message size for large behavior trees
		grpc.MaxRecvMsgSize(16 * 1024 * 1024), // 16MB
		grpc.MaxSendMsgSize(16 * 1024 * 1024),
	}

	// Add TLS credentials for TCP (QUIC handles TLS internally)
	if tlsConfig != nil {
		opts = append(opts, grpc.Creds(credentials.NewTLS(tlsConfig)))
	}

	return opts
}

// loadTLSConfig loads TLS configuration for mTLS
func loadTLSConfig(cfg *config.Config) (*tls.Config, error) {
	if !cfg.TLS.Enabled {
		return nil, nil
	}

	// Load server certificate
	serverCert, err := tls.LoadX509KeyPair(cfg.TLS.ServerCert, cfg.TLS.ServerKey)
	if err != nil {
		return nil, fmt.Errorf("failed to load server certificate: %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		MinVersion:   tls.VersionTLS13, // TLS 1.3 for QUIC
		NextProtos:   []string{"grpc-quic", "h3", "h2"}, // ALPN protocols
	}

	// Load CA certificate for client verification (mTLS)
	if cfg.TLS.RequireClientCert && cfg.TLS.CACert != "" {
		caCert, err := os.ReadFile(cfg.TLS.CACert)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate: %w", err)
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}

		tlsConfig.ClientCAs = caCertPool
		tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
	}

	return tlsConfig, nil
}

// RegisterService registers a gRPC service with both servers
func (s *Server) RegisterService(desc *grpc.ServiceDesc, impl interface{}) {
	s.grpcServer.RegisterService(desc, impl)
	if s.quicServer != nil {
		s.quicServer.RegisterService(desc, impl)
	}
}

// GetGRPCServer returns the TCP gRPC server
func (s *Server) GetGRPCServer() *grpc.Server {
	return s.grpcServer
}

// GetQUICServer returns the QUIC gRPC server
func (s *Server) GetQUICServer() *grpc.Server {
	return s.quicServer
}

// Start starts the gRPC server(s)
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("server already running")
	}
	s.running = true
	s.mu.Unlock()

	errChan := make(chan error, 2)

	// Start TCP listener (if not QUIC-only)
	if s.transportMode != TransportQUIC {
		go func() {
			if err := s.startTCPServer(ctx); err != nil {
				errChan <- fmt.Errorf("TCP server: %w", err)
			}
		}()
	}

	// Start QUIC listener (if not TCP-only)
	if s.transportMode != TransportTCP && s.cfg.TLS.Enabled {
		go func() {
			if err := s.startQUICServer(ctx); err != nil {
				errChan <- fmt.Errorf("QUIC server: %w", err)
			}
		}()
	}

	// Handle graceful shutdown
	go func() {
		<-ctx.Done()
		s.Stop()
	}()

	// Wait for error or context cancellation
	select {
	case err := <-errChan:
		return err
	case <-ctx.Done():
		return nil
	}
}

// startTCPServer starts the TCP gRPC server
func (s *Server) startTCPServer(ctx context.Context) error {
	addr := fmt.Sprintf(":%d", s.cfg.Server.GRPCPort)

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	s.tcpListener = listener

	log.Printf("[gRPC-TCP] Starting on %s (TLS: %v, mTLS: %v)",
		addr, s.cfg.TLS.Enabled, s.cfg.TLS.RequireClientCert)

	return s.grpcServer.Serve(listener)
}

// startQUICServer starts the QUIC gRPC server
func (s *Server) startQUICServer(ctx context.Context) error {
	// Use different port for QUIC (UDP)
	addr := fmt.Sprintf(":%d", s.cfg.Server.QUICPort)

	tlsConfig, err := loadTLSConfig(s.cfg)
	if err != nil {
		return fmt.Errorf("failed to load TLS for QUIC: %w", err)
	}

	// QUIC configuration optimized for robotics
	quicConfig := &quic.Config{
		MaxIdleTimeout:        s.cfg.QUIC.MaxIdleTimeout,
		KeepAlivePeriod:       s.cfg.QUIC.KeepalivePeriod,
		Allow0RTT:             s.cfg.QUIC.Enable0RTT,
		MaxIncomingStreams:    1000,
		MaxIncomingUniStreams: 100,
		// Connection migration
		DisablePathMTUDiscovery: false,
	}

	// Create QUIC listener that wraps connections as net.Conn
	quicListener, err := NewQUICListener(addr, tlsConfig, quicConfig)
	if err != nil {
		return fmt.Errorf("failed to create QUIC listener: %w", err)
	}
	s.quicListener = quicListener

	log.Printf("[gRPC-QUIC] Starting on %s (0-RTT: %v)",
		addr, s.cfg.QUIC.Enable0RTT)
	log.Printf("[gRPC-QUIC] Connection migration: enabled")

	// Serve gRPC over QUIC streams
	return s.quicServer.Serve(quicListener)
}

// Stop stops the server gracefully
func (s *Server) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	s.mu.Unlock()

	log.Println("[gRPC] Shutting down servers...")

	// Close all agent streams
	s.streamMu.Lock()
	for _, stream := range s.agentStreams {
		close(stream.Done)
	}
	s.agentStreams = make(map[string]*AgentStream)
	s.streamMu.Unlock()

	// Stop QUIC server and listener
	if s.quicServer != nil {
		s.quicServer.GracefulStop()
	}
	if s.quicListener != nil {
		s.quicListener.Close()
	}

	// Stop TCP server and listener
	s.grpcServer.GracefulStop()
	if s.tcpListener != nil {
		s.tcpListener.Close()
	}

	log.Println("[gRPC] Servers stopped")
}

// ============================================================
// Agent Stream Management
// ============================================================

// RegisterAgentStream registers a new agent stream
func (s *Server) RegisterAgentStream(agentID string, stream interface{}, sendChan chan interface{}, transportType string, remoteAddr net.Addr) {
	s.streamMu.Lock()
	defer s.streamMu.Unlock()

	// Close existing stream if any
	if existing, exists := s.agentStreams[agentID]; exists {
		close(existing.Done)
		log.Printf("[Agent] Replacing existing connection for %s (was: %s)", agentID, existing.TransportType)
	}

	s.agentStreams[agentID] = &AgentStream{
		AgentID:       agentID,
		Stream:        stream,
		SendChan:      sendChan,
		Done:          make(chan struct{}),
		TransportType: transportType,
		RemoteAddr:    remoteAddr,
		ConnectedAt:   time.Now(),
	}

	log.Printf("[Agent] Registered: %s via %s from %s", agentID, transportType, remoteAddr)
}

// UnregisterAgentStream removes an agent stream
func (s *Server) UnregisterAgentStream(agentID string) {
	s.streamMu.Lock()
	defer s.streamMu.Unlock()

	if stream, exists := s.agentStreams[agentID]; exists {
		close(stream.Done)
		delete(s.agentStreams, agentID)
		log.Printf("[Agent] Unregistered: %s (was connected via %s)", agentID, stream.TransportType)
	}
}

// GetAgentStream returns an agent's stream
func (s *Server) GetAgentStream(agentID string) (*AgentStream, bool) {
	s.streamMu.RLock()
	defer s.streamMu.RUnlock()

	stream, exists := s.agentStreams[agentID]
	return stream, exists
}

// SendToAgent sends a message to a specific agent
func (s *Server) SendToAgent(agentID string, msg interface{}) error {
	s.streamMu.RLock()
	stream, exists := s.agentStreams[agentID]
	s.streamMu.RUnlock()

	if !exists {
		return fmt.Errorf("agent %s not connected", agentID)
	}

	select {
	case stream.SendChan <- msg:
		return nil
	case <-stream.Done:
		return fmt.Errorf("agent %s stream closed", agentID)
	}
}

// BroadcastToAgents sends a message to all connected agents
func (s *Server) BroadcastToAgents(msg interface{}) {
	s.streamMu.RLock()
	defer s.streamMu.RUnlock()

	for agentID, stream := range s.agentStreams {
		select {
		case stream.SendChan <- msg:
		case <-stream.Done:
			log.Printf("[Agent] Broadcast failed for %s: stream closed", agentID)
		default:
			log.Printf("[Agent] Broadcast failed for %s: channel full", agentID)
		}
	}
}

// GetConnectedAgents returns a list of connected agent IDs
func (s *Server) GetConnectedAgents() []string {
	s.streamMu.RLock()
	defer s.streamMu.RUnlock()

	agents := make([]string, 0, len(s.agentStreams))
	for agentID := range s.agentStreams {
		agents = append(agents, agentID)
	}
	return agents
}

// GetAgentStats returns statistics for all connected agents
func (s *Server) GetAgentStats() map[string]AgentStats {
	s.streamMu.RLock()
	defer s.streamMu.RUnlock()

	stats := make(map[string]AgentStats, len(s.agentStreams))
	for agentID, stream := range s.agentStreams {
		stats[agentID] = AgentStats{
			AgentID:       agentID,
			TransportType: stream.TransportType,
			RemoteAddr:    stream.RemoteAddr.String(),
			ConnectedAt:   stream.ConnectedAt,
			Uptime:        time.Since(stream.ConnectedAt),
		}
	}
	return stats
}

// AgentStats contains statistics for an agent connection
type AgentStats struct {
	AgentID       string
	TransportType string
	RemoteAddr    string
	ConnectedAt   time.Time
	Uptime        time.Duration
}

// ============================================================
// Transport Info
// ============================================================

// GetTransportInfo returns information about active transports
func (s *Server) GetTransportInfo() TransportInfo {
	return TransportInfo{
		TCPEnabled:   s.transportMode != TransportQUIC,
		QUICEnabled:  s.transportMode != TransportTCP && s.cfg.TLS.Enabled,
		TCPPort:      s.cfg.Server.GRPCPort,
		QUICPort:     s.cfg.Server.QUICPort,
		TLSEnabled:   s.cfg.TLS.Enabled,
		MTLSEnabled:  s.cfg.TLS.RequireClientCert,
		QUIC0RTT:     s.cfg.QUIC.Enable0RTT,
	}
}

// TransportInfo contains transport configuration info
type TransportInfo struct {
	TCPEnabled  bool
	QUICEnabled bool
	TCPPort     int
	QUICPort    int
	TLSEnabled  bool
	MTLSEnabled bool
	QUIC0RTT    bool
}
