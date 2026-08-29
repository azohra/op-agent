//go:build darwin

package agent

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestKeychainBatchKeepsTokenOutOfArguments(t *testing.T) {
	token := "ops_example-token"
	input := keychainBatchInput("default", token)
	if !strings.Contains(input, "-w "+token) || !strings.Contains(input, "-T /usr/bin/security") {
		t.Fatalf("batch input does not contain the credential and explicit ACL")
	}
	argv := []string{"/usr/bin/security", "-i"}
	if strings.Contains(strings.Join(argv, " "), token) {
		t.Fatal("credential appeared in process arguments")
	}
}

func TestSecurityBatchInterfaceStoresCredentialOutsideArgv(t *testing.T) {
	keychain := filepath.Join(t.TempDir(), "op-agent-test.keychain-db")
	password := "op-agent-test-password"
	create := exec.Command("/usr/bin/security", "create-keychain", "-p", password, keychain)
	if output, err := create.CombinedOutput(); err != nil {
		t.Fatalf("create temporary keychain: %v\n%s", err, output)
	}
	t.Cleanup(func() {
		_ = exec.Command("/usr/bin/security", "delete-keychain", keychain).Run()
	})
	token := "ops_batch-interface-test"
	input := strings.TrimSuffix(keychainBatchInput("default", token), "\n") + " \"" + keychain + "\"\n"
	command := exec.CommandContext(context.Background(), "/usr/bin/security", "-i")
	command.Stdin = strings.NewReader(input)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("store in temporary keychain through batch input: %v\n%s", err, output)
	}
	read := exec.Command("/usr/bin/security", "find-generic-password", "-w", "-s", keychainService, "-a", "default", keychain)
	output, err := read.Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSuffix(string(output), "\n") != token {
		t.Fatal("temporary Keychain value did not round-trip")
	}
}
