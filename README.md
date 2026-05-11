# bpm — Bnlang Package Manager

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](./LICENSE)

`bpm` is the official package manager for the [Bnlang](https://bnlang.dev) programming language. It installs and publishes both **pure-bnl libraries** and **native plugins** (`.dll` / `.so` / `.dylib`), backed by a versioned registry with integrity-checked downloads.

- **Single binary** — Go-based, no runtime dependencies.
- **Reproducible** — every install writes a `bnl.lock` that pins direct + transitive deps to a concrete version plus a SHA-256 integrity hash.
- **Semver resolution** — `^1.2.3`, `~1.2`, `>=1.0 <2`, and exact `1.2.3` all work.
- **Native plugin friendly** — publish a single source package; resolves per-platform on install (`windows-x64`, `linux-x64`, `darwin-arm64`, …).
- **Tokens for CI** — long-lived bearer tokens scoped per machine, listable and revocable from the CLI.

---

## Install

### From a release

Download the latest `bpm` binary for your platform from the [Bnlang releases page](https://bnlang.dev/en/releases) and put it on your `PATH`.

### Build from source

Requires Go 1.23 or newer.

```sh
git clone https://github.com/bnlang/bpm.git
cd bpm
go build -o bpm        # produces ./bpm on Linux/macOS, .\bpm.exe on Windows
```

Verify:

```sh
bpm --help
```

---

## Quick start

```sh
# 1. (Optional) Point bpm at a different registry. The default is the
#    official one at https://bpm.bnlang.dev — set BPM_REGISTRY to override.
export BPM_REGISTRY=https://bpm.bnlang.dev

# 2. Authenticate (interactive — email + password, stored as a bearer token
#    under ~/.bnl/auth.json with mode 0600).
bpm login

# 3. Inside any directory:
bpm init                          # write a starter bnl.json

# 4. Add and install a dependency.
bpm install some-package
bpm install some-package@1.4.0    # pinned exact version
bpm install -g some-package       # install globally under ~/.bnl/deps/

# 5. Use it from bnl code.
#    src/main.bnl:
#        import "some-package" as pkg;
#        print(pkg.hello());

bnl src/main.bnl
```

To install everything declared in an existing project's `bnl.json` (e.g. on a fresh checkout):

```sh
bpm install
```

---

## Commands

| Command | Purpose |
|---|---|
| `bpm init` | Create a starter `bnl.json` in the current directory. |
| `bpm install` | Install every dependency listed in `bnl.json`. |
| `bpm install <name>[@version]` | Add and install one package; pin into `bnl.json`. |
| `bpm install -g <name>` | Install globally under `~/.bnl/deps/<name>/`. |
| `bpm uninstall <name> [-g]` | Remove an installed dependency. |
| `bpm list [-g]` | Show installed packages with their versions. |
| `bpm signup` | Create an account on the active registry. |
| `bpm login` | Authenticate with the registry. Prompts for email + password. |
| `bpm logout` | Forget the stored token for the active registry. |
| `bpm publish` | Publish the current package. Auto-detects pure-bnl vs. native. |
| `bpm publish --platform <os-arch> --binary <path>` | Publish a single native-plugin platform manually. |
| `bpm token create --label <label>` | Mint a CI token scoped to a label. |
| `bpm token list` | List your active tokens (id + label only, never the secret). |
| `bpm token revoke <id>` | Revoke a token. |

Global flags accepted by every command:

- `--registry <url>` — override the registry for this invocation (overrides `BPM_REGISTRY`).
- `-q`, `--quiet` — suppress non-error output.

---

## The `bnl.json` manifest

```json
{
    "name": "mathx",
    "version": "1.2.0",
    "description": "Vector and matrix helpers.",
    "license": "MIT",
    "homepage": "https://github.com/example/mathx",
    "repository": "https://github.com/example/mathx",
    "main": "src/index.bnl",
    "dependencies": {
        "core-utils": "^1.0.0",
        "logger":     "~0.4"
    }
}
```

Required: `name`. Everything else is optional; only `name`, `main`, and `native` are read by the Bnlang runtime itself — bpm uses the rest for the registry.

### Native plugins — `targets`

When publishing a C/C++ FFI plugin, declare the freshly-built binary path per platform. bpm picks the right one at install time and renames it to a canonical filename:

```json
{
    "name": "mathx-plugin",
    "version": "0.3.1",
    "targets": {
        "windows-x64":   "build/windows/mathx.dll",
        "linux-x64":     "build/linux/mathx.so",
        "darwin-arm64":  "build/darwin/mathx.dylib"
    }
}
```

Every other regular file in the binary's directory is shipped alongside (`sqlite3.dll`, `libssl.dylib`, etc.). Use a `.bpmignore` file (gitignore syntax) to exclude `.pdb`, `.ilk`, intermediate build artifacts, and similar noise.

Supported platform strings: `windows-x64`, `windows-x86`, `linux-x64`, `linux-arm64`, `darwin-x64`, `darwin-arm64`.

The installed manifest (under `deps/<name>/bnl.json`) replaces `targets` with a single `native: "<canonical>"` entry — the runtime looks at exactly that field.

---

## The `bnl.lock` lockfile

`bpm install` generates `bnl.lock` next to `bnl.json`. It pins **every** direct and transitive dependency to a concrete version plus a SHA-256 hash of the package tarball:

```json
{
    "packages": {
        "logger": {
            "version":   "0.4.7",
            "integrity": "sha256-abcd1234…",
            "deps":      { "core-utils": "^1.0.0" }
        }
    }
}
```

**Commit both `bnl.json` and `bnl.lock`.** The lockfile is what makes someone else's `bpm install` reproduce yours; the manifest alone resolves to *latest matching*, which drifts over time.

---

## Authentication

`bpm login` stores a bearer token in `~/.bnl/auth.json` (mode `0600`), keyed by registry URL:

```json
{
    "https://bpm.bnlang.dev": {
        "token": "bpm_…",
        "email": "you@example.com"
    }
}
```

For CI / publish automation, mint a dedicated token instead of using your password-derived session token:

```sh
bpm token create --label github-actions
#   bpm_eyJhbGciOi…                ← copy this, store as a CI secret

# In CI:
export BPM_TOKEN=bpm_eyJhbGciOi…
bpm publish
```

`bpm token list` and `bpm token revoke <id>` round out the lifecycle.

---

## Files bpm touches

| Path | Purpose |
|---|---|
| `./bnl.json` | Project manifest. |
| `./bnl.lock` | Generated lockfile. **Commit it.** |
| `./deps/<name>/` | Project-local installed packages. |
| `~/.bnl/deps/<name>/` | Global installs (from `bpm install -g`). |
| `~/.bnl/auth.json` | Registry bearer tokens. `chmod 600`. |
| `./.bpmignore` | gitignore-syntax exclude list at publish time. |

`bpm` never writes outside these paths.

---

## Environment variables

- `BPM_REGISTRY` — default registry URL (overridden per command by `--registry`).
- `BPM_TOKEN` — bearer token used for the current invocation. Takes precedence over `~/.bnl/auth.json`. Useful for CI.

---

## Project layout

```
bpm/
├── cmd/             # cobra command handlers (one file per top-level subcommand)
├── internal/
│   ├── archive/     # tarball creation + extraction
│   ├── auth/        # ~/.bnl/auth.json read/write
│   ├── lockfile/    # bnl.lock read/write
│   ├── manifest/    # bnl.json read/write
│   ├── paths/       # ~/.bnl/, deps/, …
│   ├── platform/    # windows-x64 / linux-x64 / darwin-arm64 / …
│   ├── registry/    # HTTP client for the registry API
│   ├── resolver/    # semver constraint resolution
│   └── semvr/       # thin wrapper around Masterminds/semver
├── main.go
├── go.mod
└── README.md
```

---

## Contributing

Pull requests welcome. For larger changes, please open an issue first to discuss the approach.

- Stick to the existing Go style (`gofmt`, `goimports`).
- Each subcommand has its own file under `cmd/`; new commands follow the same pattern.
- New `internal/` packages should have a doc.go-style comment at the top explaining the package's role.

---

## License

MIT — see [LICENSE](./LICENSE).

Copyright © 2025 Bnlang | Mamun
