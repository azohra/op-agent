package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"syscall"
)

var Version = "dev"

const misePluginRepository = "https://github.com/azohra/op-agent.git"

type App struct {
	Config      Config
	Credentials CredentialStore
	Stdin       io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
}

func NewApp(config Config) App {
	return App{
		Config:      config,
		Credentials: newCredentialStore(),
		Stdin:       os.Stdin,
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
	}
}

func (a App) Run(ctx context.Context, args []string) error {
	account := defaultAccount
	if len(args) >= 2 && args[0] == "-v" {
		account = args[1]
		args = args[2:]
	}
	if err := validateAccount(account); err != nil {
		return err
	}
	if len(args) == 0 {
		return errors.New(usage)
	}
	switch args[0] {
	case "--help", "-h", "help":
		fmt.Fprintln(a.Stdout, usage)
		return nil
	case "--version", "version":
		fmt.Fprintln(a.Stdout, currentVersion())
		return nil
	case "setup":
		return a.runSetup(ctx, args[1:], account)
	case "doctor":
		return a.runDoctor(ctx, args[1:], account)
	case "env":
		return a.runEnv(ctx, args[1:], account)
	case "read":
		if simpleReadArgs(args[1:]) {
			return a.runRead(ctx, args[1:], account)
		}
		return a.passthrough(ctx, args, account)
	case "cache":
		return a.runCache(args[1:])
	case "daemon":
		if len(args) == 2 && args[1] == "serve" {
			resolver := DirectResolver{Credentials: a.Credentials, OpCommand: a.Config.OpCommand}
			return NewDaemon(a.Config, resolver).Serve(ctx)
		}
		return errors.New("usage: op-agent daemon serve")
	default:
		return a.passthrough(ctx, args, account)
	}
}

func currentVersion() string {
	moduleVersion := ""
	if info, ok := debug.ReadBuildInfo(); ok {
		moduleVersion = info.Main.Version
	}
	return effectiveVersion(Version, moduleVersion)
}

func effectiveVersion(linkerVersion, moduleVersion string) string {
	if linkerVersion != "dev" {
		return linkerVersion
	}
	if moduleVersion != "" && moduleVersion != "(devel)" {
		return strings.TrimPrefix(moduleVersion, "v")
	}
	return linkerVersion
}

func (a App) resolver() DaemonClient {
	direct := DirectResolver{Credentials: a.Credentials, OpCommand: a.Config.OpCommand}
	return DaemonClient{Config: a.Config, Direct: direct}
}

func (a App) runEnv(ctx context.Context, args []string, inheritedAccount string) error {
	flags := flag.NewFlagSet("env", flag.ContinueOnError)
	flags.SetOutput(a.Stderr)
	keysFlag := flags.String("keys", "", "comma- or space-separated environment names")
	profileFlag := flags.String("profile", "", "named reference overlay")
	accountFlag := flags.String("account", firstNonEmpty(os.Getenv("OP_AGENT_ACCOUNT"), inheritedAccount), "credential account")
	freshFlag := flags.Bool("fresh", false, "bypass cached values")
	formatFlag := flags.String("format", "json", "output format (json)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("env does not accept positional arguments")
	}
	if *formatFlag != "json" {
		return errors.New("env supports only --format json")
	}
	if err := validateAccount(*accountFlag); err != nil {
		return err
	}
	keysValue := firstNonEmpty(*keysFlag, os.Getenv("OP_AGENT_KEYS"))
	profile := firstNonEmpty(*profileFlag, os.Getenv("OP_AGENT_PROFILE"))
	freshEnv, err := parseBoolEnv("OP_AGENT_FRESH")
	if err != nil {
		return err
	}
	mapping, err := MappingForProfile(profile, os.Getenv)
	if err != nil {
		return err
	}
	selected, err := SelectMapping(mapping, splitKeys(keysValue))
	if err != nil {
		return err
	}
	refs := make([]string, 0, len(selected))
	for _, name := range sortedKeys(selected) {
		refs = append(refs, selected[name])
	}
	values, err := a.resolver().Resolve(ctx, *accountFlag, refs, *freshFlag || freshEnv)
	if err != nil {
		return err
	}
	environmentValues := make(map[string]string, len(selected))
	for name, ref := range selected {
		value, ok := values[ref]
		if !ok {
			return errors.New("resolver returned an incomplete batch")
		}
		environmentValues[name] = value
	}
	encoder := json.NewEncoder(a.Stdout)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(environmentValues)
}

func simpleReadArgs(args []string) bool {
	refs := 0
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--no-newline", "--fresh":
		case "--account":
			index++
			if index >= len(args) {
				return false
			}
		default:
			if strings.HasPrefix(args[index], "-") || !strings.HasPrefix(args[index], "op://") {
				return false
			}
			refs++
		}
	}
	return refs == 1
}

func (a App) runRead(ctx context.Context, args []string, inheritedAccount string) error {
	flags := flag.NewFlagSet("read", flag.ContinueOnError)
	flags.SetOutput(a.Stderr)
	noNewline := flags.Bool("no-newline", false, "do not print a trailing newline")
	fresh := flags.Bool("fresh", false, "bypass cached values")
	account := flags.String("account", inheritedAccount, "credential account")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 || !strings.HasPrefix(flags.Arg(0), "op://") {
		return errors.New("usage: op-agent read [--no-newline] [--fresh] [--account NAME] op://reference")
	}
	if err := validateAccount(*account); err != nil {
		return err
	}
	ref := flags.Arg(0)
	values, err := a.resolver().Resolve(ctx, *account, []string{ref}, *fresh)
	if err != nil {
		return err
	}
	if *noNewline {
		_, err = io.WriteString(a.Stdout, values[ref])
	} else {
		_, err = fmt.Fprintln(a.Stdout, values[ref])
	}
	return err
}

func (a App) runCache(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: op-agent cache status|clear|stop")
	}
	if args[0] != "status" && args[0] != "clear" && args[0] != "stop" {
		return errors.New("usage: op-agent cache status|clear|stop")
	}
	response, err := a.resolver().Control(args[0])
	if err != nil {
		if errors.Is(err, errDaemonStopped) {
			fmt.Fprintln(a.Stdout, "cache daemon: stopped")
			return nil
		}
		return err
	}
	if response.Error != "" {
		return errors.New(response.Error)
	}
	switch args[0] {
	case "status":
		fmt.Fprintf(a.Stdout, "cache daemon: running (%d entries across %d accounts)\n", response.Entries, response.Accounts)
	case "clear":
		fmt.Fprintln(a.Stdout, "cache cleared")
	case "stop":
		fmt.Fprintln(a.Stdout, "cache daemon stopped")
	default:
		return errors.New("usage: op-agent cache status|clear|stop")
	}
	return nil
}

func (a App) passthrough(ctx context.Context, args []string, account string) error {
	token, err := runtimeCredential(ctx, a.Credentials, account)
	if err != nil {
		return err
	}
	path, err := exec.LookPath(a.Config.OpCommand)
	if err != nil {
		return fmt.Errorf("find 1Password CLI: %w", err)
	}
	environ := replaceEnv(os.Environ(), "OP_SERVICE_ACCOUNT_TOKEN", token)
	return syscall.Exec(path, append([]string{a.Config.OpCommand}, args...), environ)
}

func (a App) runSetup(ctx context.Context, args []string, inheritedAccount string) error {
	flags := flag.NewFlagSet("setup", flag.ContinueOnError)
	flags.SetOutput(a.Stderr)
	account := flags.String("account", inheritedAccount, "credential account")
	fromRef := flags.String("from-ref", "", "read the service-account token with the desktop app")
	tokenStdin := flags.Bool("token-stdin", false, "read the service-account token from stdin")
	replace := flags.Bool("replace", false, "replace an existing credential")
	skipPlugin := flags.Bool("skip-mise-plugin", false, "do not install the mise plugin")
	pluginURL := flags.String("mise-plugin-url", defaultMisePluginURL(currentVersion()), "mise plugin Git URL")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("setup does not accept positional arguments")
	}
	if err := validateAccount(*account); err != nil {
		return err
	}
	existing := false
	if _, err := a.Credentials.Read(ctx, *account); err == nil {
		existing = true
	}
	if !existing || *replace {
		token, err := a.setupToken(ctx, *fromRef, *tokenStdin)
		if err != nil {
			return err
		}
		if err := a.Credentials.Write(ctx, *account, token); err != nil {
			return err
		}
		fmt.Fprintf(a.Stdout, "credential stored for account %s\n", *account)
	} else {
		fmt.Fprintf(a.Stdout, "credential already configured for account %s\n", *account)
	}
	_, _ = a.resolver().Control("clear")
	if !*skipPlugin {
		installed, err := installMisePlugin(ctx, *pluginURL)
		if err != nil {
			return err
		}
		if installed {
			fmt.Fprintln(a.Stdout, "mise plugin ready")
		} else {
			fmt.Fprintln(a.Stdout, "mise not installed; plugin setup skipped")
		}
	}
	return nil
}

func (a App) setupToken(ctx context.Context, fromRef string, tokenStdin bool) (string, error) {
	if fromRef != "" && tokenStdin {
		return "", errors.New("choose only one of --from-ref and --token-stdin")
	}
	var token string
	switch {
	case fromRef != "":
		if !strings.HasPrefix(fromRef, "op://") {
			return "", errors.New("--from-ref must be a 1Password reference")
		}
		command := exec.CommandContext(ctx, a.Config.OpCommand, "read", "--no-newline", fromRef)
		command.Env = removeEnv(os.Environ(), "OP_SERVICE_ACCOUNT_TOKEN")
		var stdout, stderr bytes.Buffer
		command.Stdout = &stdout
		command.Stderr = &stderr
		if err := command.Run(); err != nil {
			return "", fmt.Errorf("read setup credential with 1Password desktop app: %s", strings.TrimSpace(stderr.String()))
		}
		token = stdout.String()
	case tokenStdin:
		value, err := io.ReadAll(io.LimitReader(a.Stdin, 64<<10))
		if err != nil {
			return "", fmt.Errorf("read credential from stdin: %w", err)
		}
		token = strings.TrimSpace(string(value))
	default:
		token = os.Getenv("OP_SERVICE_ACCOUNT_TOKEN")
		if token == "" {
			return "", errors.New("provide --from-ref, --token-stdin, or OP_SERVICE_ACCOUNT_TOKEN")
		}
	}
	if err := validateToken(token); err != nil {
		return "", err
	}
	return token, nil
}

func installMisePlugin(ctx context.Context, pluginURL string) (bool, error) {
	mise, err := exec.LookPath("mise")
	if err != nil {
		return false, nil
	}
	version := currentVersion()
	if version != "dev" && !strings.HasSuffix(version, "-next") {
		if err := misePluginMatches(ctx, mise, pluginURL, version); err == nil {
			return true, nil
		}
	}
	command := exec.CommandContext(ctx, mise, "--yes", "plugins", "install", "--force", "op-agent", pluginURL)
	command.Dir = os.TempDir()
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return false, fmt.Errorf("install mise plugin: %s", strings.TrimSpace(stderr.String()))
	}
	return true, nil
}

func misePluginMatches(ctx context.Context, mise, expectedURL, expectedVersion string) error {
	command := exec.CommandContext(ctx, mise, "plugins", "ls", "--urls")
	command.Dir = os.TempDir()
	output, err := command.Output()
	if err != nil {
		return err
	}
	wantURL, _, _ := strings.Cut(expectedURL, "#")
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "op-agent" {
			if fields[1] != wantURL {
				return errors.New("plugin repository differs")
			}
			return misePluginVersionCurrent(expectedVersion)
		}
	}
	return errors.New("plugin is not installed")
}

func defaultMisePluginURL(version string) string {
	version = strings.TrimPrefix(version, "v")
	if version == "" || version == "dev" || strings.HasSuffix(version, "-next") {
		return misePluginRepository
	}
	return misePluginRepository + "#v" + version
}

func (a App) runDoctor(ctx context.Context, args []string, inheritedAccount string) error {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flags.SetOutput(a.Stderr)
	account := flags.String("account", inheritedAccount, "credential account")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("doctor does not accept positional arguments")
	}
	checks := []struct {
		name string
		err  error
	}{
		{name: "1Password CLI", err: commandAvailable(a.Config.OpCommand)},
	}
	_, credentialErr := runtimeCredential(ctx, a.Credentials, *account)
	credentialName := "Keychain credential"
	if os.Getenv("OP_SERVICE_ACCOUNT_TOKEN") != "" {
		credentialName = "environment credential"
	}
	checks = append(checks, struct {
		name string
		err  error
	}{name: credentialName, err: credentialErr})
	if _, err := exec.LookPath("mise"); err == nil {
		checks = append(checks, struct {
			name string
			err  error
		}{name: "mise plugin", err: misePluginInstalled(ctx, currentVersion())})
	}
	failures := 0
	for _, check := range checks {
		if check.err != nil {
			failures++
			fmt.Fprintf(a.Stdout, "not ready: %s (%s)\n", check.name, check.err)
		} else {
			fmt.Fprintf(a.Stdout, "ready: %s\n", check.name)
		}
	}
	if response, err := a.resolver().Control("status"); err == nil {
		fmt.Fprintf(a.Stdout, "cache daemon: running (%d entries across %d accounts)\n", response.Entries, response.Accounts)
	} else if errors.Is(err, errDaemonStopped) {
		fmt.Fprintln(a.Stdout, "cache daemon: stopped (starts on demand)")
	} else {
		failures++
		fmt.Fprintf(a.Stdout, "not ready: cache daemon (%s)\n", err)
	}
	if failures > 0 {
		return fmt.Errorf("doctor found %d setup problem(s)", failures)
	}
	return nil
}

func commandAvailable(name string) error {
	_, err := exec.LookPath(name)
	return err
}

func misePluginInstalled(ctx context.Context, expectedVersion string) error {
	command := exec.CommandContext(ctx, "mise", "plugins", "ls")
	command.Dir = os.TempDir()
	output, err := command.Output()
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(output), "\n") {
		if strings.TrimSpace(line) == "op-agent" {
			return misePluginVersionCurrent(expectedVersion)
		}
	}
	return errors.New("run op-agent setup")
}

func misePluginVersionCurrent(expectedVersion string) error {
	if expectedVersion == "dev" || strings.HasSuffix(expectedVersion, "-next") {
		return nil
	}
	dataDir := os.Getenv("MISE_DATA_DIR")
	if dataDir == "" {
		if xdgData := os.Getenv("XDG_DATA_HOME"); xdgData != "" {
			dataDir = filepath.Join(xdgData, "mise")
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("locate mise plugin: %w", err)
			}
			dataDir = filepath.Join(home, ".local", "share", "mise")
		}
	}
	metadata, err := os.ReadFile(filepath.Join(dataDir, "plugins", "op-agent", "metadata.lua"))
	if err != nil {
		return errors.New("run op-agent setup")
	}
	want := `PLUGIN.version = "` + strings.TrimPrefix(expectedVersion, "v") + `"`
	for _, line := range strings.Split(string(metadata), "\n") {
		if strings.TrimSpace(line) == want {
			return nil
		}
	}
	return errors.New("plugin version differs; run op-agent setup")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func Main(args []string) int {
	config, err := ConfigFromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, "op-agent:", err)
		return 1
	}
	app := NewApp(config)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := app.Run(ctx, args); err != nil {
		fmt.Fprintln(os.Stderr, "op-agent:", err)
		return 1
	}
	return 0
}

const usage = `Usage: op-agent COMMAND [OPTIONS]

Commands:
  setup             store a service-account credential and install the mise plugin
  doctor            check local readiness without resolving a secret
  env               resolve a selected environment as JSON
  read              resolve one reference, using the memory cache when possible
  cache status      report cache counts without names or values
  cache clear       remove all in-memory values
  cache stop        stop the daemon and discard its memory

Other commands are passed directly to the 1Password CLI.`
