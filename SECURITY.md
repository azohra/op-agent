# Security

Please report vulnerabilities privately through GitHub's security advisory
form for this repository. Do not open a public issue with credential material,
secret references, or reproduction output that contains secrets.

`op-agent` stores its 1Password service-account credential in the operating
system credential store. Resolved environment values are held only in the
daemon's memory and expire. The daemon does not write values or references to
disk.

The operating-system user is the local trust boundary. Any process running as
that user can ask the daemon to resolve references available to the stored
service account. Scope that account narrowly, and treat output from `read`,
`env`, and native 1Password commands as secret-bearing.
