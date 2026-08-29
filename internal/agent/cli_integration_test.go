package agent

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNativePassthroughPreservesOutputArgumentsAndExitCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("native passthrough uses Unix process replacement")
	}
	_, sourceFile, _, _ := runtime.Caller(0)
	repository := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", ".."))
	temp := t.TempDir()
	binary := filepath.Join(temp, "op-agent")
	build := exec.Command("go", "build", "-o", binary, "./cmd/op-agent")
	build.Dir = repository
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build op-agent: %v\n%s", err, output)
	}
	fakeOp := filepath.Join(temp, "fake-op")
	fakeSource := "#!/usr/bin/env bash\nset -euo pipefail\ntest \"$OP_SERVICE_ACCOUNT_TOKEN\" = ops_passthrough\nprintf 'native stdout: %s\\n' \"$*\"\nprintf 'native stderr\\n' >&2\nexit 23\n"
	if err := os.WriteFile(fakeOp, []byte(fakeSource), 0o700); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "item", "list", "--format=json")
	command.Env = replaceEnv(os.Environ(), "OP_SERVICE_ACCOUNT_TOKEN", "ops_passthrough")
	command.Env = replaceEnv(command.Env, "OP_AGENT_OP_COMMAND", fakeOp)
	stdout, err := command.Output()
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != 23 {
		t.Fatalf("exit error = %v", err)
	}
	if string(stdout) != "native stdout: item list --format=json\n" {
		t.Fatalf("stdout = %q", stdout)
	}
	if strings.TrimSpace(string(exitError.Stderr)) != "native stderr" {
		t.Fatalf("stderr = %q", exitError.Stderr)
	}
}
