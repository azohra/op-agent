//go:build !darwin

package agent

import (
	"context"
	"fmt"
)

type systemCredentialStore struct{}

func (systemCredentialStore) Read(context.Context, string) (string, error) {
	return "", fmt.Errorf("the system credential store is not implemented on this platform")
}

func (systemCredentialStore) Write(context.Context, string, string) error {
	return fmt.Errorf("the system credential store is not implemented on this platform")
}
