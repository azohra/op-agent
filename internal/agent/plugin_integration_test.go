package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMisePluginTaskEnvironment(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping mise integration in short mode")
	}
	mise, err := exec.LookPath("mise")
	if err != nil {
		t.Skip("mise is not installed")
	}
	_, sourceFile, _, _ := runtime.Caller(0)
	repository := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", ".."))
	temp, err := os.MkdirTemp("/tmp", "op-agent-plugin-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(temp) })
	for _, dir := range []string{"bin", "cache", "config", "data", "project", "state"} {
		if err := os.Mkdir(filepath.Join(temp, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	binary := filepath.Join(temp, "bin", "op-agent")
	build := exec.Command("go", "build", "-o", binary, "./cmd/op-agent")
	build.Dir = repository
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build op-agent: %v\n%s", err, output)
	}
	countFile := filepath.Join(temp, "op-count")
	fakeOp := filepath.Join(temp, "bin", "fake-op")
	fakeSource := "#!/usr/bin/env bash\nset -euo pipefail\ntest \"$1\" = inject\nprintf x >> \"$FAKE_OP_COUNT\"\nsed 's#{{ op://vault/preview/token }}#plugin-value#g'\n"
	if err := os.WriteFile(fakeOp, []byte(fakeSource), 0o700); err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(temp, "project", "mise.toml")
	configSource := `[env]
OP_AGENT_REFS = "TOKEN op://vault/local/token"
OP_AGENT_REFS_PREVIEW_CA = "TOKEN op://vault/preview/token"

[tasks.show]
run = 'test "$TOKEN" = plugin-value && printf "%s\\n" "$TOKEN"'

[tasks.show.env._]
op-agent = { keys = ["TOKEN"], profile = "preview-ca" }

[tasks.legacy]
run = "exit 99"

[tasks.legacy.env._]
op-agent = { keys = ["TOKEN"], environment = "prod" }
`
	if err := os.WriteFile(configFile, []byte(configSource), 0o600); err != nil {
		t.Fatal(err)
	}

	environment := os.Environ()
	for name, value := range map[string]string{
		"MISE_DATA_DIR":   filepath.Join(temp, "data"),
		"MISE_CACHE_DIR":  filepath.Join(temp, "cache"),
		"MISE_CONFIG_DIR": filepath.Join(temp, "config"),
		"MISE_STATE_DIR":  filepath.Join(temp, "state"),
	} {
		environment = replaceEnv(environment, name, value)
	}
	link := exec.Command(mise, "plugins", "link", "op-agent", repository)
	link.Dir = temp
	link.Env = environment
	if output, err := link.CombinedOutput(); err != nil {
		t.Fatalf("link plugin: %v\n%s", err, output)
	}
	trust := exec.Command(mise, "trust", "--yes", configFile)
	trust.Dir = filepath.Join(temp, "project")
	trust.Env = environment
	if output, err := trust.CombinedOutput(); err != nil {
		t.Fatalf("trust test config: %v\n%s", err, output)
	}

	run := exec.Command(mise, "run", "show")
	run.Dir = filepath.Join(temp, "project")
	run.Env = environment
	for name, value := range map[string]string{
		"PATH":                     filepath.Join(temp, "bin") + string(os.PathListSeparator) + os.Getenv("PATH"),
		"OP_SERVICE_ACCOUNT_TOKEN": "ops_test",
		"OP_AGENT_OP_COMMAND":      fakeOp,
		"FAKE_OP_COUNT":            countFile,
	} {
		run.Env = replaceEnv(run.Env, name, value)
	}
	output, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("run task through plugin: %v\n%s", err, output)
	}
	if strings.Contains(string(output), "plugin-value") || !strings.Contains(string(output), "[redacted]") {
		t.Fatalf("mise did not redact plugin output:\n%s", output)
	}
	count, err := os.ReadFile(countFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(count) != "x" {
		t.Fatalf("op inject call count = %d, want 1", len(count))
	}

	legacy := exec.Command(mise, "run", "legacy")
	legacy.Dir = filepath.Join(temp, "project")
	legacy.Env = run.Env
	output, err = legacy.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "unsupported option environment") {
		t.Fatalf("legacy plugin option did not fail closed: %v\n%s", err, output)
	}
	count, err = os.ReadFile(countFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(count) != "x" {
		t.Fatalf("legacy option made a resolution call; count = %d", len(count))
	}
}
