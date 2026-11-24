# Configuration

Foliarz keeps its files in two directories in the user's home:

| Directory | Purpose |
|---|---|
| `~/.config/foliarz` | Configuration files |
| `~/.local/foliarz` | Logs and session files |

Foliarz creates these directories when they are missing, together with the
`sessions` subdirectory of the data directory. Directories are created with
mode `0700`.

Configuration files are created with mode `0600` and defaults only when they do
not exist yet. Foliarz never replaces an existing configuration with defaults.
It rewrites `foliarz.json` when persisting the last-used provider, model or
thinking level; all other fields in the file are preserved.

## Configuration files

| File | Purpose |
|---|---|
| `~/.config/foliarz/foliarz.json` | TUI-scope configuration |
| `~/.config/foliarz/models.json` | Saved models and their API configuration |
| `~/.config/foliarz/system-prompt.md` | System prompt sent with every model request |
| `~/.config/foliarz/skills/` | Pi-compatible skills (`SKILL.md` directories), loaded at startup; restart to reload. |
| `<working directory>/AGENTS.md` | Project instructions added to the context, see [project instructions](project-context.md) |
| `~/.local/foliarz/sessions/*.json` | Session files, see [sessions](sessions.md) |

## `foliarz.json`

`foliarz.json` holds TUI-scope configuration.

```json
{
  "defaultProvider": "openrouter",
  "defaultModel": "deepseek/deepseek-v4.1-flash",
  "thinkingLevel": "off",
  "hideThinkingBlock": false,
  "lastProvider": "openrouter",
  "lastModel": "deepseek/deepseek-v4.1-flash",
  "lastThinkingLevel": "high",
  "compaction": {
    "enabled": true,
    "reserveTokens": 16384,
    "keepRecentTokens": 20000
  }
}
```

| Field | Type | Default | Description |
|---|---|---|---|
| `defaultProvider` | string | `"openrouter"` | Provider ID used when a session starts. |
| `defaultModel` | string | `"deepseek/deepseek-v4.1-flash"` | Model ID used when a session starts. |
| `thinkingLevel` | string | `"off"` | Default thinking level: `off`, `minimal`, `low`, `medium`, `high`, `xhigh` or `max`. An empty value means `off`. Any other value is an error. |
| `hideThinkingBlock` | boolean | `false` | Whether the model's thinking is hidden from the transcript. |
| `lastProvider` | string | unset | Provider used by the last session. Overrides `defaultProvider` on startup. |
| `lastModel` | string | unset | Model used by the last session. Overrides `defaultModel` on startup. |
| `lastThinkingLevel` | string | unset | Thinking level used by the last session. Overrides `thinkingLevel` on startup. |
| `compaction.enabled` | boolean | `true` | Enable automatic context compaction. |
| `compaction.reserveTokens` | number | `16384` | Context headroom kept free before compaction triggers. |
| `compaction.keepRecentTokens` | number | `20000` | Approximate recent tokens kept verbatim after compaction. |

Foliarz remembers the last used provider, model and thinking level: changing the
thinking level with `shift+tab` writes the `last*` fields to `foliarz.json`.
Other fields of the file are preserved. On startup the last values win over the
defaults; if they are missing, the defaults are used. The last thinking level is
also used by `/new`.

The default provider and model must resolve in the loaded models configuration.
If they do not, Foliarz reports an error when it starts.

## `models.json`

`models.json` stores saved models and their API configuration. It is merged on
top of a built-in model catalog. See [models](models.md) for the full reference
and the default content.

## `system-prompt.md`

`system-prompt.md` is the system prompt sent to the model with every request. It
is created with a default when it does not exist; existing files are never
overwritten.

The default is derived from the Pi Agent default system prompt. `Pi` is named
`Foliarz`, and the prompt is adapted to Foliarz's tools and modes: it describes
the `read`, `ls`, `write`, `edit`, `bash` and `datetime` tools and the FOLIA,
NORMAL and YOLO modes. FOLIA exposes only the read-only tools. See [tools](tools.md) and [modes](modes.md).

Edit the file to change the agent's instructions. It is read at startup. The
current mode and any project instructions (`AGENTS.md`) are appended to the
system prompt for each request.

## Compaction

See [context compaction](compaction.md) for threshold behavior and the summary
format.

## Environment variables

`apiKey` and `headers` values support `$NAME` interpolation: a value such as
`"$OPENROUTER_API_KEY"` is replaced with the value of the `OPENROUTER_API_KEY`
environment variable when the configuration is loaded. The resolved value is
kept in memory only; the file is not rewritten.