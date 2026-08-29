//go:build darwin || linux

package agent

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestDaemonEnvironmentIsAllowlisted(t *testing.T) {
	t.Setenv("PATH", "/test/bin")
	t.Setenv("OP_AGENT_SOCKET", "/tmp/test.sock")
	t.Setenv("OP_SERVICE_ACCOUNT_TOKEN", "ops_secret")
	t.Setenv("OP_AGENT_REFS", "TOKEN op://vault/item/value")
	t.Setenv("UNRELATED_SECRET", "secret")
	config := Config{SocketPath: "/tmp/configured.sock", EntryTTL: 2, IdleTTL: 3, OpCommand: "/test/op"}
	environment := daemonEnvironment(config)
	if !slices.Contains(environment, "PATH=/test/bin") || !slices.Contains(environment, "OP_AGENT_SOCKET=/tmp/configured.sock") {
		t.Fatalf("required environment is missing: %#v", environment)
	}
	for _, entry := range environment {
		if strings.HasPrefix(entry, "OP_SERVICE_ACCOUNT_TOKEN=") || strings.HasPrefix(entry, "OP_AGENT_REFS=") || strings.HasPrefix(entry, "UNRELATED_SECRET=") {
			t.Fatalf("secret-bearing environment escaped allowlist: %s", entry)
		}
	}
	if len(environment) >= len(os.Environ()) {
		t.Fatal("daemon inherited the full process environment")
	}
}
