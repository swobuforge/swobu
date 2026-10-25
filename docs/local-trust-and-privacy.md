# Local trust and privacy contract

Swobu is a local-first daemon with a **trusted local host perimeter**.
This is a deployment assumption, not a claim that untrusted host processes can
be isolated by file mode, DACL, or loopback checks. **Any process on the host
that can connect to the local Control listener can exercise native Node control**
if it sends an admitted non-browser request; loopback is not an OS-user identity
check. This is intentional for the single-operator deployment, not multi-user
isolation.

## Trust and non-goals

- **Trusted:** the entire host-local control perimeter, including host CLI,
  scripts and other processes with access to loopback. Same-user processes can
  also read or alter the daemon's state files; other local OS users may not
  read those files, but can still reach loopback Control. Swobu does not claim
  an additional local authorization or privacy boundary among them.
- **Untrusted:** remote HTTP/MCP clients, other Workspace Principals, arbitrary
  upstream providers, client-supplied prompts/tools, and websites visited in a
  browser. None gains Node control merely by sharing a host-facing URL or a
  Workspace credential.
- **Not supported as an isolation deployment:** any untrusted process that
  can access the same loopback network namespace, including another OS account
  on a shared host; untrusted processes with access to daemon files; or a host
  operator modifying SQLite while the daemon runs. For untrusted local workloads,
  use a separate host or an isolated network namespace/container **without
  access to the Node's Control listener**. OS account separation alone is not
  sufficient for a loopback Control listener.

## Consequences for implementation

1. New managed directories and files request restrictive permissions (`0700`,
   `0600` where meaningful). Existing local paths are used according to the
   operating system's access decision; Swobu does not inspect ACLs or reject
   legal configurations solely because a POSIX mode/Windows DACL looks broad.
   Normal atomic saves of Swobu-owned state publish the file with the standard
   restrictive creation mode; that is not a preflight or repair lifecycle.
   Connect preserves existing third-party file modes and backups. An operator
   controls their own filesystem layout and backup policy.
2. Local filesystem operations still check **correctness**: regular-file type,
   durable marker identity, exact reviewed intent, size bounds, atomic commit,
   and restart recovery. No ACL preflight or symlink traversal routine may be
   justified as protection against same-user local attackers.
3. Configuration authority remains singular: the daemon's managed writer or
   explicitly read-only YAML. Read-only YAML never receives runtime writes;
   retained state is colocated under the canonical Node runtime root.
4. **Remote boundaries remain strict.** Every Workspace request uses current
   Principal/Service admission; Admin MCP is an ordinary Workspace MCP source,
   not a bypass. Access to `/workspaces/{slug}/mcp` does not grant Node control
   unless that Workspace deliberately exposes administrative tools. Publication,
   revocation, and limits remain authoritative at dispatch time.
5. **Secrets remain out of the protocol:** source credentials and one-shot
   bearer tokens are never recorded in logs, telemetry, exported Activity,
   or model-visible MCP/tool results. Persist verifier digests for issued access
   credentials; handle cleartext only at an authorized issuance/connection
   boundary. Do not confuse trusted local file access with permission to send
   secrets to remote callers or LLM contexts.
6. The browser is **not** automatically trusted merely because it connects to
   loopback. Keep Origin/Host/CSRF or equivalent local Control request admission
   that prevents arbitrary websites from inducing Node mutations. Loopback
   requests from approved local clients can use host authority; cross-origin
   web requests cannot acquire it by ambient browser network access.
7. Inference-time MCP grafting is explicit and separate from Workspace
   authorization. Tool visibility is neither authority nor a security boundary.

## Review litmus test

Every new privacy/security check must identify the **actual untrusted actor**
that it stops. If the actor already executes as the local operator, delete the
check rather than inventing a new local authorization plane. Keep fixes for
proven accidental corruption, remote privilege escalation, and secret leakage.
