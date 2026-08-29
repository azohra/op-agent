package agent

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type fakeCredentials struct {
	token  string
	reads  int
	writes int
}

func (f *fakeCredentials) Read(context.Context, string) (string, error) {
	f.reads++
	if f.token == "" {
		return "", errors.New("missing credential")
	}
	return f.token, nil
}

func (f *fakeCredentials) Write(_ context.Context, _ string, token string) error {
	f.writes++
	f.token = token
	return nil
}

func TestDirectResolverBatchesAndPreservesValues(t *testing.T) {
	t.Setenv("OP_SERVICE_ACCOUNT_TOKEN", "")
	credentials := &fakeCredentials{token: "ops_test-token"}
	values := map[string]string{
		"op://vault/item/one": "first\nline\nsecond line\n",
		"op://vault/item/two": "quotes='\"' equals== unicode=雪",
	}
	calls := 0
	resolver := DirectResolver{
		Credentials: credentials,
		OpCommand:   "op",
		Inject: func(_ context.Context, command, token, template string) (string, error) {
			calls++
			if command != "op" || token != credentials.token {
				t.Fatalf("command/token = %q/%q", command, token)
			}
			for ref, value := range values {
				template = strings.ReplaceAll(template, "{{ "+ref+" }}", value)
			}
			return template, nil
		},
	}
	got, err := resolver.Resolve(context.Background(), "default", []string{
		"op://vault/item/one",
		"op://vault/item/two",
		"op://vault/item/one",
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || credentials.reads != 1 {
		t.Fatalf("calls/credential reads = %d/%d, want 1/1", calls, credentials.reads)
	}
	if !reflect.DeepEqual(got, values) {
		t.Fatalf("values = %#v, want %#v", got, values)
	}
}

func TestDirectResolverRejectsIncompleteBatch(t *testing.T) {
	t.Setenv("OP_SERVICE_ACCOUNT_TOKEN", "")
	resolver := DirectResolver{
		Credentials: &fakeCredentials{token: "ops_test"},
		OpCommand:   "op",
		Inject: func(_ context.Context, _, _, _ string) (string, error) {
			return "not a marked response", nil
		},
	}
	if _, err := resolver.Resolve(context.Background(), "default", []string{"op://vault/item/value"}); err == nil {
		t.Fatal("incomplete batch succeeded")
	}
}

func TestDirectResolverUsesExplicitEnvironmentCredential(t *testing.T) {
	t.Setenv("OP_SERVICE_ACCOUNT_TOKEN", "ops_ci-token")
	credentials := &fakeCredentials{}
	resolver := DirectResolver{
		Credentials: credentials,
		OpCommand:   "op",
		Inject: func(_ context.Context, _, token, template string) (string, error) {
			if token != "ops_ci-token" {
				t.Fatalf("token = %q", token)
			}
			return strings.ReplaceAll(template, "{{ op://vault/item/value }}", "resolved"), nil
		},
	}
	values, err := resolver.Resolve(context.Background(), "default", []string{"op://vault/item/value"})
	if err != nil {
		t.Fatal(err)
	}
	if values["op://vault/item/value"] != "resolved" || credentials.reads != 0 {
		t.Fatalf("values/credential reads = %#v/%d", values, credentials.reads)
	}
}

func TestReplaceEnvRemovesEveryPriorCredential(t *testing.T) {
	got := replaceEnv([]string{"A=1", "OP_SERVICE_ACCOUNT_TOKEN=old", "OP_SERVICE_ACCOUNT_TOKEN=older"}, "OP_SERVICE_ACCOUNT_TOKEN", "new")
	want := []string{"A=1", "OP_SERVICE_ACCOUNT_TOKEN=new"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("environment = %#v, want %#v", got, want)
	}
}
