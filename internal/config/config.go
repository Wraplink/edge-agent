package config

import (
	"fmt"
	"net"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	DNSControl DNSControlConfig `yaml:"dns_control"`
	Agent      AgentConfig      `yaml:"agent"`
	Edge       EdgeConfig       `yaml:"edge"`
}

type DNSControlConfig struct {
	URL   string `yaml:"url"`
	Token string `yaml:"token"`
}

type AgentConfig struct {
	ID       string         `yaml:"id"`
	Region   string         `yaml:"region"`
	Interval time.Duration  `yaml:"interval"`
	Capacity CapacityConfig `yaml:"capacity"`
}

type CapacityConfig struct {
	SNI   int `yaml:"sni"`
	Route int `yaml:"route"`
}

type EdgeConfig struct {
	Addresses   []string  `yaml:"addresses"`
	UpstreamDNS []string  `yaml:"upstream_dns"`
	SNI         SNIConfig `yaml:"sni"`
}

type SNIConfig struct {
	Enabled            bool          `yaml:"enabled"`
	ListenAddress      string        `yaml:"listen_address"`
	MaxConnections     int           `yaml:"max_connections"`
	ClientHelloTimeout time.Duration `yaml:"client_hello_timeout"`
}

func Load(filename string) (Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return Config{}, fmt.Errorf("read config %q: %w", filename, err)
	}

	// Expand environment placeholders after debug.sh (or the container runtime)
	// supplies variables. Do not silently use literal ${VAR} strings.
	missing := make([]string, 0)
	expanded := os.Expand(string(data), func(key string) string {
		value, ok := os.LookupEnv(key)
		if !ok {
			missing = append(missing, key)
		}
		return value
	})
	if len(missing) > 0 {
		sort.Strings(missing)
		return Config{}, fmt.Errorf("expand config %q: missing environment variables: %s", filename, strings.Join(missing, ", "))
	}
	data = []byte(expanded)

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %q: %w", filename, err)
	}

	cfg.DNSControl.URL = strings.TrimRight(strings.TrimSpace(cfg.DNSControl.URL), "/")
	cfg.Agent.ID = strings.TrimSpace(cfg.Agent.ID)
	cfg.Agent.Region = strings.TrimSpace(cfg.Agent.Region)
	cfg.Edge.SNI.ListenAddress = strings.TrimSpace(cfg.Edge.SNI.ListenAddress)
	if len(cfg.Edge.UpstreamDNS) == 0 {
		cfg.Edge.UpstreamDNS = []string{"1.1.1.1:53", "8.8.8.8:53"}
	}
	for i := range cfg.Edge.UpstreamDNS {
		cfg.Edge.UpstreamDNS[i] = strings.TrimSpace(cfg.Edge.UpstreamDNS[i])
	}
	for i := range cfg.Edge.Addresses {
		cfg.Edge.Addresses[i] = strings.TrimSpace(cfg.Edge.Addresses[i])
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if c.DNSControl.URL == "" {
		return fmt.Errorf("dns_control.url is required")
	}
	if strings.TrimSpace(c.DNSControl.Token) == "" {
		return fmt.Errorf("dns_control.token is required")
	}
	if c.Agent.ID == "" {
		return fmt.Errorf("agent.id is required")
	}
	if c.Agent.Interval <= 0 {
		return fmt.Errorf("agent.interval must be greater than zero")
	}
	if c.Agent.Capacity.SNI < 0 || c.Agent.Capacity.Route < 0 {
		return fmt.Errorf("agent capacity cannot be negative")
	}
	if c.Agent.Capacity.SNI == 0 && c.Agent.Capacity.Route == 0 {
		return fmt.Errorf("at least one agent capacity must be greater than zero")
	}
	if c.Edge.SNI.MaxConnections < 0 {
		return fmt.Errorf("edge.sni.max_connections cannot be negative")
	}
	if c.Edge.SNI.ClientHelloTimeout < 0 {
		return fmt.Errorf("edge.sni.client_hello_timeout cannot be negative")
	}
	if c.Edge.SNI.Enabled && c.Edge.SNI.MaxConnections == 0 {
		return fmt.Errorf("edge.sni.max_connections must be greater than zero when enabled")
	}
	if c.Edge.SNI.Enabled && c.Edge.SNI.ClientHelloTimeout == 0 {
		return fmt.Errorf("edge.sni.client_hello_timeout must be greater than zero when enabled")
	}
	if c.Edge.SNI.Enabled {
		if c.Edge.SNI.ListenAddress == "" {
			return fmt.Errorf("edge.sni.listen_address is required when enabled")
		}
		if _, _, err := net.SplitHostPort(c.Edge.SNI.ListenAddress); err != nil {
			return fmt.Errorf("edge.sni.listen_address: %w", err)
		}
	}
	for _, upstream := range c.Edge.UpstreamDNS {
		host, port, err := net.SplitHostPort(upstream)
		if err != nil || net.ParseIP(host) == nil || port != "53" {
			return fmt.Errorf("edge.upstream_dns must contain IP:53 addresses: %q", upstream)
		}
	}
	if len(c.Edge.Addresses) == 0 {
		return fmt.Errorf("edge.addresses must contain at least one address")
	}
	for i, address := range c.Edge.Addresses {
		if address == "" {
			return fmt.Errorf("edge.addresses[%d] cannot be empty", i)
		}
	}
	return nil
}
