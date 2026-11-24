# Foliarz

Foliarz is a terminal AI coding agent written in Go. It runs as a full-screen
TUI, streams the model's thinking and answers, can call tools to inspect and
change files, keeps a persistent session history, and loads project instructions
from `AGENTS.md`.

The agent is designed around three modes:

- **FOLIA** — read-only tools only. Foliarz always starts here.
- **NORMAL** — all tools, but non-read-only calls require your approval.
- **YOLO** — all tools run without asking.

## Features

- Streaming chat with markdown-rendered answers and gray thinking blocks.
- Tool calls shown live with success/error/pending backgrounds, expandable with
  `ctrl+o`.
- Tools: `datetime`, `read`, `ls`, `write`, `edit`, `bash`.
- File tools are sandboxed to the session working directory.
- `bash` requires an explicit timeout and supports cancellation.
- Automatic session saving, crash-safe JSON session files, and a `/session`
  picker that restores the transcript and working directory.
- Automatic context compaction with Pi-style structured summaries and a
  `/compact` command.
- `AGENTS.md` project instructions loaded into the model context.
- Pi-compatible skills from `~/.config/foliarz/skills`, with FOLIA shell
  protection and startup display.
- Configurable OpenRouter-compatible models with model overrides and a `/model`
  picker.
- Per-session spend in the status bar, priced from model cost rates or
  OpenRouter's reported cost.
- Static single-binary build suitable for Alpine (musl) and Debian (glibc).

## Requirements

- **Go 1.26 or newer** to build.
- An API key for your model provider. The default configuration uses OpenRouter
  with `deepseek/deepseek-v4.1-flash`.
- A terminal with ANSI color support (truecolor recommended).

## Installation

Build the project:

```sh
git clone https://github.com/mdukat/foliarz.git
cd foliarz
make build
```

`make build` produces `./foliarz` with `CGO_ENABLED=0`, making it a statically
linked binary that runs on both musl-based systems such as Alpine Linux and
glibc-based systems such as Debian. Verify with:

```sh
file foliarz
# ... statically linked ...
```

Install it somewhere on your `PATH`:

```sh
install -m 0755 foliarz ~/.local/bin/foliarz
```

Then run:

```sh
foliarz
```

A plain `go build` also works, but it may link against the host libc and may not
be portable between Alpine and Debian. Use the Makefile for release binaries.

## Configuration

Foliarz creates and uses these files:

| Path | Purpose |
|---|---|
| `~/.config/foliarz/foliarz.json` | Default provider/model, thinking level, UI preferences, and last-used settings. |
| `~/.config/foliarz/models.json` | Providers, models, API keys, headers, and model overrides. |
| `~/.config/foliarz/system-prompt.md` | System prompt sent with every request. |
| `~/.config/foliarz/skills/` | Pi-compatible skills (`SKILL.md` directories). Copy Pi skills here with `cp -a ~/.pi/agent/skills/. ~/.config/foliarz/skills/`; loaded skills are shown at startup. Skills reload only on restart, by design. |
| `~/.local/foliarz/sessions/*.json` | Automatically saved sessions. |
| `~/.local/foliarz/foliarz.log` | Application log. |
| `<working directory>/AGENTS.md` | Optional project instructions added to the context. |

Directories and default files are created on first run. Foliarz never replaces
existing settings with defaults; it updates only the last-used provider, model
and thinking fields in `foliarz.json` when they change.

### API key

The default `models.json` uses:

```json
{
  "providers": {
    "openrouter": {
      "apiKey": "$OPENROUTER_API_KEY",
      "modelOverrides": {
        "deepseek/deepseek-v4.1-flash": {
          "compat": {
            "openRouterRouting": {
              "only": ["alibaba"],
              "allow_fallbacks": false
            }
          }
        }
      }
    }
  }
}
```

Set the environment variable before starting Foliarz:

```sh
export OPENROUTER_API_KEY="sk-or-..."
foliarz
```

You can also put a literal key in `models.json`. The file is created with mode
`0600`; keep it private and never commit it.

### Choosing a model

Edit `foliarz.json` to change `defaultProvider` and `defaultModel`. Supported
models are merged from the built-in catalog and `models.json`; overrides and
model fields are documented in [docs/models.md](docs/models.md).

The current built-in provider is OpenRouter. The default model is
`deepseek/deepseek-v4.1-flash`; `deepseek/deepseek-v4.1-flash-1017` is also
included for testing model switching. Use `/model` to switch at runtime. Only
models using the OpenAI-compatible `openai-completions` API are currently
supported.

## Usage

Foliarz opens a full-screen chat. Type a message and press `enter`. Responses
stream as they are generated; tool calls appear as soon as they start running.

### Keys

| Key | Action |
|---|---|
| `enter` | Send the message, queue a steering message while the agent runs, or run the highlighted slash command. |
| `shift+enter` | Insert a newline (requires terminal modified-key support). |
| `alt+enter`, `ctrl+j` | Insert a newline on any terminal. |
| `shift+tab` | Cycle the model's supported thinking levels. |
| `ctrl+o` | Expand/collapse truncated tool output. |
| `ctrl+end` | Jump to the newest content and resume auto-scrolling. |
| `ctrl+c` | Stop the response, kill a running shell command, preserve context, and switch to FOLIA. |
| `ctrl+d` | Quit Foliarz. |
| mouse wheel | Scroll the chat. |
| `y` / `n` | Allow or deny a NORMAL-mode tool call. |

Scrolling up keeps your position while new content arrives below.

While the agent is working you can keep typing: `enter` queues the text as a
steering message. It is injected between tool steps (after the current assistant
turn and its tool calls) so the model can change direction without aborting the
run; a message queued after the final answer starts a follow-up turn. `ctrl+c`
discards the queue.

### Commands

| Command | Description |
|---|---|
| `/compact` | Summarize older context to free up the context window. |
| `/skill:<name>` | Run a loaded skill explicitly (not a registered slash command). |
| `/continue` | Resume the current turn with the existing context. |
| `/exit` | Close Foliarz. |
| `/folia` | Switch to FOLIA (read-only). |
| `/model` | Open the model picker and switch the current session's model. |
| `/new` | Start a new session without closing Foliarz. |
| `/normal` | Switch to NORMAL (approve non-read-only tools). |
| `/quit` | Close Foliarz. |
| `/session` | Open the saved-session picker. |
| `/yolo` | Switch to YOLO (all tools without asking). |

In NORMAL mode, a prompt shows the tool name and arguments before a write, edit,
or shell command runs. Press `y` to allow or `n` to deny.

### Sessions

Sessions are saved automatically, including partial streamed content, and
survive crashes. `/session` lists saved sessions newest-first with their date,
UUID, working directory, and first prompt. `enter` opens the selected session
and changes to its working directory; `esc` cancels. See
[docs/sessions.md](docs/sessions.md).

## Tools

| Tool | Read-only | Description |
|---|---|---|
| `datetime` | yes | Current OS date and time. |
| `read` | yes | Read a text file with optional offset/limit. |
| `ls` | yes | List a directory. |
| `write` | no | Create or overwrite a file, creating parent directories. |
| `edit` | no | Exact, unique text replacements in a file. |
| `bash` | no | Run a `bash` command with a required timeout. |

File tools reject paths outside the session working directory. See
[docs/tools.md](docs/tools.md) for details and for how to add a tool.

## Third-party libraries

Direct dependencies:

| Library | Used for |
|---|---|
| [charm.land/bubbletea/v2](https://pkg.go.dev/charm.land/bubbletea/v2) | TUI runtime and event loop. |
| [charm.land/bubbles/v2](https://pkg.go.dev/charm.land/bubbles/v2) | Multiline text input, key bindings, cursor. |
| [charm.land/lipgloss/v2](https://pkg.go.dev/charm.land/lipgloss/v2) | Styling, layout and colored backgrounds. |
| [charm.land/glamour/v2](https://pkg.go.dev/charm.land/glamour/v2) | Markdown rendering for assistant answers. |
| [github.com/google/uuid](https://pkg.go.dev/github.com/google/uuid) | Session UUIDs. |

Indirect dependencies (including Goldmark, Chroma, Bluemonday and terminal
helpers) are listed in `go.mod`/`go.sum`.

## Development

```sh
make build   # static binary
make test    # go test ./...
make vet     # go vet ./...
make fmt     # check gofmt
make check   # fmt + vet + test
```

The project has no CI requirement; run `make check` before committing.

Further documentation:

- [Configuration](docs/configuration.md)
- [Models](docs/models.md)
- [Modes](docs/modes.md)
- [Tools](docs/tools.md)
- [Terminal UI](docs/tui.md)
- [Commands](docs/commands.md)
- [Sessions](docs/sessions.md)
- [Skills](docs/skills.md)
- [Context compaction](docs/compaction.md)
- [Project instructions](docs/project-context.md)
- [Troubleshooting](docs/troubleshooting.md)

## License

MIT © 2026 Mateusz Dukat. See [LICENSE](LICENSE).
