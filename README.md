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

## Reference-video style analysis

`analyze-style` reads the named reference sets from the ingest index and writes `.auto-editor/style.json`. It measures auditable style facts: reference-video count, total and median duration, aspect-ratio distribution, audio prevalence, scene-cut count, cuts per minute, and median shot duration. Each reference video retains its detected cut timestamps as evidence. Future iterations will add caption and editorial-style analysis on top of this evidence rather than copying individual reference videos.

The current style stage is local and deterministic: it does not send reference videos or frames to an AI provider. A future optional AI layer will analyze a bounded set of sampled frames and aggregate patterns across a named reference set, such as caption conventions, recurring visual roles, humor, emotional pacing, and likely editing techniques. It should consume the deterministic evidence already in `style.json`, return a structured style interpretation with supporting frame references, and remain opt-in through an explicit provider, model, and API-key environment setting. The planner should treat this interpretation as a preference, while preserving user inputs and deterministic constraints as the source of truth.

## Planning

`plan` combines `alignment.json` and `assets.json` into `.auto-editor/plan.json`. The current planner is deterministic: it creates one timed visual and caption for each aligned narration segment, cycles through available visual assets, preserves asset IDs, and emits a warning when `style.json` is unavailable. A future AI planner can improve candidate selection through the same validated timeline schema without inventing paths or bypassing deterministic validation.
