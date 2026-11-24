# Models

Foliarz resolves its model catalog from two sources:

1. A built-in catalog shipped with the binary. It contains two models on the
   `openrouter` provider:
   - `deepseek/deepseek-v4.1-flash`
   - `deepseek/deepseek-v4.1-flash-1017`
2. `~/.config/foliarz/models.json`, which adds or replaces models and can apply
   model overrides.

The built-in catalog is analogous to Pi Agent's `models-store.json`; the user
file is analogous to Pi Agent's `models.json`.

## Default `models.json`

When `models.json` does not exist, Foliarz creates it with this content:

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

The override routes `deepseek/deepseek-v4.1-flash` to the `alibaba` upstream
provider only, with fallbacks disabled. It is the same OpenRouter routing
override as in the Pi Agent configuration. `deepseek/deepseek-v4.1-flash-1017`
has no override and uses OpenRouter's default routing.

You can switch models at runtime with `/model`.

## Structure

```json
{
  "providers": {
    "<provider ID>": {
      "name": "Display name",
      "baseUrl": "https://api.example.com/v1",
      "api": "openai-completions",
      "apiKey": "$EXAMPLE_API_KEY",
      "headers": { "X-Example": "$EXAMPLE_HEADER" },
      "compat": {},
      "models": [],
      "modelOverrides": {}
    }
  }
}
```

### Provider fields

| Field | Type | Description |
|---|---|---|
| `name` | string | Display name. |
| `baseUrl` | string | API base URL, also applied to the provider's built-in models. |
| `api` | string | API flavor of the provider's models, for example `openai-completions`. |
| `apiKey` | string | Literal key or `$NAME` environment interpolation. |
| `headers` | object | Request headers; values support `$NAME` interpolation. |
| `compat` | object | Provider-level compatibility settings. |
| `models` | array | Models to add. An entry whose `id` matches an existing model replaces it. |
| `modelOverrides` | object | Model overrides keyed by model ID. |

A provider key that already exists in the built-in catalog changes that
provider. Any other key adds a provider.

### Model fields

| Field | Type | Description |
|---|---|---|
| `id` | string | Required. Model ID sent to the API. |
| `name` | string | Display name. |
| `api` | string | Overrides the provider API. |
| `baseUrl` | string | Overrides the provider base URL. |
| `reasoning` | boolean | Whether the model supports thinking. |
| `thinkingLevelMap` | object | Provider value for each thinking level (`off`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`). `null` marks the level as unsupported. See [thinking levels](#thinking-levels). |
| `input` | array | Accepted input types, for example `["text", "image"]`. |
| `cost` | object | Pricing in USD per million tokens: `input`, `output`, `cacheRead`, `cacheWrite`, plus optional request-wide `tiers`. |
| `contextWindow` | number | Context window in tokens. |
| `maxTokens` | number | Maximum output tokens. |
| `compat` | object | Model-level compatibility settings. |
| `inputLimits` | object | Model input limits. |
| `samplingParams` | object | Fields merged into every request body. |
| `headers` | object | Request headers; values support `$NAME` interpolation. |

### Thinking levels

`thinkingLevelMap` decides which thinking levels a model supports. A level is
supported when the map gives it a non-`null` value; a `null` value marks it as
unsupported. The TUI's `shift+tab` cycles only supported levels.

The built-in `deepseek/deepseek-v4.1-flash` and
`deepseek/deepseek-v4.1-flash-1017` models support `off`, `low`, `high` and
`max`; `minimal`, `medium` and `xhigh` are `null`.

A model without a `thinkingLevelMap` supports every level when `reasoning` is
`true`, and only `off` when it is `false`.

### Model overrides

Each key under `modelOverrides` is the ID of a model from the built-in catalog
or from `models`. Overrides are applied after all models have been collected.

- Object fields are merged field by field, recursively. For example, an
  override that sets `cost.input` keeps the other `cost` values from the
  original model, and a `compat` override keeps unrelated `compat` fields.
- All other values replace the original value.
- Overrides for unknown model IDs are ignored.

In the default configuration the override adds
`compat.openRouterRouting` while keeping the catalog's other `compat` fields
such as `thinkingFormat` and `supportsStrictMode`.

## Runtime support

Foliarz currently implements the `openai-completions` API only. A model or
provider declaring another `api` value is rejected when selected.

Provider-level `compat` settings are merged into each model; model values win
per field. Applied to requests:

- provider/model `headers`, `apiKey`, `baseUrl`
- `maxTokens` (`max_tokens`)
- `samplingParams`, merged into the request body
- `thinkingLevelMap` and `compat.thinkingFormat`
- `compat.supportsStrictMode` for schemas whose properties are all required and
  `additionalProperties` is false
- `compat.sendSessionAffinityHeaders` with `compat.sessionAffinityFormat`
- `compat.requiresReasoningContentOnAssistantMessages` (sends an empty
  `reasoning_content` on replayed assistant messages that lack one)
- `compat.openRouterRouting` (sent as the OpenRouter `provider` field)

Assistant reasoning is replayed to the provider, like Pi Agent: structured
`reasoning_details` blocks returned by the API are stored in the session and sent
back verbatim, and plain thinking text is sent in the model's reasoning field
(`reasoning` for OpenRouter-style formats). Replaying reasoning keeps multi-step
work coherent so the model does not repeat investigative steps it already
performed.

### Compatibility formats

`compat.thinkingFormat` selects how the thinking level is sent:

| Value | Request shaping |
|---|---|
| `openrouter` (default) | `reasoning: {effort}` |
| `openai` | `reasoning_effort` |
| `deepseek` | `thinking: {type: enabled/disabled}` plus `reasoning_effort` |
| `zai` | `thinking: {type: enabled/disabled}` |
| `qwen` | `enable_thinking` |
| `string-thinking` | `thinking` string |

`compat.sessionAffinityFormat` selects the session-affinity headers sent when
`compat.sendSessionAffinityHeaders` is true: `openrouter` sends `x-session-id`,
`openai-nosession` sends `x-client-request-id` and `x-session-affinity`, and the
default also sends `session_id`.

### Headers and body overrides

`headers` are applied after `Authorization`, so a provider/model header can
replace the `Authorization` value. `samplingParams` are merged last into the
request body and can therefore override reserved fields such as `model`,
`messages`, `tools` or `stream`; use it only for provider-specific sampling
fields.

`cost` is used to price each request: input, output, cache-read and cache-write
rates per million tokens, with the highest matching request-wide tier applied to
the whole request. On OpenRouter the billed cost reported by the API replaces
the catalog estimate (BYOK requests add the upstream cost), like Pi Agent. The
sum is shown in the status bar and stored as `costUsd` in the session.

Parsed but not currently applied: `inputLimits`, `promptCache` is not modeled,
and non-text model inputs.

## Merging order

1. The built-in catalog is loaded.
2. Providers and models from `models.json` are merged over it; models with the
   same `id` replace catalog models.
3. Model overrides from the catalog and from `models.json` are applied; the user
   file wins on conflicts.
4. `$NAME` environment interpolation is applied to `apiKey` and `headers`.