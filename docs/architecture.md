# Architecture

`op-agent` separates durable configuration, credential storage, resolution,
and short-lived acceleration. Each concern has one authority.

## Data flow

For a local mise task:

1. mise passes `keys`, `profile`, and `account` to the Lua plugin.
2. The plugin invokes the fixed command `op-agent env --format json`, carrying
   its options in child environment variables rather than shell arguments.
3. `op-agent` validates `OP_AGENT_REFS`, applies the selected named overlay,
   and selects the requested names. A selected profile without its matching
   `OP_AGENT_REFS_<PROFILE>` map fails closed.
4. The daemon returns unexpired values and sends all missing references through
   one cold resolver lane.
5. The resolver obtains the selected service-account credential from Keychain
   and makes one `op inject` call for the full missing set.
6. Only a complete batch is published to the memory cache. The CLI returns a
   JSON object and the plugin marks the complete returned environment for mise
   redaction.

When `OP_SERVICE_ACCOUNT_TOKEN` is already present, the CLI resolves directly.
It does not start a daemon or copy that credential into another process's
long-lived environment.

## Persistent state

The only credential persisted by `op-agent` is the service-account token stored
in the operating system credential store. Reference maps remain in the owner's
ignored mise configuration. They can describe ordinary configuration and
secrets stored together in 1Password. Resolved values are never written by
`op-agent`.

On macOS, setup uses the `security` batch interface so the token is supplied on
stdin rather than argv. The Keychain entry trusts `/usr/bin/security`, which is
the executable used for later retrieval.

## Daemon boundary

The daemon listens on a versioned Unix socket inside a directory accessible only
to the current user. Both endpoints verify the peer user ID. The socket is mode
`0600`; its directory is mode `0700` and must be owned by the current user.

The operating-system user is the trust boundary. Any process running as that
user can ask the daemon to resolve a reference permitted by the stored service
account. Use a service account scoped to the smallest practical set of vaults
and items.

The daemon starts with a small allowlisted environment. In particular, it does
not inherit `OP_SERVICE_ACCOUNT_TOKEN`, resolved task variables, or the
reference map that caused it to start. It retrieves a credential only while
performing a cold resolution and passes that credential only to the short-lived
`op` child process.

Status responses contain entry and account counts. They do not include cache
keys, references, values, or per-entry timing.

## Batch integrity

Each cold batch uses cryptographically random, per-entry boundary markers in a
single `op inject` template. Parsing is positional and does not use `eval`,
dotenv syntax, line splitting, or shell assignment. This preserves arbitrary
string content without treating the value as executable input.

The cache update is atomic at the batch boundary. A command failure, missing
marker, or incomplete resolver response publishes none of that batch.

## Process behavior

Simple `read` and structured `env` calls are cache-aware. More complex native
1Password operations use process replacement, so native output, errors,
signals, and exit codes are preserved instead of emulated.

Native writes do not invalidate the memory cache automatically. Call
`op-agent cache clear` after a write when later reads must observe the change
before the normal expiry time.
