package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func BenchmarkDaemonWarmResolve(b *testing.B) {
	b.Setenv("OP_SERVICE_ACCOUNT_TOKEN", "")
	config := Config{EntryTTL: time.Hour, IdleTTL: time.Hour}
	daemon := NewDaemon(config, &countingResolver{})
	refs := []string{"op://vault/item/one", "op://vault/item/two"}
	if _, err := daemon.Resolve(context.Background(), "default", refs, false); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for range b.N {
		if _, err := daemon.Resolve(context.Background(), "default", refs, false); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDaemonProtocolWarmResolve(b *testing.B) {
	b.Setenv("OP_SERVICE_ACCOUNT_TOKEN", "")
	dir, err := os.MkdirTemp("/tmp", "op-agent-benchmark-")
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = os.RemoveAll(dir) })
	config := Config{
		SocketPath: filepath.Join(dir, "agent.sock"),
		EntryTTL:   time.Hour,
		IdleTTL:    time.Hour,
		OpCommand:  "op",
	}
	resolver := &countingResolver{}
	daemon := NewDaemon(config, resolver)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- daemon.Serve(ctx) }()
	for deadline := time.Now().Add(time.Second); ; {
		if _, err := os.Stat(config.SocketPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			b.Fatal("daemon socket did not appear")
		}
		time.Sleep(time.Millisecond)
	}
	client := DaemonClient{Config: config, Direct: resolver}
	refs := []string{"op://vault/item/one", "op://vault/item/two"}
	if _, err := client.Resolve(context.Background(), "default", refs, false); err != nil {
		cancel()
		b.Fatal(err)
	}
	b.Cleanup(func() {
		_, _ = client.Control("stop")
		cancel()
		<-done
	})
	b.ResetTimer()
	for range b.N {
		if _, err := client.Resolve(context.Background(), "default", refs, false); err != nil {
			b.Fatal(err)
		}
	}
}
