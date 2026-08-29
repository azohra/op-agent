package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	ProtocolVersion = 1
	keychainService = "dev.azohra.op-agent"
	defaultAccount  = "default"
)

var (
	accountPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	profilePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	tokenPattern   = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

type Config struct {
	SocketPath string
	EntryTTL   time.Duration
	IdleTTL    time.Duration
	OpCommand  string
}

func ConfigFromEnv() (Config, error) {
	entryTTL, err := durationEnv("OP_AGENT_CACHE_TTL", 8*time.Hour)
	if err != nil {
		return Config{}, err
	}
	idleTTL, err := durationEnv("OP_AGENT_IDLE_TTL", 8*time.Hour)
	if err != nil {
		return Config{}, err
	}
	socketPath := os.Getenv("OP_AGENT_SOCKET")
	if socketPath == "" {
		socketPath = filepath.Join(os.TempDir(), fmt.Sprintf("op-agent-%d", os.Getuid()), fmt.Sprintf("agent-v%d.sock", ProtocolVersion))
	}
	opCommand := os.Getenv("OP_AGENT_OP_COMMAND")
	if opCommand == "" {
		opCommand = "op"
	}
	return Config{SocketPath: socketPath, EntryTTL: entryTTL, IdleTTL: idleTTL, OpCommand: opCommand}, nil
}

func durationEnv(name string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", name)
	}
	return d, nil
}

func validateAccount(account string) error {
	if !accountPattern.MatchString(account) {
		return errors.New("account must contain 1-64 letters, numbers, dots, underscores, or hyphens")
	}
	return nil
}

func validateToken(token string) error {
	if !tokenPattern.MatchString(token) {
		return errors.New("service-account token contains unsupported characters")
	}
	return nil
}

func parseBoolEnv(name string) (bool, error) {
	value := os.Getenv(name)
	if value == "" {
		return false, nil
	}
	b, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", name)
	}
	return b, nil
}

func replaceEnv(environ []string, name, value string) []string {
	prefix := name + "="
	out := make([]string, 0, len(environ)+1)
	for _, entry := range environ {
		if !strings.HasPrefix(entry, prefix) {
			out = append(out, entry)
		}
	}
	return append(out, prefix+value)
}
