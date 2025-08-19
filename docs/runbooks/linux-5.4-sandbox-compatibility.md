# Linux 5.4 sandbox compatibility and policy authoring

This document explains compatibility constraints for Linux 5.4-era kernels, supported network modes and their caveats, how the execution bundle works (including inclusion of `agentcli`), how to author sandbox policies, and known limitations of rootless operation.

## Why this matters

Many servers and CI runners still use Linux 5.4 LTS. Certain modern sandboxing features like Landlock are unavailable, and user namespaces behave differently across distributions. This guide documents a safe, portable baseline.

## Kernel constraints (Linux ≥ 5.4)

- No Landlock: do not rely on Landlock file restrictions. Use chroot + bind mounts instead.
- No overlayfs-in-userns: avoid overlayfs inside unprivileged user namespaces. Use bind mounts and tmpfs.
- User namespaces: require unprivileged user namespaces enabled (`kernel.unprivileged_userns_clone=1`).
- Seccomp: available and recommended for syscall filtering, but ensure your kernel config includes seccomp.
- CLONE_NEWNET: unprivileged network namespaces may be disabled by distro; detect at runtime and fall back.

### Troubleshooting kernel prerequisites

- Enable user namespaces:
  - Ubuntu/Debian: `sudo sysctl -w kernel.unprivileged_userns_clone=1 && echo kernel.unprivileged_userns_clone=1 | sudo tee /etc/sysctl.d/99-userns.conf`
- Check seccomp: `grep SECCOMP /boot/config-$(uname -r)` should show `CONFIG_SECCOMP=y` and `CONFIG_SECCOMP_FILTER=y`.
- Check newnet: attempt `unshare -n true`; if it fails with EPERM, network namespaces for unprivileged users are blocked.

## Network modes

- off: no network allowed. Prefer this for tools that do not need network access.
- allow_all: do not create a network namespace; outbound egress allowed subject to host firewall. Less strict.
- proxy_allowlist: route traffic via an HTTP(S) proxy that enforces destination allowlists. Use when selective egress is required on hosts without unprivileged `CLONE_NEWNET`.

Caveats:
- On systems without unprivileged network namespaces, `off` still works; `proxy_allowlist` requires a reachable proxy.
- DNS resolution and time may vary by container/VM; prefer explicit IP:port and TLS verification where possible.

## Bundle assembly overview

On each run, `agentcli` assembles a minimal bundle directory with subdirs `bin`, `etc`, `in`, `out`, `tmp`. It copies exactly allowlisted tool executables and a copy of the current `agentcli` binary into `bin`. Only `/bundle/bin` is executable; other mounts are `noexec`.

- Verify executables are regular files, reject symlinks.
- Enforce ELF arch matches the running OS and architecture.
- Cap total bundle size to prevent abuse.
- Generate a manifest with file paths, sizes, and SHA-256 hashes.

## Policy authoring

A deny-by-default sandbox policy defines filesystem, environment, resources, and network:

```json
{
  "filesystem": {
    "bundle": { "binaries": [ "/usr/bin/jq" ] },
    "inputs": [ { "host": "/etc/ssl/certs/ca-certificates.crt", "guest": "/etc/ssl/certs/ca.pem" } ],
    "outputs": [ "/out/result.json" ]
  },
  "env": { "allow": [ "TZ", "HTTP_PROXY=http://127.0.0.1:8080" ] },
  "resources": { "timeoutSec": 10, "max_output_bytes": 1048576, "rlimits": { "NOFILE": 256, "NPROC": 64, "AS": 268435456 } },
  "network": { "mode": "off", "allow": [] },
  "audit": { "redact": [ "OAI_API_KEY" ] }
}
```

Guidelines:
- Only list absolute host paths under `filesystem.bundle.binaries` and ensure they are static binaries when possible.
- Use `filesystem.inputs[]` for read-only mounts; avoid mounting broad directories.
- Constrain outputs to a small set under `/out`.
- Keep env allowlist minimal. Prefer literal `NAME=value` for one-off overrides.
- Set tight timeouts and rlimits appropriate for the tool.
- Prefer `network.mode: off` unless network is essential.

## Rootless limitations

- cgroups: only available if delegated by the system; otherwise CPU/memory limits may be advisory only.
- Mounts: require user namespaces; without them, mount operations will fail for unprivileged users.
- No raw sockets, no ptrace, limited `fork()` scale; design tools to be short-lived and modest in resource consumption.

## Copy-paste examples

- Minimal offline run with no network and one input file:

```bash
cat > policy.json <<'JSON'
{
  "filesystem": {
    "bundle": { "binaries": [ "/usr/bin/jq" ] },
    "inputs": [ { "host": "$(pwd)/in.json", "guest": "/in/in.json" } ],
    "outputs": [ "/out/out.json" ]
  },
  "env": { "allow": [ "TZ=UTC" ] },
  "resources": { "timeoutSec": 5, "rlimits": { "NOFILE": 128 } },
  "network": { "mode": "off" }
}
JSON

./bin/agentcli -prompt 'Process input with jq' -tools ./tools.json -debug
```

- Proxy allowlist mode sketch:

```bash
export HTTPS_PROXY=http://127.0.0.1:8080
# Proxy must enforce destination allowlists externally.
```

## Troubleshooting checklist

- EPERM creating namespaces: switch to `allow_all` or `proxy_allowlist`; document risk.
- Read-only filesystem errors: write only under `/out` or `/tmp`.
- Missing binary dependencies: rebuild tools statically or enable dynamic dependency copying in the bundle.
- Timeouts: increase `resources.timeoutSec` conservatively; investigate tool performance.
