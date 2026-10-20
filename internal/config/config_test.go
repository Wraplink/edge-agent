package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testConfig = `dns_control:
  url: ${DNS_CONTROL_API_URL}
  token: ${DNS_CONTROL_API_TOKEN}
agent:
  id: edge-agent-test
  region: eu-1
  interval: 10s
  capacity:
    sni: 1
    route: 0
edge:
  addresses:
    - 203.0.113.10
`

func writeTestConfig(t *testing.T, data string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadExpandsEnvironment(t *testing.T) {
	t.Setenv("DNS_CONTROL_API_URL", "http://127.0.0.1:8080")
	t.Setenv("DNS_CONTROL_API_TOKEN", "example-local-token")

	cfg, err := Load(writeTestConfig(t, testConfig))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DNSControl.URL != "http://127.0.0.1:8080" {
		t.Fatalf("URL not expanded: %q", cfg.DNSControl.URL)
	}
	if cfg.DNSControl.Token != "example-local-token" {
		t.Fatal("DNS control token was not expanded")
	}
}

func TestLoadReportsMissingVariable(t *testing.T) {
	t.Setenv("DNS_CONTROL_API_URL", "http://127.0.0.1:8080")
	t.Setenv("DNS_CONTROL_API_TOKEN", "example-local-token")
	configText := strings.ReplaceAll(testConfig, "${DNS_CONTROL_API_TOKEN}", "${EDGE_AGENT_TEST_MISSING_VARIABLE}")
	_ = os.Unsetenv("EDGE_AGENT_TEST_MISSING_VARIABLE")

	_, err := Load(writeTestConfig(t, configText))
	if err == nil || !strings.Contains(err.Error(), "EDGE_AGENT_TEST_MISSING_VARIABLE") {
		t.Fatalf("expected missing environment variable error, got %v", err)
	}
}

func TestLoadRejectsEmptyControlToken(t *testing.T) {
	t.Setenv("DNS_CONTROL_API_URL", "http://127.0.0.1:8080")
	t.Setenv("DNS_CONTROL_API_TOKEN", "")

	_, err := Load(writeTestConfig(t, testConfig))
	if err == nil || !strings.Contains(err.Error(), "dns_control.token is required") {
		t.Fatalf("expected missing token error, got %v", err)
	}
}
