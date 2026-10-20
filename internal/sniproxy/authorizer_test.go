package sniproxy

import (
	"testing"
	"time"

	"github.com/wraplink/edge-agent/internal/client"
)

func TestAuthorizerFailClosedAndRevocation(t *testing.T) {
	var a Authorizer
	if a.Allowed("203.0.113.10") {
		t.Fatal("zero-value authorizer must deny")
	}
	ip := "203.0.113.10"
	if err := a.Replace(true, []client.AccessGrant{{ClientIP: ip, ExpiresAt: time.Now().Add(time.Minute)}}); err != nil {
		t.Fatal(err)
	}
	if !a.Allowed(ip) {
		t.Fatal("valid entitlement must allow client")
	}
	if a.Allowed("203.0.113.11") {
		t.Fatal("unlisted client must be denied")
	}
	if err := a.Replace(true, nil); err != nil {
		t.Fatal(err)
	}
	if a.Allowed(ip) {
		t.Fatal("empty snapshot must revoke client")
	}
}

func TestAuthorizerRejectsExpiredAndDisabledGrants(t *testing.T) {
	var a Authorizer
	ip := "203.0.113.10"
	if err := a.Replace(true, []client.AccessGrant{{ClientIP: ip, ExpiresAt: time.Now().Add(-time.Second)}}); err != nil {
		t.Fatal(err)
	}
	if a.Allowed(ip) {
		t.Fatal("expired grant must be denied")
	}
	if err := a.Replace(false, []client.AccessGrant{{ClientIP: ip, ExpiresAt: time.Now().Add(time.Hour)}}); err != nil {
		t.Fatal(err)
	}
	if a.Allowed(ip) {
		t.Fatal("unenforced snapshot must deny")
	}
	if err := a.Replace(true, []client.AccessGrant{{ClientIP: "invalid", ExpiresAt: time.Now().Add(time.Hour)}}); err == nil {
		t.Fatal("invalid IPv4 must fail")
	}
}

func TestAuthorizerVersionAdvancesOnReplacement(t *testing.T) {
	var a Authorizer
	initial := a.Version()
	if err := a.Replace(true, []client.AccessGrant{{
		ClientIP:  "203.0.113.10",
		ExpiresAt: time.Now().Add(time.Minute),
	}}); err != nil {
		t.Fatal(err)
	}
	if a.Version() <= initial {
		t.Fatal("snapshot replacement must advance generation")
	}
	previous := a.Version()
	if err := a.Replace(true, nil); err != nil {
		t.Fatal(err)
	}
	if a.Version() <= previous {
		t.Fatal("revocation snapshot must advance generation")
	}
}
