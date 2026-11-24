# Sessions

A session is the state of one Foliarz conversation. Sessions are stored as JSON
files in `~/.local/foliarz/sessions/`, one file per session:

```
~/.local/foliarz/sessions/<uuid>.json
```

The session UUID is stored in the file's `id` field and is also the file name.
It lets a session that was closed earlier be opened again.

Session files are written atomically: the JSON is written to a temporary file,
synced to disk and then renamed to its final name. Files are created with mode
`0600`. A process crash or a power loss therefore leaves either the previous or
the newest complete file.

## File format

```json
{
  "id": "6f1a3c1e-3f0e-4c0b-9d4a-2b6a5f8c7d10",
  "createdAt": "2026-01-02T15:04:05.123456789Z",
  "updatedAt": "2026-01-02T15:10:42.987654321Z",
  "workDir": "/home/user/project",
  "mode": "FOLIA",
  "provider": "openrouter",
  "model": "deepseek/deepseek-v4.1-flash",
  "thinkingLevel": "off",
  "tokensUsed": 1234,
  "contextWindow": 1048576,
  "costUsd": 0.0042,
  "messages": [
    { "role": "user", "content": "hello" },
    {
      "role": "assistant",
      "content": "",
      "thinking": "I should check the time.",
      "reasoningDetails": [
        { "type": "reasoning.text", "text": "I should check the time.", "format": "unknown", "index": 0 }
      ],
      "toolCalls": [
        { "id": "call-1", "name": "datetime", "arguments": "{}" }
      ]
    },
    {
      "role": "tool",
      "content": "2026-11-24T15:38:48+01:00 (Tuesday)",
      "toolCallId": "call-1",
      "isError": false
    },
    { "role": "assistant", "content": "It is Tuesday afternoon." }
  ]
}
```

| Field | Type | Description |
|---|---|---|
| `id` | string | Session UUID; also the file name without `.json`. |
| `createdAt` | string | Creation time in RFC 3339 (UTC). |
| `updatedAt` | string | Time of the last save in RFC 3339 (UTC). |
| `workDir` | string | Working directory of the session. |
| `mode` | string | Tool mode, `FOLIA`, `NORMAL` or `YOLO`. Always reset to `FOLIA` when a session is loaded. |
| `provider` | string | Provider ID the session uses. |
| `model` | string | Model ID the session uses. |
| `thinkingLevel` | string | Thinking level of the session. |
| `tokensUsed` | number | Tokens consumed in the session. |
| `contextWindow` | number | Context window of the model in tokens; omitted when unknown. |
| `costUsd` | number | Total session spend in US dollars, accumulated from catalog pricing or provider-reported cost; omitted when zero. |
| `summary` | string | Structured summary of older context after compaction. |
| `firstKeptMessage` | number | Index of the first message kept after the summary. |
| `summaryReadFiles` | array | Cumulative files read in summarized context. |
| `summaryModifiedFiles` | array | Cumulative files modified in summarized context. |
| `summaryTokensBefore` | number | Estimated context size before the last compaction. |
| `messages` | array | Session transcript. Each message has a `role` and `content`; assistant messages may carry `thinking`, `reasoningDetails` and `toolCalls`, and tool results carry `toolCallId`, `isError` and, for executed tools, `durationMs`. |

## Behavior

- Saving an existing session keeps its `id` and `createdAt`, updates
  `updatedAt`, and replaces the file.
- Loading requires a valid UUID; any other ID is rejected, which also prevents
  paths outside the sessions directory.
- A session file whose `id` does not match its file name is rejected.
- Loading always sets the mode to `FOLIA`, even if the session was saved in
  another mode.
- Listing returns all sessions, most recently updated first, and returns an
  empty list when the sessions directory does not exist yet.

`AGENTS.md` project instructions are not stored in session files; they are
reloaded whenever a session starts.

## Automatic saving

Sessions are saved automatically; an explicit save command is not needed.
Foliarz writes the session file:

- when a user message is submitted,
- when a response completes or fails,
- when `ctrl+c` stops a response,
- when the mode or thinking level changes,
- while a response streams, at most every 500 ms, including the partial
  thinking and answer produced so far,
- immediately when a tool result arrives (this bypasses the 500 ms throttle).

Writes are atomic (temporary file plus rename), so a crash leaves either the
previous or the newest complete session file. Sessions with no messages are not
saved and are hidden from the picker.

## Switching sessions

`/session` opens a session picker in the chat area. Sessions are listed newest
first, with:

- the date and time of the last interaction,
- the session UUID,
- the working directory,
- the beginning of the first user prompt.

Keys:

| Key | Action |
|---|---|
| `up` / `down` | Move the selection. |
| `enter` | Open the selected session. |
| `esc` | Cancel and close the picker. |

Opening a session replaces the current session with the saved transcript, changes
the process working directory to the session's working directory, and reloads
that directory's `AGENTS.md`. The session starts in FOLIA mode, regardless of
the mode it had when it was saved. `/new` after switching creates the new session
in the opened session's working directory.

The picker cannot be opened while a response is running.
