package config

import (
	"fmt"
	"os"
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
	Addresses []string `yaml:"addresses"`
}

func Load(filename string) (Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return Config{}, fmt.Errorf("read config %q: %w", filename, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %q: %w", filename, err)
	}

	cfg.DNSControl.URL = strings.TrimRight(strings.TrimSpace(cfg.DNSControl.URL), "/")
	cfg.Agent.ID = strings.TrimSpace(cfg.Agent.ID)
	cfg.Agent.Region = strings.TrimSpace(cfg.Agent.Region)
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
