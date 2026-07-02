# GitVis: Keyboard-First Git Navigation

A locally run desktop app that renders a repository's git history as a scrollable DAG graph, and utilizes a command palette to perform common git actions.


## MVP Supported Actions

The command palette is context-aware: the commands it shows depend on what's
selected and the repository's current state. 

**Global Level**
- Jump to HEAD (`G`)
- Search commits (`/`)
- Refresh graph (`R`)
- Open a different repo (`Ctrl+O`)
- Fetch from origin, with an optional prune
- Stash changes (with an optional message), stash pop, stash drop
- Rebase: continue / abort
- Cherry-pick: abort

**Commit Level**
- Copy SHA (`Y`)
- Checkout (detached)
- Branch from here — create, or create and check out
- Tag this commit
- Cherry-pick onto the current branch
- Revert this commit
- Reset `--soft` / `--mixed` / `--hard` to this commit
- Delete a tag

**Branch Level**
- Checkout a branch
- Merge a branch into the current one (merge or squash)
- Rebase the current branch onto another
- Rename the current branch
- Delete a branch


## Try It Out !

**Prerequisites:** [Node.js](https://nodejs.org), [Go](https://go.dev),
[Rust](https://rustup.rs) (for Tauri).

```bash
# 1. install frontend dependencies
cd frontend
npm install

# 2. build the Go backend sidecar (outputs to frontend/src-tauri/binaries/)
cd ..
./scripts/build_backend.sh

# 3. run the app in development
npx @tauri-apps/cli dev
```

Rebuild the backend any time the Go code changes with `./scripts/build_backend.sh`.
The script detects your Rust host triple and names the binary accordingly so Tauri
can pick it up as a sidecar.


## How It Works

The app is packaged with **Tauri**, where a **Go** backend is bundled as a sidecar binary that Tauri spawns on
startup. The backend reads git data, computes the graph layout, and serves it over
HTTP on `127.0.0.1:7832`. Thus, despite maintaining clean client-server separation, everything runs locally with no need for an internet connection, external servers, or accounts.


### Backend

Written in Go using [`go-git`](https://github.com/go-git/go-git). On launch it opens
the repo, walks every ref, topologically sorts all reachable commits (Kahn's
algorithm, timestamp tie-breaking), and assigns each commit a **lane** (column) and
**row** (vertical position). The layout is served over HTTP:

| Endpoint | Purpose |
|---|---|
| `GET /ping` | Health check → `pong` |
| `GET /graph?limit=500` | Graph rows as NDJSON, one JSON object per line |
| `GET /refs` | Branches, tags, HEAD, any in-progress operation |
| `GET /commit/:sha` | Full metadata for one commit |
| `GET /search?q=…` | Fuzzy search over commit messages and authors |
| `POST /action` | Execute a git operation (checkout, branch, revert, …) |
| `POST /open` | Switch to a different repository at runtime |

Large repos load in two phases: the first 500 commits make the server ready
immediately, and the full history streams in from a background goroutine. A file
watcher on `.git/` keeps the ref index live.


### Frontend

React 19 + [Zustand](https://github.com/pmndrs/zustand) for state. The graph is drawn
on a single HTML `<canvas>`. All
backend calls go through `src/api/client.ts`; no component calls `fetch` directly.
