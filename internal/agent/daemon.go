package agent

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type cacheKey struct {
	account string
	ref     string
}

type cacheEntry struct {
	value     string
	expiresAt time.Time
}

type Daemon struct {
	config    Config
	resolver  Resolver
	mu        sync.Mutex
	resolveMu sync.Mutex
	cache     map[cacheKey]cacheEntry
}

func NewDaemon(config Config, resolver Resolver) *Daemon {
	return &Daemon{config: config, resolver: resolver, cache: map[cacheKey]cacheEntry{}}
}

func (d *Daemon) Resolve(ctx context.Context, account string, refs []string, fresh bool) (map[string]string, error) {
	if err := validateAccount(account); err != nil {
		return nil, err
	}
	refs = uniqueReferences(refs)
	if len(refs) == 0 {
		return map[string]string{}, nil
	}
	if !fresh {
		if values, complete := d.cached(account, refs); complete {
			return values, nil
		}
	}

	// A single cold-resolution lane batches concurrent pressure against 1Password.
	// Rechecking after acquiring it means followers consume the first caller's work.
	d.resolveMu.Lock()
	defer d.resolveMu.Unlock()

	missing := refs
	values := map[string]string{}
	if !fresh {
		var complete bool
		values, complete = d.cached(account, refs)
		if complete {
			return values, nil
		}
		missing = missingReferences(refs, values)
	}
	resolved, err := d.resolver.Resolve(ctx, account, missing)
	if err != nil {
		return nil, err
	}
	if len(resolved) != len(missing) {
		return nil, fmt.Errorf("resolver returned an incomplete batch")
	}
	for _, ref := range missing {
		if _, ok := resolved[ref]; !ok {
			return nil, fmt.Errorf("resolver returned an incomplete batch")
		}
	}
	now := time.Now()
	d.mu.Lock()
	for _, ref := range missing {
		value := resolved[ref]
		d.cache[cacheKey{account: account, ref: ref}] = cacheEntry{value: value, expiresAt: now.Add(d.config.EntryTTL)}
		values[ref] = value
	}
	d.mu.Unlock()
	return values, nil
}

func (d *Daemon) cached(account string, refs []string) (map[string]string, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now()
	values := make(map[string]string, len(refs))
	for _, ref := range refs {
		key := cacheKey{account: account, ref: ref}
		entry, ok := d.cache[key]
		if !ok || !entry.expiresAt.After(now) {
			delete(d.cache, key)
			continue
		}
		values[ref] = entry.value
	}
	return values, len(values) == len(refs)
}

func missingReferences(refs []string, values map[string]string) []string {
	missing := make([]string, 0, len(refs)-len(values))
	for _, ref := range refs {
		if _, ok := values[ref]; !ok {
			missing = append(missing, ref)
		}
	}
	return missing
}

func (d *Daemon) Clear() {
	d.mu.Lock()
	d.cache = map[cacheKey]cacheEntry{}
	d.mu.Unlock()
}

func (d *Daemon) Status() (entries, accounts int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now()
	seen := map[string]bool{}
	for key, entry := range d.cache {
		if !entry.expiresAt.After(now) {
			delete(d.cache, key)
			continue
		}
		entries++
		seen[key.account] = true
	}
	return entries, len(seen)
}

func (d *Daemon) Serve(ctx context.Context) error {
	listener, err := listenPrivate(d.config.SocketPath)
	if err != nil {
		if errors.Is(err, errDaemonAlreadyRunning) {
			return nil
		}
		return err
	}
	socketInfo, err := os.Lstat(d.config.SocketPath)
	if err != nil {
		listener.Close()
		return err
	}
	defer func() {
		listener.Close()
		if current, err := os.Lstat(d.config.SocketPath); err == nil && os.SameFile(socketInfo, current) {
			_ = os.Remove(d.config.SocketPath)
		}
	}()
	go func() {
		<-ctx.Done()
		listener.Close()
	}()
	for {
		if err := listener.SetDeadline(time.Now().Add(d.config.IdleTTL)); err != nil {
			return err
		}
		conn, err := listener.AcceptUnix()
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				return nil
			}
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		stop := d.handleConnection(conn)
		conn.Close()
		if stop {
			return nil
		}
	}
}

func (d *Daemon) handleConnection(conn *net.UnixConn) bool {
	uid, err := peerUID(conn)
	if err != nil || uid != uint32(os.Getuid()) {
		_ = writeFrame(conn, Response{Error: "daemon peer is not authorized"})
		return false
	}
	var request Request
	if err := readFrame(conn, &request); err != nil {
		_ = writeFrame(conn, Response{Error: "invalid daemon request"})
		return false
	}
	if request.Version != ProtocolVersion {
		_ = writeFrame(conn, Response{Error: "daemon protocol version mismatch"})
		return false
	}
	response := Response{}
	stop := false
	switch request.Action {
	case "resolve":
		values, err := d.Resolve(context.Background(), request.Account, request.Refs, request.Fresh)
		if err != nil {
			response.Error = err.Error()
		} else {
			response.Values = values
		}
	case "status":
		response.Entries, response.Accounts = d.Status()
	case "clear":
		d.Clear()
	case "stop":
		stop = true
	default:
		response.Error = "unknown daemon action"
	}
	_ = writeFrame(conn, response)
	return stop
}

func listenPrivate(socketPath string) (*net.UnixListener, error) {
	dir := filepath.Dir(socketPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create daemon directory: %w", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("daemon directory %s must be private (0700)", dir)
	}
	if !ownedByCurrentUser(info) {
		return nil, fmt.Errorf("daemon directory %s is not owned by the current user", dir)
	}
	if info, err := os.Lstat(socketPath); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("refusing to replace non-socket path %s", socketPath)
		}
		if !ownedByCurrentUser(info) {
			return nil, fmt.Errorf("refusing to replace a socket owned by another user")
		}
		if conn, dialErr := net.DialTimeout("unix", socketPath, 100*time.Millisecond); dialErr == nil {
			conn.Close()
			return nil, errDaemonAlreadyRunning
		}
		if err := os.Remove(socketPath); err != nil {
			return nil, fmt.Errorf("remove stale daemon socket: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(socketPath, 0o600); err != nil {
		listener.Close()
		return nil, err
	}
	return listener, nil
}

var errDaemonAlreadyRunning = errors.New("daemon is already running")
var errDaemonStopped = errors.New("cache daemon is stopped")

type DaemonClient struct {
	Config Config
	Direct Resolver
}

func (c DaemonClient) Resolve(ctx context.Context, account string, refs []string, fresh bool) (map[string]string, error) {
	// An explicitly supplied credential is already process-scoped and portable.
	// Resolve directly instead of copying it into a long-lived daemon environment.
	if os.Getenv("OP_SERVICE_ACCOUNT_TOKEN") != "" {
		return c.Direct.Resolve(ctx, account, refs)
	}
	request := Request{Version: ProtocolVersion, Action: "resolve", Account: account, Refs: refs, Fresh: fresh}
	response, sent, err := c.requestState(request)
	if err == nil {
		if response.Error != "" {
			return nil, errors.New(response.Error)
		}
		return response.Values, nil
	}
	if sent {
		return nil, fmt.Errorf("daemon request failed after delivery: %w", err)
	}
	if startErr := startDaemonProcess(c.Config); startErr == nil {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
			response, requestSent, requestErr := c.requestState(request)
			if requestErr == nil {
				if response.Error != "" {
					return nil, errors.New(response.Error)
				}
				return response.Values, nil
			}
			if requestSent {
				return nil, fmt.Errorf("daemon request failed after delivery: %w", requestErr)
			}
		}
	}
	return c.Direct.Resolve(ctx, account, refs)
}

func (c DaemonClient) Control(action string) (Response, error) {
	response, sent, err := c.requestState(Request{Version: ProtocolVersion, Action: action})
	if err == nil {
		return response, nil
	}
	if !sent {
		return Response{}, errDaemonStopped
	}
	return Response{}, fmt.Errorf("daemon request failed after delivery: %w", err)
}

func (c DaemonClient) requestState(request Request) (Response, bool, error) {
	conn, err := net.DialTimeout("unix", c.Config.SocketPath, protocolWindow)
	if err != nil {
		return Response{}, false, err
	}
	defer conn.Close()
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return Response{}, false, fmt.Errorf("daemon connection is not a Unix socket")
	}
	uid, err := peerUID(unixConn)
	if err != nil || uid != uint32(os.Getuid()) {
		return Response{}, false, fmt.Errorf("daemon peer is not authorized")
	}
	if err := writeFrame(conn, request); err != nil {
		return Response{}, true, err
	}
	var response Response
	if err := readFrameWithin(conn, &response, responseWindow); err != nil {
		return Response{}, true, err
	}
	return response, true, nil
}
