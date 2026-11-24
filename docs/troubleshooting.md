# Troubleshooting

## Foliarz does not start

- **`cannot determine working directory`** — the directory Foliarz was started
  in no longer exists. Run it from an existing directory.
- **`default provider "…" is not configured` / `default model "…"`** — the
  provider or model in `~/.config/foliarz/foliarz.json` is not present in the
  catalog or `models.json`. Fix the ID or add the model.
- **`unsupported API "…"`** — only OpenAI-compatible `openai-completions`
  endpoints are supported.
- **`read …/AGENTS.md: …`** — `AGENTS.md` in the working directory exists but is
  unreadable. Fix its permissions or contents.
- A malformed `foliarz.json` or `models.json` reports a parse error naming the
  file. Repair the JSON; Foliarz never replaces an existing configuration with
  defaults. Deleting the file recreates it with defaults on the next start.
- An invalid `lastThinkingLevel` is ignored, so it cannot block startup.

## HTTP 401 or "no API key"

- The default `models.json` uses `"apiKey": "$OPENROUTER_API_KEY"`. Export the
  variable before starting Foliarz, or put a literal key in the file.
- Foliarz logs a warning when an interpolated key resolves to an empty value;
  check `~/.local/foliarz/foliarz.log`.
- Requests are only sent to the provider and base URL of the selected model.

## Model answers look non-reasoning

- The model's `thinkingLevelMap` may not support the selected level. Switching
  models clamps the level to the new model's supported levels and persists the
  clamped value.
- Check the level in the status bar and cycle with `shift+tab`.

## Context compaction

- Compaction triggers when context tokens exceed
  `contextWindow - reserveTokens`. With a small `contextWindow` override it can
  trigger on every turn; raise `reserveTokens` or use a larger window.
- If compaction fails (for example the provider rejects the summarization
  request), the error is shown and the turn continues with the uncompacted
  context. Retry later with `/compact`.

## A tool call is denied or blocked

- In NORMAL mode every non-read-only call waits for `y`. Pressing `n` denies it
  and the model is told the user denied the call.
- In FOLIA, `write`, `edit` and `bash` are blocked. Use `/normal` or `/yolo`, or
  `ctrl+c`/`/folia` to return to read-only mode.
- File tools reject paths outside the working directory, including symlink
  escapes. `bash` is not sandboxed.

## `ctrl+c` does not exit

By design. `ctrl+c` stops the response, kills a running shell command, discards
queued steering messages, preserves the context built so far, and switches to
FOLIA. Use `/quit`, `/exit` or `ctrl+d` to exit.

## A skill is missing or will not run

- Skills load from `~/.config/foliarz/skills/` at startup only; restart Foliarz
  after adding or editing one.
- Invalid `SKILL.md` files are skipped with a warning in the log.
- Shell-requiring skills fail in FOLIA. Switch to NORMAL or YOLO.

## Sessions

- `/session` lists sessions newest first and skips unreadable files with a log
  warning.
- Opening a session changes the working directory to the session's directory and
  starts in FOLIA.
- A damaged session file can be deleted from `~/.local/foliarz/sessions/`.

## Logs and disk usage

- `~/.local/foliarz/foliarz.log` is appended to and never rotated. Truncate or
  delete it to reclaim space. It contains errors and lifecycle events, not
  conversations.
- Truncated `bash` output is kept in `$TMPDIR/foliarz-bash-*` and pruned after 7
  days at startup.
