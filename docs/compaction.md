# Context compaction

Foliarz compacts long conversations by summarizing older messages into a
structured context summary, then keeping only the most recent messages. This is
the same approach as Pi Agent: the model receives the summary followed by the
retained recent messages, while the full transcript remains in the session file
and the TUI.

## Settings

Compaction is configured in `foliarz.json`:

```json
{
  "compaction": {
    "enabled": true,
    "reserveTokens": 16384,
    "keepRecentTokens": 20000
  }
}
```

If the object is missing, these Pi-compatible defaults apply:
`enabled: true`, `reserveTokens: 16384`, `keepRecentTokens: 20000`.

- **reserveTokens** — headroom kept free below the model context window.
- **keepRecentTokens** — approximate number of recent tokens to keep verbatim.

## When it runs

Compaction triggers when:

```
context tokens > context window - reserveTokens
```

Foliarz checks before a new prompt, after a response completes, and between
tool steps when a tool result pushes the projected context over the threshold.
It can also be run manually with `/compact`. Because compaction makes its own
model request, the TUI briefly waits for the summary to be generated.

The context window and current token usage come from the session; the defaults
match the selected model. You can use a smaller `contextWindow` override in
`models.json` to test compaction with a smaller conversation.

## What the model sees

Older messages are cut at a user or assistant message boundary (never in the
middle of a tool result), summarized, and replaced in the request context by:

```
The conversation history before this point was compacted into the following summary:

<summary>
...structured summary...
</summary>
```

The recent messages selected by `keepRecentTokens` follow the summary. The full
transcript remains available in the TUI and in the session JSON.

## Summary format

Foliarz uses the Pi Agent structured checkpoint format:

- Goal
- Constraints & Preferences
- Progress (Done / In Progress / Blocked)
- Key Decisions
- Next Steps
- Critical Context

It also tracks files read and modified, appending `<read-files>` and
`<modified-files>` lists to the summary. On repeated compactions the previous
summary is passed back to the summarizer so existing information is preserved.

## Stored data

The session file stores:

| Field | Meaning |
|---|---|
| `summary` | Structured summary of older messages. |
| `firstKeptMessage` | Index of the first message kept after the summary. |
| `summaryReadFiles` | Cumulative files read in summarized context. |
| `summaryModifiedFiles` | Cumulative files modified in summarized context. |
| `summaryTokensBefore` | Estimated context size before the last compaction. |

The summary is included in every subsequent request until the context is
compacted again.
