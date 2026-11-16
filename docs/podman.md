# Podman support

This page is for users whose machine runs **podman** instead of Docker — most
commonly, a Mac with `podman machine start` but no Docker daemon, or a Linux
host with podman installed but no dockerd. Running Man can supervise such a
project end-to-end once `runtime: podman` is wired through; the steps
required on the way are described here.

## Why this exists

The Docker daemon, the moby Go client, and the standard
`docker-compose-plugin` all share assumptions that stop on a host without
dockerd:

- The moby client speaks `/v1.40/...` over a Unix socket that dockerd owns.
  Without dockerd the socket does not exist.
- The `docker-compose` CLI (v2 plugin) ships with Docker Desktop, and
  fails to start when invoked on a host without dockerd, because the plugin
  proxies through the same socket.
- Podman-machine exposes an equivalent socket at the podman-machine level
  (`podman system info --format {{.Host.RemoteSocket.Path}}` gives the path),
  but its event-stream and log-stream responses diverge from docker's in
  shape, so moby's Go client does not parse them cleanly.

For these hosts, Running Man reads and writes through a **dedicated podman
adapter**: same `Networking.List/Inspect/Logs/Events/...` interface as the
docker side, same verb-by-verb meaning, plus the differences called out in
[Limits](#limits).

## Requirements

- A working `podman` installation — brew-managed
  (`brew install podman`) on macOS, distro package on Linux.
- A running virtual machine on macOS:
  ```bash
  podman machine init
  podman machine start
  ```
  Confirm with `podman ps`; if a hello-world container comes up, the
  machine is healthy.
- On Linux, podman runs rootless by default — no machine layer required.
  The socket path differs; check `podman info` and look for
  `host.remoteSocket.path`.
- The compose plugin or the standalone `podman-compose` binary
  (`brew install podman-compose` on macOS). Either works; the adapter
  prefers the plugin.

## Tested on

Tested in development on `podman 5.x` on macOS applehv machines; the
`/v1.40/...` paths the adapter targets are fixed as of podman 5.0 and
remain stable in 5.x. Earlier podman releases may move endpoints under
`/libpod/...`; the adapter does not currently support those.

## Configuration

Set **runtime: podman** in `running-man.yml`, or override on the command
line:

```yaml
runtime: podman
```

```bash
running-man run --runtime=podman
```

The adapter reads the podman socket path from
`podman system info --format "{{.Host.RemoteSocket.Path}}"` once per
instance. Calls that rely on a non-empty socket — `List`, `Inspect`,
`Logs`, `Events`, `Wait` — fail fast if podman is not installed or the
machine is not running.

## Limits

Podman and Docker responses differ in three places that affect the
Running Man supervisor:

| Endpoint | Docker shape | Podman shape | Adapter behaviour |
| --- | --- | --- | --- |
| `/containers/{id}/logs` | Multiplexed stdout/stderr frames (8-byte header per frame; type byte = 1 = stdout or 2 = stderr). | Same shape, but a stream of zero-length frames means "container crashed" on podman. | Reuses moby's `stdcopy`. Empty frame handling stays as-is. |
| `/events` | JSON object per message; `Action` is an enum string. | NDJSON: one JSON object per line, `Action` is sometimes an enum, sometimes a free-form string (e.g. `died`, `container_died`). | Adapter reads with `bufio.Scanner`; non-container types filtered. |
| `/containers/json` | `Id` truncated to 12 chars in the wire response. | Podman returns the full sha256; prefix must be taken locally. | `shortID` helper called on every record. |

Three smaller differences worth knowing:

- Podman containers may carry image digests that moby containers do not.
  The `Container.Image` field holds whatever the runtime reports, with
  the same field layout.
- `Connected` / `Running` flags on inspect are present on docker but
  not surface-stable on podman. The adapter reads `State.Status` and
  `State.ExitCode` only; live-state inference is slower and is best
  done against podman's `/events` stream.
- The `Wait` operation maps to `/containers/{id}/wait`; only the
  `condition=not-running` value is honoured. Any other value surfaces
  the daemon's error verbatim.

## Troubleshooting

**`podman system info returned no RemoteSocket.Path`** — podman is
installed but the machine is not running. `podman machine start` first.
On Linux, rootless socket discovery may need a `DOCKER_HOST`-style
override; report an issue if so.

**`/v1.40/containers/json: connection refused`** — the supervisor found
a socket path but the daemon at the path refused the connection. On
macOS this is almost always the VM having been stopped or restarted
since the supervisor last cached. Reset the cache by restarting the
Running Man instance (the `instance.json` marker also resets).

**`/events: HTTP 404`** — podman version mismatch. Podman 5.0 dropped a
few intermediate paths; verify with `podman --version`. Versions
earlier than 5.0 are not supported by Running Man today.

**`/containers/{id}/logs: stream truncated at first line`** —
symptomatic of a TTY container; Running Man inspects the container
before streaming and treats TTY streams as raw, so a non-TTY container
will demultiplex correctly. If truncation persists, file an issue
with `podman inspect <id>` output attached.

## What's still on the docker side

The configuration flag parses today; the supervisor's existing
container-discovery, log-streaming and event-stream paths inside
`cmd/running-man/main.go` still direct-moby against `*runtime.Client`
and are bridged through the `Runtime` interface only at the discovery
and integrate-test edges. The end-to-end call into the podman adapter
from `cmd/running-man/main.go` follows in a follow-up commit; setting
`runtime: podman` today produces a stderr fallback message and runs
against docker instead. The vocabulary, the configuration surface,
the flag, and the wired podman adapter are in place so the dispatch
is a small addition, not a redesign.
