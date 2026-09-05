# auto-editor

`auto-editor` is a Go-first CLI for an agentic video-editing system. It currently initializes projects, validates and indexes inputs, aligns a voiceover transcript to its script, and analyzes visual assets.

## Future: semantic-analysis providers

Asset semantic analysis is designed behind a provider-neutral interface. The current implementation supports an OpenAI vision provider when explicitly enabled in the project configuration.

Future work should add adapters for additional vision-capable providers, such as Claude, Gemini, and local models. Every adapter should return the same structured asset analysis—summary, subjects, actions, settings, mood, and visible text—so planning and rendering stay independent of the chosen model provider.

Each provider should be configured with an explicit provider name, model name, and API-key environment variable. Deterministic asset inspection must remain available without any external provider or API key.

## Asset-analysis output

Visual asset analysis separates two concepts:

- `literal`: evidence visible in the asset, such as subjects, actions, setting, mood, and on-screen text.
- `editorial_meaning`: an interpretation of the asset's likely reaction, joke, emotional beat, and useful editing roles.

Video semantic analysis uses start, middle, and end sample frames in one provider request. This gives the model sequence context while avoiding the added services and cost of audio transcription or full-video OCR.

## Semantic AI communication

When `assets.semantic_provider` is `openai`, the semantic provider uses a small, resilient communication layer:

- It sends the start/middle/end frames together as one asset-level request, so the model can infer sequence and meme context without three separate calls.
- It requests concise, schema-constrained JSON for `literal` and `editorial_meaning`; downstream stages receive predictable fields instead of fragile prose parsing.
- It caches successful results in `.auto-editor/semantic-cache.json`. The cache key includes the ordered frame bytes, selected model, image-detail setting, and semantic prompt revision, so unchanged assets do not consume another API request while changed frames or prompts are reanalyzed.
- It uses `store: false`, a bounded request timeout, and up to three attempts for provider-declared transient failures (429, 408, and 5xx), respecting `Retry-After` when supplied. It does not retry validation or authentication failures.

The user-editable settings below trade cost against fidelity. `auto` is the default because semantic context and visible captions are important; use `low` for inexpensive broad triage, or `high` for difficult visual detail.

```json
{
  "assets": {
    "semantic_provider": "openai",
    "semantic_model": "gpt-4.1-mini",
    "api_key_environment": "OPENAI_API_KEY",
    "semantic_cache": true,
    "semantic_image_detail": "auto",
    "semantic_max_attempts": 3,
    "semantic_timeout_seconds": 60
  }
}
```

## Reference-video style analysis

`analyze-style` reads the named reference sets from the ingest index and writes `.auto-editor/style.json`. It always measures auditable style facts: reference-video count, total and median duration, aspect-ratio distribution, audio prevalence, scene-cut count, cuts per minute, and median shot duration. Each reference video retains its detected cut timestamps as evidence.

An optional semantic second pass can analyze a bounded, round-robin sample of reference frames for each named set. It returns a separate `semantic_style` object with caption conventions, humor patterns, emotional pacing, visual roles, editing techniques, planning preferences, supporting frame IDs, and confidence. The pass reuses the same OpenAI transport, bounded retry policy, `store: false` behavior, image-detail setting, and content-hash cache strategy as asset semantic analysis. Its cache is `.auto-editor/style-semantic-cache.json`.

Enable it explicitly in the project configuration. The default cap of 12 frames balances set-level coverage against cost; the cap is shared round-robin across videos so a large set does not let one reference dominate the interpretation.

```json
{
  "style": {
    "semantic_provider": "openai",
    "semantic_model": "gpt-4.1-mini",
    "api_key_environment": "OPENAI_API_KEY",
    "semantic_cache": true,
    "semantic_image_detail": "auto",
    "semantic_max_frames": 12,
    "semantic_max_attempts": 3,
    "semantic_timeout_seconds": 60
  }
}
```

If the key, provider, or semantic pass is unavailable, deterministic style analysis still succeeds and records that the semantic portion was skipped or failed.

## Planning

`plan` combines `alignment.json` and `assets.json` into `.auto-editor/plan.json`. It creates one timed visual and caption for each aligned narration segment, preserves source asset IDs and timing constraints, and cycles through every eligible visual asset.

When `style.json` contains completed semantic `planning_preferences`, the planner applies them only as soft ranking inputs. It deterministically compares meaningful preference terms with completed literal/editorial asset metadata, weights matches by the reference set's semantic confidence, and uses original asset order to break ties. Preferences never remove an asset, invent an ID, alter narration timing, or override source-duration limits. The version 2 plan artifact records the policy, exact preferences, per-asset scores, matched terms, and original order under `asset_selection`; `style_status` explains whether preferences were applied, unavailable, absent, or unmatched.
