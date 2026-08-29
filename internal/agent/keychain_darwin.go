//go:build darwin

package agent

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type systemCredentialStore struct{}

func (systemCredentialStore) Read(ctx context.Context, account string) (string, error) {
	if err := validateAccount(account); err != nil {
		return "", err
	}
	command := exec.CommandContext(ctx, "/usr/bin/security", "find-generic-password", "-w", "-s", keychainService, "-a", account)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("credential for account %q is unavailable; run op-agent setup --account %s", account, account)
	}
	token := strings.TrimSuffix(stdout.String(), "\n")
	if err := validateToken(token); err != nil {
		return "", fmt.Errorf("stored credential for account %q is invalid: %w", account, err)
	}
	return token, nil
}

func (systemCredentialStore) Write(ctx context.Context, account, token string) error {
	if err := validateAccount(account); err != nil {
		return err
	}
	if err := validateToken(token); err != nil {
		return err
	}
	// The batch interface keeps the credential out of argv and process listings.
	input := keychainBatchInput(account, token)
	command := exec.CommandContext(ctx, "/usr/bin/security", "-i")
	command.Stdin = strings.NewReader(input)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("store credential in Keychain: %s", strings.TrimSpace(stderr.String()))
	}
	return nil
}

func keychainBatchInput(account, token string) string {
	return fmt.Sprintf("add-generic-password -U -s %s -a %s -w %s -T /usr/bin/security\n", keychainService, account, token)
}
