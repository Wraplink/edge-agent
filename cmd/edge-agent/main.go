package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wraplink/edge-agent/internal/agent"
	"github.com/wraplink/edge-agent/internal/allocator"
	"github.com/wraplink/edge-agent/internal/client"
	"github.com/wraplink/edge-agent/internal/config"
	"github.com/wraplink/edge-agent/internal/dataplane"
	"github.com/wraplink/edge-agent/internal/inventory"
	"github.com/wraplink/edge-agent/internal/sniproxy"
	"github.com/wraplink/edge-agent/internal/usage"
)

func main() {
	configFile := flag.String("config", "config.yml", "path to configuration file")
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
	dnsClient.SetDiagnostics(client.Diagnostics{
		AdvertisedIPs:    cfg.Edge.Addresses,
		UpstreamDNS:      cfg.Edge.UpstreamDNS,
		SNIEnabled:       cfg.Edge.SNI.Enabled,
		SNIListenAddress: cfg.Edge.SNI.ListenAddress,
	})
	sniAllocator := allocator.NewSNI(cfg.Edge.Addresses)
	routeAllocator := allocator.NewRoute(cfg.Edge.Addresses)

	dataplaneManager, err := dataplane.NewManager(cfg.Edge.LocalOutputDNAT)
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

	service.SetUpstreamDNS(cfg.Edge.UpstreamDNS)
	if cfg.Agent.Capacity.Route > 0 {
		service.SetRouteInventory(inventory.Client{URL: cfg.Backend.URL, Key: cfg.Backend.ControlKey}, cfg.Backend.RoutePublicIPs)
	}
	log.Printf("SNI upstream DNS: %v", cfg.Edge.UpstreamDNS)

	// SNI listener configuration is loaded exclusively from config.yml.
	authorizer := &sniproxy.Authorizer{}
	service.SetEntitlementObserver(authorizer)

	ctx, cancel := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer cancel()
	startAccessDiagnostics(ctx, authorizer, cfg.Diagnostics)

	var usageOutbox *usage.Outbox
	if cfg.Backend.UsageOutboxDir != "" {
		usageOutbox, err = usage.OpenOutbox(cfg.Backend.UsageOutboxDir)
		if err != nil {
			log.Fatalf("open usage outbox: %v", err)
		}
		sender := usage.Sender{Outbox: usageOutbox, Client: usage.Client{URL: cfg.Backend.URL, Key: cfg.Backend.ControlKey}}
		go func() {
			ticker := time.NewTicker(10 * time.Second)
			defer ticker.Stop()
			for {
				if _, err := sender.Flush(ctx); err != nil && ctx.Err() == nil {
					log.Printf("usage delivery failed; will retry: %v", err)
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	}

	if cfg.Edge.SNI.Enabled {
		address := cfg.Edge.SNI.ListenAddress
		listener, err := net.Listen("tcp", address)
		if err != nil {
			log.Fatalf("start SNI listener: %v", err)
		}
		proxy := &sniproxy.Proxy{
			Access:             authorizer,
			MaxConnections:     cfg.Edge.SNI.MaxConnections,
			ClientHelloTimeout: cfg.Edge.SNI.ClientHelloTimeout,
		}
		if usageOutbox != nil {
			proxy.OnUsage = func(measurement sniproxy.SessionUsage) {
				if measurement.UploadedBytes == 0 && measurement.DownloadedBytes == 0 {
					return
				}
				sequence, err := usageOutbox.ReserveSequence()
				if err != nil {
					log.Printf("CRITICAL: reserve usage sequence: %v", err)
					return
				}
				report := usage.Report{AgentID: cfg.Agent.ID, Sequence: sequence, ClientIP: measurement.ClientIP, UploadedBytes: measurement.UploadedBytes, DownloadedBytes: measurement.DownloadedBytes, StartedAt: measurement.StartedAt, EndedAt: measurement.EndedAt}
				if err := usageOutbox.Enqueue(report); err != nil {
					log.Printf("CRITICAL: persist usage report: %v", err)
				}
			}
		}
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

// startAccessDiagnostics exposes an explicitly configured inspection API.
// Operators must restrict network access and supply a strong token.
func startAccessDiagnostics(ctx context.Context, authorizer *sniproxy.Authorizer, cfg config.DiagnosticsConfig) {
	if !cfg.Enabled {
		return
	}
	token := cfg.Token
	if token == "" {
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/access/allowed-ips", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		provided := r.Header.Get("Authorization")
		expected := "Bearer " + token
		if subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		enforced, version, entries := authorizer.Snapshot()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"enforced":    enforced,
			"revision":    version,
			"count":       len(entries),
			"allowed_ips": entries,
		})
	})
	listener, err := net.Listen("tcp", cfg.ListenAddress)
	if err != nil {
		log.Printf("access diagnostics listener unavailable: %v", err)
		return
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("access diagnostics stopped: %v", err)
		}
	}()
	log.Printf("access diagnostics listening on %s", listener.Addr())
}
