package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

type countingResolver struct {
	mu       sync.Mutex
	calls    [][]string
	accounts []string
	delay    time.Duration
	fail     bool
}

type resolverFunc func(context.Context, string, []string) (map[string]string, error)

func (f resolverFunc) Resolve(ctx context.Context, account string, refs []string) (map[string]string, error) {
	return f(ctx, account, refs)
}

func (r *countingResolver) Resolve(_ context.Context, account string, refs []string) (map[string]string, error) {
	r.mu.Lock()
	r.calls = append(r.calls, append([]string(nil), refs...))
	r.accounts = append(r.accounts, account)
	r.mu.Unlock()
	if r.delay > 0 {
		time.Sleep(r.delay)
	}
	if r.fail {
		return nil, fmt.Errorf("resolution failed")
	}
	values := make(map[string]string, len(refs))
	for _, ref := range refs {
		values[ref] = account + ":" + ref
	}
	return values, nil
}

func (r *countingResolver) snapshot() ([][]string, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]string(nil), r.calls...), append([]string(nil), r.accounts...)
}

func testConfig(t *testing.T) Config {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "op-agent-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return Config{
		SocketPath: filepath.Join(dir, "agent.sock"),
		EntryTTL:   time.Hour,
		IdleTTL:    time.Hour,
		OpCommand:  "op",
	}
}

func TestDaemonSingleflightsConcurrentColdResolution(t *testing.T) {
	t.Setenv("OP_SERVICE_ACCOUNT_TOKEN", "")
	resolver := &countingResolver{delay: 25 * time.Millisecond}
	daemon := NewDaemon(testConfig(t), resolver)
	refs := []string{"op://vault/item/one", "op://vault/item/two"}
	start := make(chan struct{})
	var wait sync.WaitGroup
	for range 20 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			values, err := daemon.Resolve(context.Background(), "default", refs, false)
			if err != nil || len(values) != 2 {
				t.Errorf("Resolve() = %#v, %v", values, err)
			}
		}()
	}
	close(start)
	wait.Wait()
	calls, _ := resolver.snapshot()
	if len(calls) != 1 || !reflect.DeepEqual(calls[0], refs) {
		t.Fatalf("resolver calls = %#v, want one full batch", calls)
	}
}

func TestDaemonResolvesOnlyMissingReferences(t *testing.T) {
	t.Setenv("OP_SERVICE_ACCOUNT_TOKEN", "")
	resolver := &countingResolver{}
	daemon := NewDaemon(testConfig(t), resolver)
	one := "op://vault/item/one"
	two := "op://vault/item/two"
	if _, err := daemon.Resolve(context.Background(), "default", []string{one}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := daemon.Resolve(context.Background(), "default", []string{one, two}, false); err != nil {
		t.Fatal(err)
	}
	calls, _ := resolver.snapshot()
	want := [][]string{{one}, {two}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("resolver calls = %#v, want %#v", calls, want)
	}
}

func TestDaemonFreshAndExpiredValuesResolveAgain(t *testing.T) {
	t.Setenv("OP_SERVICE_ACCOUNT_TOKEN", "")
	config := testConfig(t)
	config.EntryTTL = 5 * time.Millisecond
	resolver := &countingResolver{}
	daemon := NewDaemon(config, resolver)
	ref := "op://vault/item/value"
	if _, err := daemon.Resolve(context.Background(), "default", []string{ref}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := daemon.Resolve(context.Background(), "default", []string{ref}, true); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if _, err := daemon.Resolve(context.Background(), "default", []string{ref}, false); err != nil {
		t.Fatal(err)
	}
	calls, _ := resolver.snapshot()
	if len(calls) != 3 {
		t.Fatalf("resolver calls = %d, want 3", len(calls))
	}
}

func TestDaemonIsolatesAccounts(t *testing.T) {
	t.Setenv("OP_SERVICE_ACCOUNT_TOKEN", "")
	resolver := &countingResolver{}
	daemon := NewDaemon(testConfig(t), resolver)
	ref := "op://vault/item/value"
	first, err := daemon.Resolve(context.Background(), "default", []string{ref}, false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := daemon.Resolve(context.Background(), "work", []string{ref}, false)
	if err != nil {
		t.Fatal(err)
	}
	if first[ref] == second[ref] {
		t.Fatal("account-specific values were shared")
	}
	calls, accounts := resolver.snapshot()
	if len(calls) != 2 || !reflect.DeepEqual(accounts, []string{"default", "work"}) {
		t.Fatalf("calls/accounts = %#v/%#v", calls, accounts)
	}
}

func TestDaemonFailurePublishesNothing(t *testing.T) {
	t.Setenv("OP_SERVICE_ACCOUNT_TOKEN", "")
	resolver := &countingResolver{fail: true}
	daemon := NewDaemon(testConfig(t), resolver)
	if _, err := daemon.Resolve(context.Background(), "default", []string{"op://vault/item/value"}, false); err == nil {
		t.Fatal("failed resolution succeeded")
	}
	entries, accounts := daemon.Status()
	if entries != 0 || accounts != 0 {
		t.Fatalf("status = %d/%d, want 0/0", entries, accounts)
	}
}

func TestDaemonIncompleteBatchPublishesNothing(t *testing.T) {
	t.Setenv("OP_SERVICE_ACCOUNT_TOKEN", "")
	config := testConfig(t)
	resolver := resolverFunc(func(_ context.Context, _ string, refs []string) (map[string]string, error) {
		return map[string]string{refs[0]: "first", "unrequested": "other"}, nil
	})
	daemon := NewDaemon(config, resolver)
	refs := []string{"op://vault/item/one", "op://vault/item/two"}
	if _, err := daemon.Resolve(context.Background(), "default", refs, false); err == nil {
		t.Fatal("incomplete resolution succeeded")
	}
	entries, accounts := daemon.Status()
	if entries != 0 || accounts != 0 {
		t.Fatalf("status = %d/%d, want 0/0", entries, accounts)
	}
}

func TestDaemonProtocolReportsCountsAndStops(t *testing.T) {
	t.Setenv("OP_SERVICE_ACCOUNT_TOKEN", "")
	config := testConfig(t)
	resolver := &countingResolver{}
	daemon := NewDaemon(config, resolver)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- daemon.Serve(ctx) }()
	for deadline := time.Now().Add(time.Second); ; {
		if _, err := os.Stat(config.SocketPath); err == nil {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("daemon exited before opening its socket: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("daemon socket did not appear")
		}
		time.Sleep(time.Millisecond)
	}
	if err := NewDaemon(config, resolver).Serve(ctx); err != nil {
		t.Fatalf("second daemon start should accept the existing server: %v", err)
	}
	if _, err := os.Stat(config.SocketPath); err != nil {
		t.Fatalf("second daemon start removed the active socket: %v", err)
	}
	client := DaemonClient{Config: config, Direct: resolver}
	ref := "op://vault/item/value"
	values, err := client.Resolve(context.Background(), "default", []string{ref}, false)
	if err != nil || values[ref] == "" {
		t.Fatalf("Resolve() = %#v, %v", values, err)
	}
	status, err := client.Control("status")
	if err != nil {
		t.Fatal(err)
	}
	if status.Entries != 1 || status.Accounts != 1 || status.Values != nil {
		t.Fatalf("status = %#v", status)
	}
	if _, err := client.Control("stop"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("daemon did not stop")
	}
}
