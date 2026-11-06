package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// NFTCounterReader reads only explicitly tagged WrapLink Full Route counters.
// It does not install rules. The nftables dataplane must create matching
// FORWARD rules before this reader can return traffic measurements.
type NFTCounterReader struct {
	Command string
	Table   string
	Chain   string
}

func (r NFTCounterReader) Read(ctx context.Context) ([]RouteCounter, error) {
	command := r.Command
	if command == "" {
		command = "nft"
	}
	table := r.Table
	if table == "" {
		table = "wraplink"
	}
	chain := r.Chain
	if chain == "" {
		chain = "route_accounting"
	}
	output, err := exec.CommandContext(ctx, command, "-j", "list", "chain", "ip", table, chain).Output()
	if err != nil {
		return nil, fmt.Errorf("read nft route counters: %w", err)
	}
	return ParseNFTCounters(output)
}

// Counter comments use the exact format:
// wraplink:route-usage:<client IPv4>:<upload|download>:<stable counter ID>
// Stable IDs are necessary to distinguish replacement/reset counters.
func ParseNFTCounters(data []byte) ([]RouteCounter, error) {
	var document struct {
		NFTables []struct {
			Rule *struct {
				Comment string            `json:"comment"`
				Expr    []json.RawMessage `json:"expr"`
			} `json:"rule"`
		} `json:"nftables"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("decode nft counters: %w", err)
	}
	if document.NFTables == nil {
		return nil, fmt.Errorf("invalid nft counters: missing nftables array")
	}
	result := make([]RouteCounter, 0)
	seen := make(map[string]bool)
	for _, item := range document.NFTables {
		if item.Rule == nil || !strings.HasPrefix(item.Rule.Comment, "wraplink:route-usage:") {
			continue
		}
		parts := strings.Split(item.Rule.Comment, ":")
		if len(parts) != 5 || parts[0] != "wraplink" || parts[1] != "route-usage" {
			return nil, fmt.Errorf("invalid route counter tag %q", item.Rule.Comment)
		}
		clientIP, direction, identity := parts[2], parts[3], parts[4]
		if identity == "" || (direction != "upload" && direction != "download") {
			return nil, fmt.Errorf("invalid route counter tag %q", item.Rule.Comment)
		}
		// Validate IPv4 using the same rules as the backend usage report.
		if err := (Report{AgentID: "validation", Sequence: 1, ClientIP: clientIP, StartedAt: time.Unix(1, 0), EndedAt: time.Unix(2, 0)}).Validate(); err != nil {
			return nil, fmt.Errorf("invalid route counter client %q: %w", clientIP, err)
		}
		if seen[item.Rule.Comment] {
			return nil, fmt.Errorf("duplicate route counter tag %q", item.Rule.Comment)
		}
		seen[item.Rule.Comment] = true
		count := 0
		var bytes uint64
		for _, raw := range item.Rule.Expr {
			var expression map[string]json.RawMessage
			if err := json.Unmarshal(raw, &expression); err != nil {
				return nil, err
			}
			counter, ok := expression["counter"]
			if !ok {
				continue
			}
			var values struct {
				Bytes json.Number `json:"bytes"`
			}
			if err := json.Unmarshal(counter, &values); err != nil {
				return nil, err
			}
			if values.Bytes == "" {
				return nil, fmt.Errorf("counter missing bytes")
			}
			value, err := strconv.ParseUint(values.Bytes.String(), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid counter bytes: %w", err)
			}
			bytes = value
			count++
		}
		if count != 1 {
			return nil, fmt.Errorf("expected exactly one counter for %q", item.Rule.Comment)
		}
		result = append(result, RouteCounter{Key: item.Rule.Comment, ClientIP: clientIP, Direction: direction, Bytes: bytes})
	}
	return result, nil
}
