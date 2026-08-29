# op-agent

`op-agent` makes 1Password-backed mise tasks fast and predictable. It resolves
every reference needed by a task in one batch, then keeps the results in memory
for warm runs. This reduces task latency and 1Password service-account
rate-limit pressure.

The repository ships a Go binary and a mise environment plugin in each release.
The binary owns credential storage, batch resolution, and the memory cache. The
plugin turns task options into one binary request and redacts the returned
environment in mise output.

## Why use it

- One `op inject` call for every cold task batch instead of one request per
  value
- Memory-only warm reads that avoid another 1Password call
- Task-scoped loading: each task requests only the environment names it needs
- Named overlays for deployment targets and other profiles
- Exact transport of multiline values, quotes, equals signs, Unicode, and
  trailing newlines
- Ordinary tracked mise tasks, with operator-specific wiring kept in ignored
  local configuration

The daemon starts on demand. Direct CLI commands remain available for setup,
diagnostics, cache control, and scripts that do not use mise.

## Requirements

- Go 1.27 or a released `op-agent` binary
- The [1Password CLI](https://developer.1password.com/docs/cli/)
- mise when using the environment plugin

Local Keychain setup is available on macOS. Linux and CI callers supply
`OP_SERVICE_ACCOUNT_TOKEN` and resolve each batch directly.

## Install

```sh
go install github.com/azohra/op-agent/cmd/op-agent@v0.1.2
```

[GitHub Releases](https://github.com/azohra/op-agent/releases) provides macOS
and Linux archives with a SHA-256 checksum manifest. A local mise configuration
can pin the released binary instead:

```toml
[tools]
"github:azohra/op-agent" = "0.1.2"
```

## Set up a local credential

The recommended first setup reads a service-account token through the signed-in
1Password desktop integration, stores it in Keychain, and installs the matching
mise plugin:

```sh
op-agent setup --from-ref 'op://vault/service-account/credential'
op-agent doctor
```

That first `op read` may show an interactive 1Password prompt. Runtime reads do
not use the desktop session. Setup is idempotent for the credential and
reconciles the plugin to the running binary's release. Use `--replace` when
rotating an existing credential. `--token-stdin` and an existing
`OP_SERVICE_ACCOUNT_TOKEN` are also supported when a desktop reference is not
appropriate.

Development builds install the plugin from the repository's current revision.
Use `--skip-mise-plugin` or `--mise-plugin-url` to manage that installation
explicitly.

## Use it with mise

Keep the repository's task portable and describe its normal environment
contract in the tracked `mise.toml`:

```toml
[tasks.deploy]
run = "./scripts/deploy"
```

The owner opts into `op-agent` from an ignored `mise.local.toml`. The map can
cover the complete environment—ordinary configuration as well as secrets—while
each task selects only the names it consumes:

```toml
[tools]
"github:azohra/op-agent" = "0.1.2"

[env]
OP_AGENT_REFS = """
DEPLOY_TOKEN op://vault/local-app/deploy-token
ACCOUNT_ID op://vault/shared/account-id
API_BASE_URL op://vault/local-app/api-base-url
"""

[tasks.deploy.env._]
op-agent = { keys = ["DEPLOY_TOKEN", "ACCOUNT_ID", "API_BASE_URL"] }
```

Named profiles apply one overlay to the base map. An overlay can replace base
references or add names that exist only in that profile:

```toml
[env]
OP_AGENT_REFS_PROD = """
DEPLOY_TOKEN op://vault/production-app/deploy-token
API_BASE_URL op://vault/production-app/api-base-url
"""

[tasks.deploy-prod.env._]
op-agent = { keys = ["DEPLOY_TOKEN", "ACCOUNT_ID", "API_BASE_URL"], profile = "prod" }
```

Profiles contain 1–32 lowercase letters, numbers, or hyphens and start with a
letter. The profile `prod-ca` reads `OP_AGENT_REFS_PROD_CA`. Selecting a
profile without a non-empty matching overlay is an error; it never falls back
silently to the base map.

`keys` selects environment variable names. `profile` selects the reference
overlay. `account` selects a separately stored service-account credential.
Because a batch can mix secrets and ordinary configuration, the plugin redacts
every returned value in mise output.

The plugin runs when mise resolves the environment for the task carrying the
directive. A protected task refuses to run when its mapping, selected overlay,
credential, or plugin setup is missing.

## Contributors and CI

The checked-in task remains a normal mise task. Contributors who do not use
`op-agent` can provide its documented environment through their own tooling;
they do not need the binary, plugin, reference maps, or a 1Password account.

CI should normally continue using its platform's environment and secret store.
If a workflow deliberately uses `op-agent`, install the binary and plugin in
that job, provide `OP_SERVICE_ACCOUNT_TOKEN`, and supply the reference maps in
the job environment. An explicit environment credential resolves directly and
is never copied into a daemon.

## Use it directly

The binary also provides direct commands:

```text
op-agent setup
op-agent doctor
op-agent env --keys NAME,OTHER [--profile NAME] --format json
op-agent read [--no-newline] op://vault/item/field
op-agent cache status
op-agent cache clear
op-agent cache stop
```

`env` also accepts `OP_AGENT_KEYS`, `OP_AGENT_PROFILE`, and
`OP_AGENT_ACCOUNT`.

Unknown commands, including `inject` and write operations, execute the native
`op` binary with the selected service-account credential. Stdout, stderr,
signals, and exit status remain native to that process. Clear the cache after a
write when a subsequent command must see the new value immediately:

```sh
op-agent item edit example field=value && op-agent cache clear
```

## Cache behavior

The daemon starts on the first cacheable read and exits after eight idle hours.
Entries expire after eight hours by default. Cache keys include both the
credential account and the complete reference. Cold requests are serialized
and rechecked so concurrent callers share one resolution instead of multiplying
1Password calls.

The mise plugin reports `cacheable = false`, keeping values out of mise's disk
cache. The memory daemon provides warm reuse.

`op-agent cache status` reports counts only. It never lists reference names or
values. `clear` and `stop` do not start a missing daemon.

The defaults can be changed with `OP_AGENT_CACHE_TTL` and
`OP_AGENT_IDLE_TTL`. `OP_AGENT_SOCKET` is available for isolated testing.

See [Architecture](docs/architecture.md) for the security and data-flow
contract.
