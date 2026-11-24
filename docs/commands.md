# Commands

Foliarz has slash commands. Typing `/` at the start of the input opens a list of
matching commands above the input bar. The list filters as you type, `up` and
`down` move the highlight, and `enter` runs the highlighted command.

Only a single command token is treated as a command. Text that contains
arguments or newlines, such as `/quit now`, is submitted as a normal message.

Skills use a separate syntax: `/skill:<name> [request]`. See
[skills](skills.md#explicit-invocation).

## Available commands

| Command | Description |
|---|---|
| `/compact` | Summarize older context to free up the context window. |
| `/continue` | Resume the current turn using the existing context, without adding a user message. |
| `/exit` | Close Foliarz. |
| `/folia` | Switch to FOLIA mode (read-only tools). |
| `/model` | Open the model picker and switch the current session's model. |
| `/new` | Start a new session without closing Foliarz. |
| `/normal` | Switch to NORMAL mode (asks before non-read-only tools). |
| `/quit` | Close Foliarz. |
| `/session` | Open the saved-session picker. |
| `/yolo` | Switch to YOLO mode (all tools without asking). |

`/session` opens a window with saved sessions; see
[sessions](sessions.md#switching-sessions).

`/quit` and `/exit` do the same thing; `ctrl+d` also closes Foliarz. `/new`
replaces the current session with a fresh one: it gets a new UUID, an empty
transcript, the last-used provider, model and thinking level (falling back to
the configured defaults on first run), and starts in FOLIA mode. The TUI stays open. See [modes](modes.md).

`/continue` re-sends the current session context to the model as if the turn
were still running. It adds no new user message, so it is useful when the
connection to the API was lost or a response was stopped with `ctrl+c`.
Partial streamed answers preserved by `ctrl+c` become part of the context.

## Adding a command

Commands are modular: one command is one Go source file in
`internal/commands`. A file registers its command in an `init` function, so no
other file needs to change.

For example, `internal/commands/hello.go`:

```go
package commands

func init() {
	Register(Command{
		Name:        "hello",
		Description: "Say hello",
		Run:         func() Action { return ActionNone },
	})
}
```

Command names must be non-empty, must not start with `/`, and must not contain
whitespace. Registering a duplicate name panics at startup.

`Run` returns an `Action`:

| Action | Effect |
|---|---|
| `ActionNone` | Nothing else; the command handled everything itself. |
| `ActionQuit` | Close the TUI. |
| `ActionNewSession` | Start a new session without closing the TUI. |
| `ActionSetFolia` | Switch to FOLIA mode. |
| `ActionSetNormal` | Switch to NORMAL mode. |
| `ActionSetYolo` | Switch to YOLO mode. |
| `ActionListSessions` | Open the saved-session picker. |
| `ActionCompact` | Compact the conversation context. |
| `ActionSelectModel` | Open the model picker. |
| `ActionContinue` | Resume the current turn with the existing context. |

`/skill:<name>` is handled separately from registered commands.

The command list is sorted by command name.