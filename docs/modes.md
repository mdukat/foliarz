# Modes

Foliarz has three tool modes:

| Mode | Tools | Approval |
|---|---|---|
| `FOLIA` | Read-only tools only | None needed; mutating tools are blocked. |
| `NORMAL` | All tools | The user must allow each non-read-only tool call before it runs. |
| `YOLO` | All tools | Non-read-only tools run without asking. |

Foliarz **always starts in FOLIA mode**, even when reopening a session that was
saved in another mode. Sessions created with `/new` also start in FOLIA.

## Switching modes

| Command | Effect |
|---|---|
| `/folia` | Switch to FOLIA (read-only). |
| `/normal` | Switch to NORMAL (asks before non-read-only tools). |
| `/yolo` | Switch to YOLO (all tools without asking). |

The current mode is shown in the bottom status bar and is sent to the model in
the system prompt on every request.

## Approval in NORMAL

When the model calls a non-read-only tool in NORMAL mode, Foliarz shows the tool
name and arguments and asks:

```
allow tool call? NORMAL mode
bash {"command": "rm -rf build", "timeout": 30}
press y to allow, n to deny
```

- `y` runs the tool.
- `n` denies it. The model receives a tool error saying the user denied the
  call and can continue with another approach.
- `ctrl+c` denies the call, stops the response, preserves the context, and
  switches to FOLIA.
- Other keys are ignored while the approval prompt is visible.

Read-only tools (`datetime`, `read`, `ls`) run without approval. In FOLIA the model is
explicitly told that Foliarz is read-only and that non-read-only tools must not
be called. If it still calls one, the tool result says the tool is blocked in
FOLIA mode and that the user can switch to NORMAL or YOLO — it is not reported
as an unknown tool.

## Interrupting

`ctrl+c` does not close the TUI. It:

1. stops the current model response (including streaming),
2. kills a running shell command and its child processes,
3. preserves the context built so far: completed tool steps and the partial
   thinking/answer remain in the session and are replayed to the provider on the
   next request, like completed turns,
4. switches Foliarz back to FOLIA mode.

After stopping, `/continue` resumes the turn with the preserved context. The
only ways to exit are the `/quit` and `/exit` commands, or `ctrl+d`.

## Tools per mode

| Tool | FOLIA | NORMAL | YOLO |
|---|---|---|---|
| `datetime` | yes | yes | yes |
| `read` | yes | yes | yes |
| `ls` | yes | yes | yes |
| `write` | blocked | approval | yes |
| `edit` | blocked | approval | yes |
| `bash` | blocked | approval | yes |

Tools are marked as read-only in their source file. See [tools](tools.md).
