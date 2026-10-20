package main

import (
	"context"
	"flag"
	"log"
	"net"
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

	// SNI leases are not yet activated, so the listener has no forwarding routes.
	// An explicit listen address enables isolated listener testing only.
	authorizer := &sniproxy.Authorizer{}
	service.SetEntitlementObserver(authorizer)

	ctx, cancel := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer cancel()

	if address := os.Getenv("EDGE_SNI_LISTEN_ADDR"); address != "" {
		listener, err := net.Listen("tcp", address)
		if err != nil {
			log.Fatalf("start SNI listener: %v", err)
		}
		proxy := &sniproxy.Proxy{Access: authorizer}
		service.SetSNIForwarder(proxy)
		go func() {
			if err := proxy.Serve(ctx, listener); err != nil {
				log.Printf("SNI listener stopped: %v", err)
				cancel()
			}
		}()
		log.Printf("SNI listener started address=%s", listener.Addr())
	}

	if err := service.Run(ctx); err != nil {
		if ctx.Err() != nil {
			log.Printf("edge-agent stopped")
			return
		}
		log.Fatalf("edge-agent failed: %v", err)
	}
}
