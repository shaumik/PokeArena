# Publishing PokéArena

An operator runbook for cutting a release and getting PokéArena in front of the
people (and agents) who go shopping for MCP servers.

Most of this is automated by [`.github/workflows/release.yml`](../.github/workflows/release.yml).
The parts that are not are the parts that need *your* credentials or *your*
GitHub identity — those are collected in [What still needs a human](#what-still-needs-a-human)
at the bottom.

---

## 0. The one-paragraph version

The repo has zero tags today, and that is the single thing blocking discovery.
The [official MCP Registry](https://registry.modelcontextprotocol.io) only lists
servers whose artifacts live on a GitHub/GitLab release, and pkg.go.dev only
shows a Go module once it has a semver tag. **Pushing `v0.1.0` fixes both at
once**: the release workflow builds the binaries, wraps `pokearena-mcp` in an
`.mcpb` bundle, attaches everything to the GitHub release, and publishes
`io.github.shaumik/pokearena` to the registry over GitHub OIDC — no secrets
required.

---

## 1. Cut `v0.1.0`

From a clean checkout of the commit you want to release:

```bash
# 1. Sanity: the tag must build and pass the same gates CI runs.
go build ./...
make lint
make test

# 2. Confirm you are on the commit you mean to ship.
git log --oneline -1
git status --porcelain          # must be empty

# 3. Annotated tag. GoReleaser reads the tag message into the release notes.
git tag -a v0.1.0 -m "PokéArena v0.1.0 — first public release"

# 4. Push it. This is the trigger; everything after is automatic.
git push origin v0.1.0
```

Watch it run:

```bash
gh run watch --repo shaumik/PokeArena
gh release view v0.1.0 --repo shaumik/PokeArena
```

**If you need to redo a tag** (only ever before anyone has downloaded it — the
MCP Registry treats a published version as immutable):

```bash
git tag -d v0.1.0
git push origin :refs/tags/v0.1.0
gh release delete v0.1.0 --repo shaumik/PokeArena --yes
# then re-tag and re-push
```

Once `v0.1.0` is published on the registry, the next release must be `v0.1.1`
or later — you cannot overwrite a version in place.

---

## 2. What the workflow does for you

`.github/workflows/release.yml` has two jobs.

### Job `release` — artifacts (permissions: `contents: write`)

1. **GoReleaser** (config: [`.goreleaser.yaml`](../.goreleaser.yaml)) cross-compiles
   three binaries for linux/darwin/windows × amd64/arm64, using the same flags
   as the Dockerfile (`CGO_ENABLED=0 -trimpath -ldflags="-s -w"`):

   | Package | Released binary |
   |---|---|
   | `./cmd/pokearena-mcp` | `pokearena-mcp` |
   | `./cmd/pokearena-agent` | `pokearena-agent` |
   | `./cmd/bench` | `pokearena-bench` — renamed on the way out so the artifact is self-describing in someone's `~/Downloads`; the package path stays `./cmd/bench` |

   All three ship in **one archive per platform** (`pokearena_0.1.0_darwin_arm64.tar.gz`,
   `…_windows_amd64.zip`, …) alongside `README.md`, `LICENSE`, and the two docs
   a newcomer needs. `checksums.txt` (sha256) covers every archive.

2. **`.mcpb` bundle.** `pokearena-mcp-0.1.0.mcpb` is an
   [MCP Bundle](https://github.com/anthropics/mcpb) — a zip with `manifest.json`
   at the root and the server binaries under `server/`. This is the artifact the
   MCP Registry entry points at.

   MCPB `manifest_version` 0.3 selects an executable **per OS but not per
   architecture**, so the bundle ships a macOS *universal* binary (arm64 + amd64
   fused by GoReleaser) plus linux/amd64 and windows/amd64. Linux and Windows
   **arm64** users should take the platform tarball instead of the bundle.

3. **Rendered `server.json`.** The committed `server.json` is a template: its
   `fileSha256` is a placeholder of 64 zeros and its `identifier` points at
   whatever the last release was. The workflow regenerates it with this tag's
   version, download URL, and real SHA-256, and uploads the result to the
   release as `server.json` so the published record is auditable next to the
   artifact it describes.

### Job `publish-registry` — the MCP Registry (permissions: `id-token: write`)

Downloads the official [`mcp-publisher`](https://github.com/modelcontextprotocol/registry)
CLI, then:

```
mcp-publisher validate server.published.json
mcp-publisher login github-oidc
mcp-publisher publish server.published.json
```

`login github-oidc` exchanges the GitHub Actions OIDC token for a registry
token. The registry grants the `io.github.shaumik/*` namespace because the OIDC
claim's `repository_owner` is `shaumik`. **There is no secret to configure** —
just the `id-token: write` permission, which the job already declares.

The job is deliberately *separate* from the artifact job and runs only on a real
tag push: if the registry is down or rejects the payload, the binaries are still
released, and you can re-run just the publish by hand (below). A
`workflow_dispatch` re-run rebuilds artifacts for an existing tag without
re-publishing to the registry.

---

## 3. Publishing to the MCP Registry by hand

Only needed if the `publish-registry` job fails, or you want to push a metadata
fix without cutting a new tag.

### 3.1 Install the CLI

```bash
# macOS / Linux
curl -L "https://github.com/modelcontextprotocol/registry/releases/latest/download/mcp-publisher_$(uname -s | tr '[:upper:]' '[:lower:]')_$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/').tar.gz" \
  | tar xz mcp-publisher && sudo mv mcp-publisher /usr/local/bin/

# or
brew install mcp-publisher

mcp-publisher --help
```

### 3.2 Fill in the real artifact hash

The committed `server.json` carries a placeholder hash. **Publishing it as-is
would ship a record whose hash never matches the download**, and MCP clients
verify that hash before running the bundle. Render a real one:

```bash
VERSION=0.1.0
gh release download "v${VERSION}" --repo shaumik/PokeArena \
  --pattern "pokearena-mcp-${VERSION}.mcpb" --dir /tmp

# macOS: shasum -a 256   /   Linux: sha256sum
SHA=$(shasum -a 256 "/tmp/pokearena-mcp-${VERSION}.mcpb" | cut -d' ' -f1)
URL="https://github.com/shaumik/PokeArena/releases/download/v${VERSION}/pokearena-mcp-${VERSION}.mcpb"

jq --arg v "$VERSION" --arg id "$URL" --arg sha "$SHA" '
  .version = $v
  | .packages = [
      .packages[]
      | if .registryType == "mcpb"
        then .identifier = $id | .version = $v | .fileSha256 = $sha
        else . end
    ]
' server.json > /tmp/server.published.json

mcp-publisher validate /tmp/server.published.json
```

(Or simply `gh release download v0.1.0 --pattern server.json` — the workflow
already uploaded the rendered file.)

### 3.3 Authenticate as `shaumik`

The server name is `io.github.shaumik/pokearena`, so the registry requires proof
that you control the GitHub account `shaumik`. Interactive device flow:

```bash
mcp-publisher login github
# → open https://github.com/login/device and enter the printed code
```

Nothing is scoped to the repo — the registry only reads your identity. It never
reads or writes your code.

### 3.4 Publish and verify

```bash
mcp-publisher publish /tmp/server.published.json

curl -s "https://registry.modelcontextprotocol.io/v0.1/servers?search=io.github.shaumik/pokearena" \
  | jq '.servers[] | {name, version}'
```

### Troubleshooting

| Error | Fix |
|---|---|
| `You do not have permission to publish this server` | You authenticated as someone other than `shaumik`. `mcp-publisher logout`, then `login github` again. |
| `Invalid or expired Registry JWT token` | Re-run `mcp-publisher login github`. |
| `invalid audience` | Your `mcp-publisher` binary predates the current registry deployment. Reinstall it. |
| `expected length <= 100` on `description` | The schema caps `description` at 100 characters. Keep it short. |
| Version already exists | Versions are immutable. Bump to `v0.1.1`. |

---

## 4. The `server.json` contract

Targeted schema — pinned in the file's `$schema` field:

```
https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json
```

Required top-level fields are exactly `name`, `description`, `version`; a
package (or remote) entry is required in practice — every server currently in
the registry has one. Each package requires `registryType`, `identifier`, and
`transport`.

References, all checked against the live registry:

- [server.json format specification](https://github.com/modelcontextprotocol/registry/blob/main/docs/reference/server-json/generic-server-json.md)
- [Official registry requirements](https://github.com/modelcontextprotocol/registry/blob/main/docs/reference/server-json/official-registry-requirements.md) — namespace auth, ownership verification, allowed registry base URLs
- [Supported package types](https://github.com/modelcontextprotocol/registry/blob/main/docs/modelcontextprotocol-io/package-types.mdx) — `npm`, `pypi`, `nuget`, `cargo`, `oci`, `mcpb`
- [Quickstart: publish a server](https://github.com/modelcontextprotocol/registry/blob/main/docs/modelcontextprotocol-io/quickstart.mdx)
- [Automate publishing with GitHub Actions](https://github.com/modelcontextprotocol/registry/blob/main/docs/modelcontextprotocol-io/github-actions.mdx)
- [About the registry](https://modelcontextprotocol.io/registry/about)

### Why `mcpb` and not something else

There is **no Go registry type**. The registry supports npm, PyPI, NuGet, Cargo,
OCI, and MCPB. For a compiled binary with no runtime, the registry's own
guidance is MCPB: *"prebuilt binary distributed via GitHub or GitLab Releases.
End users need no toolchain."* MCPB artifacts must be hosted on
`https://github.com` or `https://gitlab.com` releases, the URL must contain the
string `mcp` (ours does, twice), and the record must carry `fileSha256`.

An OCI image on `ghcr.io` is the other viable path and would additionally
require a `Dockerfile` carrying
`LABEL io.modelcontextprotocol.server.name="io.github.shaumik/pokearena"`.
Worth considering later if Docker-first users show up; it is not needed now.

### Optional hardening: pin the repository ID

`repository.id` lets the registry detect a delete-and-recreate of the repo.
It needs an authenticated API call, so it is not in the committed file:

```bash
ID=$(gh api repos/shaumik/PokeArena --jq '.id')
jq --arg id "$ID" '.repository.id = $id' server.json > server.tmp && mv server.tmp server.json
```

---

## 5. Where else to submit

### pkg.go.dev — automatic, no submission

pkg.go.dev indexes any public Go module the moment the module proxy sees a
semver tag. The module path is `github.com/shaumik/PokeArena` (note the capitals
— the proxy escapes them as `!poke!arena`). The proxy already knows the repo but
has **no tagged versions**, which is exactly why the docs page 404s today.

After pushing `v0.1.0`, nudge the proxy so indexing happens in minutes instead
of hours:

```bash
GOPROXY=https://proxy.golang.org GO111MODULE=on \
  go list -m github.com/shaumik/PokeArena@v0.1.0

# verify the proxy sees it
curl -s 'https://proxy.golang.org/github.com/shaumik/!poke!arena/@v/list'
```

Then check <https://pkg.go.dev/github.com/shaumik/PokeArena>. Nothing else is
required — there is no submission form.

### MCP directories

| Where | How to submit |
|---|---|
| **Official MCP Registry** | Automated by the release workflow. Browse: <https://registry.modelcontextprotocol.io/v0.1/servers?search=pokearena> |
| **punkpeye/awesome-mcp-servers** (the large one) | Pull request against <https://github.com/punkpeye/awesome-mcp-servers> — add one line under a fitting category. Read its `README.md` header for the exact line format before opening the PR. |
| **wong2/awesome-mcp-servers / mcpservers.org** | Does **not** take PRs. Use the web form at <https://mcpservers.org/submit> |
| **Glama** | <https://glama.ai/mcp/servers> — indexes public GitHub repos automatically; submit/claim from the site. |
| **Smithery** | <https://smithery.ai/new> — connect the GitHub repo. |
| **PulseMCP** | <https://www.pulsemcp.com/submit> |

For any of these, lead with the positioning that makes PokéArena *not* the 139th
PokéAPI wrapper: it is a **playable environment** — the agent takes a trainer
slot in a real 6v6 game under fog of war, against a human or another agent — and
it doubles as a reproducible benchmark.

### Claude Code plugin marketplace

Two different things, don't confuse them:

- **The official directory** (`anthropics/claude-plugins-official`) — submit via
  the form at <https://clau.de/plugin-directory-submission>. Entries are
  reviewed against quality and security standards.
- **Community directories** — e.g. <https://claudemarketplaces.com> and
  <https://www.claudepluginhub.com/marketplaces>.

Both expect a **plugin**, not just an MCP server: a repo containing
`.claude-plugin/plugin.json` plus an `.mcp.json` pointing at the server. That
scaffolding does not exist in this repo yet — see the checklist below. The
format is documented at
<https://code.claude.com/docs/en/plugin-marketplaces>.

---

## What still needs a human

Things this repo cannot do for you, roughly in priority order.

- [ ] **Push the first tag.** `git tag -a v0.1.0 … && git push origin v0.1.0`.
      Nothing else in this document matters until this happens. Requires push
      access to `shaumik/PokeArena`.
- [ ] **Confirm Actions can write releases.** Repo → Settings → Actions →
      General → *Workflow permissions*. The workflow requests `contents: write`
      per-job, which works under the default *read* setting — but if the org
      has disabled that, the release job fails on upload.
- [ ] **Watch the first `publish-registry` run.** OIDC needs no secret, but the
      first publish of a namespace is the one most likely to surprise. If it
      fails, fall back to §3 (interactive `mcp-publisher login github`) — that
      step needs your GitHub credentials and cannot be automated from CI.
- [ ] **Decide the public gateway story.** `POKEARENA_GATEWAY_URL` defaults to
      `ws://localhost:8080`, which means every registry visitor must run
      `docker compose up` before the server can do anything. If you stand up a
      hosted arena, change the default in `server.json` (and the MCPB
      `user_config.gateway_url` default in the release workflow) — that single
      change is the difference between "install and play" and "install, then
      read a README".
- [ ] **Submit to the directories in §5.** Each needs a GitHub account or a web
      form; none can be done from CI.
- [ ] **Optional: add `repository.id`** to `server.json` (§4) — one authenticated
      `gh api` call.
- [ ] **Optional: add Claude Code plugin scaffolding** (`.claude-plugin/plugin.json`
      + `.mcp.json`) if you want a listing in the plugin marketplaces rather
      than only the MCP registry.
- [ ] **Optional: sign / notarize the macOS binaries.** Deliberately out of
      scope here — it needs an Apple Developer ID and repository secrets. Until
      then, macOS users may need
      `xattr -d com.apple.quarantine ./pokearena-mcp` on first run; this is
      called out in the release notes.
