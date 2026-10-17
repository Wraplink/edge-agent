package client

import "time"

// ---------- Shared ----------

type Capacity struct {
	SNI   int `json:"sni"`
	Route int `json:"route"`
}

// ---------- Agent registration ----------

type RegisterRequest struct {
	AgentID  string   `json:"agent_id"`
	Region   string   `json:"region,omitempty"`
	Capacity Capacity `json:"capacity"`
}

type RegisterResponse struct {
	OK bool `json:"ok"`
}

// ---------- Poll ----------

type PollRequest struct {
	AgentID   string   `json:"agent_id"`
	Available Capacity `json:"available"`
}

type PollResponse struct {
	Revision uint64  `json:"revision"`
	Leases   []Lease `json:"leases"`
}

// ---------- Lease ----------

type Lease struct {
	ID string `json:"id"`

	Domain string `json:"domain"`
	Type   string `json:"type"`
	State  string `json:"state"`

	AgentID   string `json:"agent_id,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`

	Address string `json:"address,omitempty"`
	SNI     string `json:"sni,omitempty"`
	RouteID string `json:"route_id,omitempty"`

	DestinationIP string `json:"destination_ip,omitempty"`
	Protocol      string `json:"protocol,omitempty"`

	DestinationPortStart uint16 `json:"destination_port_start,omitempty"`
	DestinationPortEnd   uint16 `json:"destination_port_end,omitempty"`

	Revision uint64 `json:"revision"`
}

// ---------- Entitlement access grants ----------

type AccessGrant struct {
	EntitlementID  string    `json:"entitlementId"`
	SubscriptionID string    `json:"subscriptionId"`
	ClientIP       string    `json:"clientIp"`
	ExpiresAt      time.Time `json:"expiresAt"`
}

// ---------- Heartbeat ----------

type HeartbeatRequest struct {
	AgentID             string   `json:"agent_id"`
	LeaseIDs            []string `json:"lease_ids"`
	EntitlementRevision uint64   `json:"entitlement_revision"`
}

type HeartbeatResponse struct {
	OK                  bool          `json:"ok"`
	Registered          bool          `json:"registered"`
	Revision            uint64        `json:"revision"`
	ReleaseLeaseIDs     []string      `json:"release_lease_ids,omitempty"`
	EntitlementRevision uint64        `json:"entitlement_revision"`
	Entitlements        []AccessGrant `json:"entitlements,omitempty"`
}

// ---------- Report ----------

type ReportRequest struct {
	AgentID string `json:"agent_id"`
	LeaseID string `json:"lease_id"`
	Success bool   `json:"success"`

	Address string `json:"address,omitempty"`
	SNI     string `json:"sni,omitempty"`
	RouteID string `json:"route_id,omitempty"`

	DestinationIP string `json:"destination_ip,omitempty"`
	Protocol      string `json:"protocol,omitempty"`

	DestinationPort uint16 `json:"destination_port,omitempty"`
	SourcePort      uint16 `json:"source_port,omitempty"`
}
