package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/wraplink/edge-agent/internal/agent"
	"github.com/wraplink/edge-agent/internal/allocator"
	"github.com/wraplink/edge-agent/internal/client"
	"github.com/wraplink/edge-agent/internal/config"
	"github.com/wraplink/edge-agent/internal/dataplane"
	"github.com/wraplink/edge-agent/internal/sniproxy"
)

func main() {
	configFile := flag.String("config", "config.yaml", "path to configuration file")
	flag.Parse()

	cfg, err := config.Load(*configFile)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	log.Printf("edge-agent starting id=%s region=%s", cfg.Agent.ID, cfg.Agent.Region)
	log.Printf("dns-control: %s", cfg.DNSControl.URL)
	log.Printf("poll interval: %s", cfg.Agent.Interval)
	log.Printf("edge addresses: %v", cfg.Edge.Addresses)
	log.Printf("capacity: sni=%d route=%d", cfg.Agent.Capacity.SNI, cfg.Agent.Capacity.Route)

	dnsClient := client.New(cfg.DNSControl.URL, cfg.DNSControl.Token)
	sniAllocator := allocator.NewSNI(cfg.Edge.Addresses)
	routeAllocator := allocator.NewRoute(cfg.Edge.Addresses)

	dataplaneManager, err := dataplane.NewManager()
	if err != nil {
		log.Fatalf("create dataplane manager: %v", err)
	}

	service := agent.New(
		cfg.Agent.ID,
		cfg.Agent.Region,
		cfg.Agent.Interval,
		cfg.Agent.Capacity.SNI,
		cfg.Agent.Capacity.Route,
		dnsClient,
		sniAllocator,
		routeAllocator,
		dataplaneManager,
	)

	// The SNI listener remains disabled until lease routing is fully wired.
	// Entitlements are synchronized now so the proxy can fail closed.
	service.SetEntitlementObserver(&sniproxy.Authorizer{})

	ctx, cancel := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer cancel()

	if err := service.Run(ctx); err != nil {
		if ctx.Err() != nil {
			log.Printf("edge-agent stopped")
			return
		}
		log.Fatalf("edge-agent failed: %v", err)
	}
}
