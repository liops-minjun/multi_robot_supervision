package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the server configuration
type Config struct {
	Server  ServerConfig  `yaml:"server"`
	Neo4j   Neo4jConfig   `yaml:"neo4j"`
	TLS     TLSConfig     `yaml:"tls"`
	QUIC    QUICConfig    `yaml:"quic"`
	Logging LoggingConfig `yaml:"logging"`
}

type ServerConfig struct {
	GRPCPort        int    `yaml:"grpc_port"`
	QUICPort        int    `yaml:"quic_port"`
	RawQUICPort     int    `yaml:"raw_quic_port"`     // Port for raw protobuf over QUIC (C++ agents)
	HTTPPort        int    `yaml:"http_port"`
	DefinitionsPath string `yaml:"definitions_path"`
}

type Neo4jConfig struct {
	URI      string `yaml:"uri"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	Database string `yaml:"database"`
}

func (n *Neo4jConfig) DSN() string {
	return fmt.Sprintf("neo4j://%s@%s", n.Username, n.URI)
}

type TLSConfig struct {
	Enabled           bool   `yaml:"enabled"`
	CACert            string `yaml:"ca_cert"`
	ServerCert        string `yaml:"server_cert"`
	ServerKey         string `yaml:"server_key"`
	RequireClientCert bool   `yaml:"require_client_cert"`
}

type QUICConfig struct {
	MaxIdleTimeout       time.Duration `yaml:"max_idle_timeout"`
	KeepalivePeriod      time.Duration `yaml:"keepalive_period"`
	Enable0RTT           bool          `yaml:"enable_0rtt"`
	QUICOnly             bool          `yaml:"quic_only"`             // Disable TCP, use QUIC only
	MaxIncomingStreams   int64         `yaml:"max_incoming_streams"`
	ConnectionMigration  bool          `yaml:"connection_migration"`  // Allow IP change
	InitialStreamWindow  uint64        `yaml:"initial_stream_window"` // Flow control window
	InitialConnWindow    uint64        `yaml:"initial_conn_window"`
}

type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// Load reads configuration from a YAML file
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Expand environment variables
	data = []byte(os.ExpandEnv(string(data)))

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Apply defaults
	applyDefaults(&cfg)

	// Override with environment variables
	applyEnvOverrides(&cfg)

	return &cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.Server.GRPCPort == 0 {
		cfg.Server.GRPCPort = 9090
	}
	if cfg.Server.QUICPort == 0 {
		cfg.Server.QUICPort = 9443 // Default QUIC port (UDP)
	}
	if cfg.Server.RawQUICPort == 0 {
		cfg.Server.RawQUICPort = 9444 // Default raw QUIC port for C++ agents
	}
	if cfg.Server.HTTPPort == 0 {
		cfg.Server.HTTPPort = 8081
	}
	if cfg.Neo4j.URI == "" {
		cfg.Neo4j.URI = "neo4j://localhost:7687"
	}
	if cfg.Neo4j.Username == "" {
		cfg.Neo4j.Username = "neo4j"
	}
	if cfg.Neo4j.Password == "" {
		cfg.Neo4j.Password = "neo4j"
	}
	if cfg.Neo4j.Database == "" {
		cfg.Neo4j.Database = "neo4j"
	}
	// QUIC defaults optimized for robotics
	if cfg.QUIC.MaxIdleTimeout == 0 {
		cfg.QUIC.MaxIdleTimeout = 30 * time.Second
	}
	if cfg.QUIC.KeepalivePeriod == 0 {
		cfg.QUIC.KeepalivePeriod = 10 * time.Second
	}
	if cfg.QUIC.MaxIncomingStreams == 0 {
		cfg.QUIC.MaxIncomingStreams = 1000
	}
	if cfg.QUIC.InitialStreamWindow == 0 {
		cfg.QUIC.InitialStreamWindow = 1 * 1024 * 1024 // 1MB
	}
	if cfg.QUIC.InitialConnWindow == 0 {
		cfg.QUIC.InitialConnWindow = 2 * 1024 * 1024 // 2MB
	}
	// Enable connection migration by default (important for mobile robots)
	cfg.QUIC.ConnectionMigration = true
	if cfg.Logging.Level == "" {
		cfg.Logging.Level = "info"
	}
	if cfg.Logging.Format == "" {
		cfg.Logging.Format = "json"
	}
}

func applyEnvOverrides(cfg *Config) {
	if uri := os.Getenv("NEO4J_URI"); uri != "" {
		cfg.Neo4j.URI = uri
	}
	if user := os.Getenv("NEO4J_USER"); user != "" {
		cfg.Neo4j.Username = user
	}
	if pass := os.Getenv("NEO4J_PASSWORD"); pass != "" {
		cfg.Neo4j.Password = pass
	}
	if name := os.Getenv("NEO4J_DATABASE"); name != "" {
		cfg.Neo4j.Database = name
	}
}
