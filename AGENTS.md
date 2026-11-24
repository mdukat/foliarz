# Foliarz

Foliarz is a TUI AI Agent.

## Language

The project is written entirely in Go.

## Architecture

The codebase is modular: adding a new tool for the agent to call should be as
easy as creating a single new source file.

| Path | Purpose |
|---|---|
| `main.go` | Entrypoint; resolves the working directory, loads configuration and starts the TUI. |
| `internal/paths` | Resolves and creates the Foliarz directories. |
| `internal/config` | Loads `foliarz.json`, `models.json` and `system-prompt.md`, merges the built-in model catalog, applies overrides and resolves compaction settings. |
| `internal/session` | Session state and the JSON session store. |
| `internal/thinking` | Thinking levels and cycling. |
| `internal/commands` | Slash commands; one command per source file. |
| `internal/projectcontext` | Loads `AGENTS.md` project instructions for a session. |
| `internal/logging` | File logger under `~/.local/foliarz/foliarz.log`. |
| `internal/skills` | Loads Pi-compatible `SKILL.md` skills from the config directory at startup; no runtime reload by design. |
| `internal/tools` | Model tools; one tool per source file. |
| `internal/llm` | OpenAI-compatible chat completions client. |
| `internal/agent` | Model requests and the tool-call loop. |
| `internal/buildinfo` | Reports the build version and commit. |
| `internal/tui` | Bubble Tea TUI: header, chat area, input bar and status bar. |
| `docs` | Project documentation. |

Shared tool infrastructure lives in `internal/tools/tools.go` (registry and
working-directory context), `internal/tools/path.go` (path resolution and
sandboxing) and `internal/tools/helpers.go` (output limits and truncation). A
new tool still only needs one new tool file.

The TUI talks to the model through `tui.Responder` and compacts context through
`tui.Compactor`; `main` builds both for the active provider/model. Session
switches rebuild them through `tui.WithResponderFactory`. Tool calls receive the
session working directory and session ID through the context.

## Adding or changing features

- **Tools:** add one file in `internal/tools`, register it from `init()`, set
  `ReadOnly` correctly, resolve paths with `resolvePathInWorkDir`, and add tests.
  FOLIA exposes only read-only tools.
- **Commands:** add one file in `internal/commands`, register it from `init()`,
  return a `commands.Action`, and handle the action in `internal/tui`. Update
  the command tests and docs.
- **Models/providers:** update `internal/config/catalog.json` and
  `internal/config/defaults/models.json`, then update `docs/models.md`.
- **TUI/session behavior:** keep `Responder`, `Compactor`, approval events,
  steering (`agent.MessageQueue`), session autosave and the chat cache
  invalidation rules consistent. Session
  data must remain crash-safe and must never contain API keys.
- **Tests:** add focused tests next to the package; run `make check` before
  committing. `go test -race ./...` is also expected for concurrency changes.
- **No CI:** the project is validated locally; keep `make check` green.
- **Docs:** update the matching file in `docs/`, this file, and `README.md`
  when user-visible behavior changes.

## Files and directories

| Path | Purpose |
|---|---|
| `~/.config/foliarz/foliarz.json` | TUI-scope configuration. |
| `~/.config/foliarz/models.json` | Saved models and their API configuration. |
| `~/.config/foliarz/system-prompt.md` | System prompt sent with every model request. |
| `~/.config/foliarz/skills/` | Pi-compatible skills, discovered recursively at startup and reloaded only on restart by design. |
| `<working directory>/AGENTS.md` | Project instructions added to the model context. |
| `~/.local/foliarz/` | Logs and session files. |
| `~/.local/foliarz/foliarz.log` | Application log file. |
| `~/.local/foliarz/sessions/<uuid>.json` | One JSON file per session, identified by UUID. |

Missing directories are created on startup. Missing configuration files are
created with defaults; Foliarz never replaces existing settings with defaults.
It updates only the last-used fields in `foliarz.json` when they change.

User data is not part of the repository: never commit API keys, session files,
logs, or skill contents from `~/.config/foliarz` or `~/.local/foliarz`.

## Documentation

- [Configuration](docs/configuration.md): directories, `foliarz.json` and
  environment variable interpolation.
- [Models](docs/models.md): built-in catalog, `models.json` reference and model
  overrides.
- [Sessions](docs/sessions.md): session file location and JSON format.
- [Context compaction](docs/compaction.md): automatic and manual compaction.
- [Terminal UI](docs/tui.md): layout and key bindings.
- [Modes](docs/modes.md): FOLIA, NORMAL and YOLO tool modes.
- [Project instructions](docs/project-context.md): loading `AGENTS.md`.
- [Commands](docs/commands.md): slash commands and how to add one.
- [Tools](docs/tools.md): model tools and how to add one.
- [Skills](docs/skills.md): Pi-compatible skills and FOLIA safety.
- [Troubleshooting](docs/troubleshooting.md): common startup, API and tool issues.

## Development

- Build (static): `make build`
- Test: `make test`
- Vet: `make vet`
- Format: `make fmt`
- Check before committing: `make check`

`make build` sets `CGO_ENABLED=0`, so the resulting binary is statically linked
and runs on both musl-based (Alpine) and glibc-based (Debian) Linux systems. A
plain `go build` may link against the host libc and is not portable between
them.

## Release

- Run `make check` and `make build`.
- The binary is static and portable across musl and glibc systems.
- `internal/buildinfo` reports the build version and commit; a release tag
  supplies the version through Go's build information.

## License

MIT, copyright Mateusz Dukat. See `LICENSE`.

## Development workflow

- Work atomically: make small changes, not huge ones.
- Commit changes atomically.
- If a request contains multiple changes or features, split them into atomic
  commits and work on them one by one.
- Keep the documentation in `docs/` and this file up to date when behavior
  changes.