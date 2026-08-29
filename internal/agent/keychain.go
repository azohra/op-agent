package agent

import "context"

type CredentialStore interface {
	Read(context.Context, string) (string, error)
	Write(context.Context, string, string) error
}

func newCredentialStore() CredentialStore {
	return systemCredentialStore{}
}
