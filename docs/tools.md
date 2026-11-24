# Tools

Tools are functions the model can call. They are modular: one tool is one Go
source file in `internal/tools`, and a file registers its tool in an `init`
function, so no other file needs to change.

The model receives the tools available in the current [mode](modes.md) with
every request. When it asks for a tool call, Foliarz runs the tool and sends the
result back to the model. There is no fixed limit on tool steps: a turn runs
until the model returns a final answer or the user stops it with `ctrl+c`.

Tool calls are displayed in the chat as soon as they start executing: gray with
`running…` while the tool runs, then green for success or red for errors,
together with a preview of the result. `ctrl+o` expands or collapses truncated
output.

## Available tools

| Tool | Read-only | Description |
|---|---|---|
| `datetime` | yes | Returns the current date and time from the operating system as an RFC 3339 timestamp with the weekday, for example `2026-11-24T15:38:48+01:00 (Tuesday)`. |
| `read` | yes | Reads a text file, optionally starting at a 1-indexed `offset` with a maximum number of lines. |
| `ls` | yes | Lists a directory, sorted alphabetically, with `/` after directories. |
| `write` | no | Writes content to a file, creating parent directories and overwriting existing files. |
| `edit` | no | Replaces exact text in a file with one or more `{oldText, newText}` edits. |
| `bash` | no | Executes a bash command in the working directory, returning stdout and stderr. |

FOLIA mode exposes only read-only tools. NORMAL exposes all tools but asks the
user to approve non-read-only calls; YOLO runs all tools without asking. See
[modes](modes.md).

`bash` runs in the session working directory but is not restricted to it: a
command can access any path the user can. The working-directory sandbox applies
to `read`, `ls`, `write` and `edit` only.

Tools resolve relative paths against the current session working directory.
A leading `~` is expanded and a leading `@` is stripped. `read`, `ls`, `write` and
`edit` reject any path outside the working directory, including paths that
escape through `..` or symlinks. For example, with Foliarz started in
`~/test`, `~/test/abc/def/test.txt` is allowed but `/tmp/test` is not.

## `read`

- `path` is required; `offset` (1-indexed) and `limit` (lines) are optional.
- Output is limited to 2000 lines or 50KB. A notice explains how to continue
  with `offset`.
- Lines are counted the way Pi counts them: a trailing newline does not add an
  empty line, and an empty file returns an empty result.
- Files are read as text; images are not supported.

## `ls`

- `path` is optional and defaults to the working directory; `limit` defaults to
  500 entries.
- Entries are sorted alphabetically (case-insensitive), include dotfiles, and
  directories have a `/` suffix.
- Output is limited to 500 entries or 50KB. A notice explains how to request
  more with `limit`.
- An empty directory returns `(empty directory)`.

## `write`

- `path` and `content` are required.
- Parent directories are created, and an existing file is overwritten.
- Returns `Successfully wrote to <path>`.

## `edit`

- `path` and a non-empty `edits` array are required.
- Each `oldText` must match exactly after line endings are normalized to LF,
  and it must occur exactly once in the original file.
- Edits are matched against the original content, not incrementally, and must
  not overlap.
- CRLF files keep CRLF line endings; a UTF-8 BOM is preserved.
- Returns `Successfully replaced N block(s) in <path>`.

Unlike Pi Agent, matching is exact and does not apply fuzzy Unicode/whitespace
normalization.

## `bash`

- `command` and `timeout` are required. A command is not executed without a
  timeout.
- The command runs with `bash -c` in the session working directory.
- stdout and stderr are combined.
- Output is limited to the last 2000 lines or 50KB. When truncated, the full
  output is saved to a temporary file (`foliarz-bash-*` in `$TMPDIR`) and its
  path is included in the result. Files older than 7 days are pruned when
  Foliarz starts.
- A non-zero exit code, timeout, or abort is reported as a tool error with the
  status appended to the output.
- The command is not executed when `timeout` is missing or not positive.
- `ctrl+c` cancels a running command and kills its process group.

## Tool interface

Each tool is a `tools.Tool`:

| Field | Description |
|---|---|
| `Name` | Function name sent to the model. |
| `Description` | What the tool does. |
| `Parameters` | JSON schema of the arguments. |
| `ReadOnly` | When true, the tool is available in FOLIA mode. |
| `Run` | `func(ctx context.Context, args json.RawMessage) (string, error)`; returns the result as text. |

Arguments are passed to `Run` as raw JSON. A tool error marks the result as an
error; the result text and the error message are both sent to the model, and the
call is shown with a red background.

If the model calls a tool that exists but is not available in the current mode
(for example `bash` in FOLIA), the result says that the tool is blocked in FOLIA
mode and how to switch to NORMAL or YOLO; it is not reported as an unknown tool.
In NORMAL, a non-read-only call waits for user approval; denial is returned to
the model as a tool error.

## Adding a tool

For example, `internal/tools/echo.go`:

```go
package tools

import (
	"context"
	"encoding/json"
)

func init() {
	Register(Tool{
		Name:        "echo",
		Description: "Echo the provided text",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`),
		ReadOnly:    true,
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			var input struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(args, &input); err != nil {
				return "", err
			}
			return input.Text, nil
		},
	})
}
```

Tool names must be non-empty and must not contain whitespace or `/`. Registering
a duplicate name, a missing description, an invalid parameter schema or a
missing `Run` function panics at startup. Mark tools that only read data as
`ReadOnly: true` so they are available in FOLIA mode.

Shared tool infrastructure lives in `tools.go` (registry and context),
`path.go` (working-directory path resolution and sandboxing) and `helpers.go`
(output limits and truncation). A tool file does not need to change them, but
tools that read or write paths should use `resolvePathInWorkDir` to stay inside
the working directory.

The default system prompt lists the built-in tools and explains the modes. Edit
`~/.config/foliarz/system-prompt.md` to change it.

Shell tool calls display the required timeout in the call header and the
execution duration (`Took 1.2s`) below the output.
