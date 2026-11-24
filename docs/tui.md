# Terminal UI

Foliarz runs as a full-screen terminal UI.

## Layout

From top to bottom:

1. **Header** — `Foliarz` followed by the build version and commit the binary was
   built from.
2. **Chat area** — the session transcript.
3. **Input bar** — a multiline text editor framed by two horizontal rules, like
   the Pi Agent editor. It grows with its content up to 10 rows; longer input
   scrolls.
4. **Status bar** — the current mode (`FOLIA`, `NORMAL` or `YOLO`), tokens used,
   the session spend in US dollars (shown once it is non-zero, with Pi's three
   decimals, so very small spends appear as `$0.000`)
   and context window of the current session, the working directory, the model
   and the current thinking level.

## Key bindings

| Keys | Action |
|---|---|
| `shift+tab` | Switch to the next thinking level supported by the current model. |
| `shift+enter` | Insert a newline without submitting. Only works when the terminal reports modified keys (see below). |
| `alt+enter` | Insert a newline without submitting. Works on terminals that do not report modified Enter. |
| `ctrl+j` | Insert a newline without submitting. Works on terminals that do not report modified Enter. |
| `enter` | Submit the input, queue a steering message while a response runs, or run the highlighted slash command. |
| `y` / `n` | Allow or deny a tool call in NORMAL mode. |
| `up` / `down` | Move the selection in the session or model picker. |
| `enter` | Open the selected session or model. |
| `esc` | Close the session or model picker without switching. |
| `up` / `down` | Move the highlight in the slash command list. |
| `ctrl+o` | Expand or collapse truncated tool output. |
| `ctrl+c` | Stop the current response, kill a running shell command, and switch to FOLIA mode. Does not exit. |
| `ctrl+end` | Jump back to the newest streamed content and resume auto-scrolling. |
| `ctrl+d` | Quit Foliarz. |

Thinking-level cycling uses the model's `thinkingLevelMap`, so it visits only
levels the model supports. For `deepseek/deepseek-v4.1-flash` the cycle is
`off` → `low` → `high` → `max` → `off`, because `minimal`, `medium` and
`xhigh` are mapped to `null` in the model catalog. See
[models](models.md#thinking-levels).

The input bar supports the usual editing and navigation keys, including
`backspace`, `delete`, `left`, `right`, `up`, `down`, `home` and `end`. Lines
that are longer than the terminal width wrap onto the next row.

Without keyboard enhancement support, a terminal sends `enter` and `shift+enter`
as the same byte, so Foliarz cannot tell them apart and both submit the input.
`shift+enter` therefore works only on terminals that report modified Enter keys
through the Kitty keyboard protocol or `modifyOtherKeys`. In tmux, enable
extended keys:

```
set -g extended-keys on
```

`ctrl+j` and `alt+enter` are always available as fallbacks because they do not
depend on keyboard enhancement support.

When a session starts, the chat area shows the system prompt (including
`AGENTS.md` project instructions, if present) on a blue background, the tools
available in the current mode, and the loaded skills. The info disappears after
the first message and is shown again for a new session.

Skills are discovered only at startup; restart Foliarz to pick up added or
edited skills. This is intentional: changed skill instructions cannot enter an
already-running session.

The chat area scrolls with the mouse wheel (three lines per notch). While you
are scrolled up, new thinking, tool calls and answers keep arriving below
without moving your view; a hint shows `ctrl+end`, which jumps to the newest
content and resumes auto-scrolling. Scrolling reuses the rendered transcript, so
it stays responsive while the model streams.

Mouse reporting is enabled by the TUI; hold `shift` while selecting text to use
the terminal's own selection.

## Steering

Messages typed while the model is working are queued and shown in the chat with
a `(queued)` marker. The agent injects them between steps, right after the
current assistant turn and its tool calls, so the model can change direction
without aborting the run. A message queued after the final answer starts a
follow-up turn in the same response.

`ctrl+c` stops the run and discards queued messages; the context produced so far
is still preserved and replayed to the model on the next request. `/new` and
switching sessions also clear the queue.

Messages submitted with `enter` are appended to the in-memory session and sent
to the configured model. Responses are streamed: thinking and answer deltas are
rendered as they arrive in temporary `foliarz` blocks, so there is no need to
wait for the complete answer. Thinking from earlier tool steps stays visible
while later steps stream; after completion the transcript keeps the thinking of
every assistant message. Until the first delta arrives the chat shows
`waiting for the model…`. While a response is streaming, pressing `enter` queues the input as a steering
message instead of dropping it.

Tool calls appear as soon as they start executing: a gray background with
`running…` while the tool runs, then green for success or red for errors with a
preview of the tool output. In NORMAL mode, non-read-only calls pause at a
yellow prompt showing the tool and its arguments until you press `y` or `n`.
Shell calls also show their timeout and how long the command took. Consecutive
tool outputs are separated by a single blank line without a background.
`ctrl+o` expands or collapses the truncated tool output. Assistant messages are labelled `foliarz`;
their thinking is shown in gray above the answer, and the completed answer is
rendered as markdown. Set `hideThinkingBlock` to `true` in
`foliarz.json` to hide the thinking. Request errors are shown in the chat area
as well.

## Slash commands

When the input starts with `/`, Foliarz shows the matching commands above the
input bar. The list filters as you type, `up` and `down` move the highlight, and
`enter` runs the highlighted command, including `/folia`, `/normal`, `/yolo`,
`/model`, `/compact` and `/session`. See [commands](commands.md),
[modes](modes.md) and [sessions](sessions.md#switching-sessions).