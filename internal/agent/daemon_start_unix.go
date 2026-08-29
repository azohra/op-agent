//go:build darwin || linux

package agent

import (
	"os"
	"os/exec"
	"syscall"
)

func startDaemonProcess(config Config) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	command := exec.Command(executable, "daemon", "serve")
	command.Env = daemonEnvironment(config)
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer null.Close()
	command.Stdin = null
	command.Stdout = null
	command.Stderr = null
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

func daemonEnvironment(config Config) []string {
	allowed := []string{
		"HOME",
		"LANG",
		"LC_ALL",
		"PATH",
		"TMPDIR",
		"XDG_RUNTIME_DIR",
	}
	environment := make([]string, 0, len(allowed)+4)
	for _, name := range allowed {
		if value, ok := os.LookupEnv(name); ok {
			environment = append(environment, name+"="+value)
		}
	}
	environment = replaceEnv(environment, "OP_AGENT_SOCKET", config.SocketPath)
	environment = replaceEnv(environment, "OP_AGENT_CACHE_TTL", config.EntryTTL.String())
	environment = replaceEnv(environment, "OP_AGENT_IDLE_TTL", config.IdleTTL.String())
	environment = replaceEnv(environment, "OP_AGENT_OP_COMMAND", config.OpCommand)
	return environment
}
