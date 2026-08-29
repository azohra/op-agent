package agent

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupTokenFromStdin(t *testing.T) {
	credentials := &fakeCredentials{}
	app := App{Credentials: credentials, Stdin: strings.NewReader("ops_test-token\n")}
	token, err := app.setupToken(context.Background(), "", true)
	if err != nil {
		t.Fatal(err)
	}
	if token != "ops_test-token" {
		t.Fatalf("token = %q", token)
	}
}

func TestSetupTokenRequiresExplicitSource(t *testing.T) {
	t.Setenv("OP_SERVICE_ACCOUNT_TOKEN", "")
	app := App{Stdin: strings.NewReader("")}
	if _, err := app.setupToken(context.Background(), "", false); err == nil {
		t.Fatal("setup without a credential source succeeded")
	}
}

func TestCacheStatusDoesNotStartDaemon(t *testing.T) {
	config := testConfig(t)
	var stdout bytes.Buffer
	app := App{Config: config, Credentials: &fakeCredentials{}, Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &bytes.Buffer{}}
	if err := app.runCache([]string{"status"}); err != nil {
		t.Fatal(err)
	}
	if got := stdout.String(); got != "cache daemon: stopped\n" {
		t.Fatalf("output = %q", got)
	}
}

func TestEffectiveVersionUsesModuleVersionForGoInstall(t *testing.T) {
	if got := effectiveVersion("dev", "v0.1.2"); got != "0.1.2" {
		t.Fatalf("effectiveVersion = %q, want %q", got, "0.1.2")
	}
}

func TestEffectiveVersionPrefersLinkerVersion(t *testing.T) {
	if got := effectiveVersion("0.1.2", "v0.1.3"); got != "0.1.2" {
		t.Fatalf("effectiveVersion = %q, want %q", got, "0.1.2")
	}
}

func TestEffectiveVersionKeepsDevelopmentFallback(t *testing.T) {
	if got := effectiveVersion("dev", "(devel)"); got != "dev" {
		t.Fatalf("effectiveVersion = %q, want %q", got, "dev")
	}
}

func TestDefaultMisePluginURLPinsRelease(t *testing.T) {
	want := misePluginRepository + "#v0.1.2"
	if got := defaultMisePluginURL("0.1.2"); got != want {
		t.Fatalf("defaultMisePluginURL = %q, want %q", got, want)
	}
}

func TestDefaultMisePluginURLLeavesDevelopmentOnCurrentRevision(t *testing.T) {
	for _, version := range []string{"dev", "0.1.2-next"} {
		if got := defaultMisePluginURL(version); got != misePluginRepository {
			t.Fatalf("defaultMisePluginURL(%q) = %q, want %q", version, got, misePluginRepository)
		}
	}
}

func TestInstallMisePluginReconcilesExistingInstallation(t *testing.T) {
	temp := t.TempDir()
	log := filepath.Join(temp, "mise.log")
	script := filepath.Join(temp, "mise")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" > \"$MISE_TEST_LOG\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", temp)
	t.Setenv("MISE_TEST_LOG", log)
	ready, err := installMisePlugin(context.Background(), "https://example.com/op-agent.git#v0.1.2")
	if err != nil {
		t.Fatal(err)
	}
	if !ready {
		t.Fatal("mise plugin was not reported ready")
	}
	arguments, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	want := "--yes plugins install --force op-agent https://example.com/op-agent.git#v0.1.2\n"
	if string(arguments) != want {
		t.Fatalf("mise arguments = %q, want %q", arguments, want)
	}
}

func TestMisePluginMatchesRepositoryAndVersion(t *testing.T) {
	temp := t.TempDir()
	data := filepath.Join(temp, "data")
	plugin := filepath.Join(data, "plugins", "op-agent")
	if err := os.MkdirAll(plugin, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plugin, "metadata.lua"), []byte("PLUGIN.version = \"0.1.2\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mise := filepath.Join(temp, "mise")
	script := "#!/bin/sh\nprintf '%s\\n' 'op-agent  https://example.com/op-agent.git  HEAD abc1234'\n"
	if err := os.WriteFile(mise, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MISE_DATA_DIR", data)
	if err := misePluginMatches(context.Background(), mise, "https://example.com/op-agent.git#v0.1.2", "0.1.2"); err != nil {
		t.Fatal(err)
	}
	if err := misePluginMatches(context.Background(), mise, "https://example.com/fork.git#v0.1.2", "0.1.2"); err == nil {
		t.Fatal("plugin from another repository was accepted")
	}
}

func TestMisePluginVersionCurrent(t *testing.T) {
	data := t.TempDir()
	plugin := filepath.Join(data, "plugins", "op-agent")
	if err := os.MkdirAll(plugin, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MISE_DATA_DIR", data)
	metadata := filepath.Join(plugin, "metadata.lua")
	if err := os.WriteFile(metadata, []byte("PLUGIN.version = \"0.1.2\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := misePluginVersionCurrent("0.1.2"); err != nil {
		t.Fatal(err)
	}
	if err := misePluginVersionCurrent("0.1.3"); err == nil {
		t.Fatal("stale plugin version was accepted")
	}
}
