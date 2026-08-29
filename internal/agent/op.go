package agent

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type Resolver interface {
	Resolve(context.Context, string, []string) (map[string]string, error)
}

type DirectResolver struct {
	Credentials CredentialStore
	OpCommand   string
	Inject      func(context.Context, string, string, string) (string, error)
}

func (r DirectResolver) Resolve(ctx context.Context, account string, refs []string) (map[string]string, error) {
	if err := validateAccount(account); err != nil {
		return nil, err
	}
	unique := uniqueReferences(refs)
	if len(unique) == 0 {
		return map[string]string{}, nil
	}
	for _, ref := range unique {
		if !strings.HasPrefix(ref, "op://") || len(ref) == len("op://") {
			return nil, fmt.Errorf("invalid 1Password reference")
		}
	}
	token, err := runtimeCredential(ctx, r.Credentials, account)
	if err != nil {
		return nil, err
	}
	boundary, err := randomBoundary()
	if err != nil {
		return nil, fmt.Errorf("create resolution boundary: %w", err)
	}
	template, markers := buildTemplate(boundary, unique)
	inject := r.Inject
	if inject == nil {
		inject = runInject
	}
	output, err := inject(ctx, r.OpCommand, token, template)
	if err != nil {
		return nil, err
	}
	values, err := parseInjected(output, unique, markers)
	if err != nil {
		return nil, err
	}
	return values, nil
}

func runtimeCredential(ctx context.Context, credentials CredentialStore, account string) (string, error) {
	if token := os.Getenv("OP_SERVICE_ACCOUNT_TOKEN"); token != "" {
		if err := validateToken(token); err != nil {
			return "", fmt.Errorf("OP_SERVICE_ACCOUNT_TOKEN is invalid: %w", err)
		}
		return token, nil
	}
	return credentials.Read(ctx, account)
}

func runInject(ctx context.Context, opCommand, token, template string) (string, error) {
	command := exec.CommandContext(ctx, opCommand, "inject")
	command.Env = replaceEnv(os.Environ(), "OP_SERVICE_ACCOUNT_TOKEN", token)
	command.Stdin = strings.NewReader(template)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return "", fmt.Errorf("1Password resolution failed: %s", message)
	}
	return stdout.String(), nil
}

type markerPair struct {
	begin string
	end   string
}

func randomBoundary() (string, error) {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func buildTemplate(boundary string, refs []string) (string, []markerPair) {
	var template strings.Builder
	markers := make([]markerPair, len(refs))
	for index, ref := range refs {
		markers[index] = markerPair{
			begin: fmt.Sprintf("__OP_AGENT_%s_%d_BEGIN__", boundary, index),
			end:   fmt.Sprintf("__OP_AGENT_%s_%d_END__", boundary, index),
		}
		template.WriteString(markers[index].begin)
		template.WriteString("{{ ")
		template.WriteString(ref)
		template.WriteString(" }}")
		template.WriteString(markers[index].end)
	}
	return template.String(), markers
}

func parseInjected(output string, refs []string, markers []markerPair) (map[string]string, error) {
	if len(refs) != len(markers) {
		return nil, fmt.Errorf("internal marker mismatch")
	}
	values := make(map[string]string, len(refs))
	position := 0
	for index, ref := range refs {
		begin := strings.Index(output[position:], markers[index].begin)
		if begin < 0 {
			return nil, fmt.Errorf("1Password returned an incomplete batch")
		}
		valueStart := position + begin + len(markers[index].begin)
		end := strings.Index(output[valueStart:], markers[index].end)
		if end < 0 {
			return nil, fmt.Errorf("1Password returned an incomplete batch")
		}
		valueEnd := valueStart + end
		values[ref] = output[valueStart:valueEnd]
		position = valueEnd + len(markers[index].end)
	}
	return values, nil
}

func uniqueReferences(refs []string) []string {
	seen := make(map[string]bool, len(refs))
	unique := make([]string, 0, len(refs))
	for _, ref := range refs {
		if !seen[ref] {
			seen[ref] = true
			unique = append(unique, ref)
		}
	}
	return unique
}

func removeEnv(environ []string, name string) []string {
	prefix := name + "="
	out := make([]string, 0, len(environ))
	for _, entry := range environ {
		if !strings.HasPrefix(entry, prefix) {
			out = append(out, entry)
		}
	}
	return out
}
