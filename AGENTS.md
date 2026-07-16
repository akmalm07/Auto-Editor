# AGENTS.md

Guidance for AI agents working in this repository.

## Project

This is `auto-editor`, a Go-first CLI for an agentic video editing system.

The long-term program takes:

- a voiceover audio file
- the written script for that voiceover
- video/image/audio/text assets
- zero or more named reference-video sets

and eventually outputs a fully edited video that uses the supplied assets and voiceover while matching the style of the references when provided.

The system should be built in layers:

```text
init -> ingest -> align -> analyze-assets -> analyze-style -> plan -> render -> review
```

Do not jump straight to LLM planning or rendering before the deterministic layers are solid.

## Current State

The project currently has:

- `cmd/auto-editor/main.go`: CLI entrypoint.
- `internal/cli`: command routing and terminal presentation.
- `internal/project`: user-editable project manifest model.
- `internal/config`: user-editable CLI/application config model.
- `internal/ingest`: deterministic input validation and index generation.
- `internal/ui/loading`: reusable terminal loading animation wrapper.

Main user-facing files:

- `auto-editor.json`: user-editable project manifest, created by `init`.
- `auto-editor.config.json`: user-editable config, created by `init`.
- `.auto-editor/index.json`: generated ingest index, created by `ingest`.

Generated files under `.auto-editor/` should not be committed.

## Development Commands

Use the workspace Go cache to avoid local cache permission issues:

```powershell
$env:GOCACHE='C:\Users\dev\tools\auto-editor\.gocache'
go test ./...
```

Common smoke commands:

```powershell
go run ./cmd/auto-editor init --name launch-video --script script.txt --voiceover voiceover.wav --asset assets --reference shorts=refs/shorts
go run ./cmd/auto-editor ingest --manifest auto-editor.json --config auto-editor.config.json
```

Always run:

```powershell
gofmt -w <changed-go-files>
go test ./...
```

before calling a coding task done.

## Coding Style

- Keep Go code simple, typed, and boring in the good way.
- Keep CLI code thin. Business logic belongs in testable `internal/...` packages.
- Prefer small packages with clear ownership over large mixed-purpose files.
- Return typed errors when the caller needs to branch on behavior.
- Accumulate user-facing validation diagnostics when possible instead of failing on the first issue.
- Use `filepath` and structured APIs rather than ad hoc path/string handling.
- Keep generated data separate from user-authored data.
- Do not add global mutable state unless the external library forces it.
- Do not introduce large frameworks unless the benefit is obvious.

## CLI Experience

The CLI should feel transparent and alive:

- show what stage is running
- use reusable loading animations for longer work
- print warnings in yellow and errors in red when color is enabled
- gracefully fall back to plain text in non-interactive output
- make failures actionable
- avoid noisy stack traces for normal user mistakes

Terminal presentation should stay reusable. Prefer extending `internal/ui/loading` and `internal/cli/presenter.go` instead of sprinkling ANSI codes or spinner calls across commands.

## Config Philosophy

`auto-editor.config.json` exists so behavior can evolve without turning every choice into a CLI flag.

Current shape:

```json
{
  "ui": {
    "color": "auto",
    "progress": true,
    "loading": {
      "enabled": true,
      "style": "ascii",
      "interval_ms": 80
    }
  }
}
```

Rules:

- Keep config human-editable.
- Validate config values clearly.
- Add defaults in `internal/config`.
- Do not silently ignore invalid config.
- Prefer stable field names because users may edit this file directly.

## Manifest Philosophy

`auto-editor.json` is the user-authored project source of truth.

It should describe intent and inputs, not generated facts. Keep it readable.

Current shape:

```json
{
  "name": "launch-video",
  "aspect_ratio": "16:9",
  "language": "en",
  "runtime": "60s",
  "created_at": "2026-07-16T12:00:00Z",
  "inputs": {
    "script": "script.txt",
    "voiceover": "voiceover.wav",
    "assets": ["assets"],
    "reference_sets": [
      {
        "name": "shorts",
        "paths": ["refs/shorts"]
      }
    ]
  }
}
```

Rules:

- Relative paths resolve from the manifest directory.
- Assets may be files or directories.
- Reference sets are named because different reference groups may imply different style weights later.
- References are optional.
- Script, voiceover, and at least one asset are currently required by ingest.

## Ingest Philosophy

Ingest is deterministic. It should catch structural problems and produce stable facts, not infer meaning.

It should validate:

- required manifest fields
- path existence
- file vs directory expectations
- empty files
- supported extensions
- UTF-8 script text
- duplicate file content

It should produce:

- normalized paths
- media kind
- extension
- file size
- SHA-256 hash
- stable-ish IDs
- warnings for skipped files inside directories

It should not try to decide whether the voiceover matches the script. That belongs to a future `align` stage using ASR and confidence scores.

## Planned Next Layers

Recommended order:

1. `align`: run ASR on the voiceover, compare it to the script, and emit timed script/audio segments with confidence scores.
2. `analyze-assets`: extract deterministic and model-assisted metadata from assets.
3. `analyze-style`: inspect reference videos and produce a structured style profile.
4. `plan`: produce a constrained timeline JSON using IDs from the ingest/analysis outputs.
5. `render`: render from the constrained timeline.
6. `review`: score the render and suggest patchable timeline changes.

Keep each generated artifact versioned and schema-shaped.

## Testing Expectations

Add tests when changing:

- manifest/config parsing or defaults
- validation behavior
- path expansion
- file classification
- ID/hash behavior
- CLI output behavior that has branching logic
- reusable UI helpers

Tests should use `t.TempDir()` and avoid depending on real user files.

## Dependency Guidance

External modules are allowed when they make the code simpler and are easy to isolate.

Current relevant modules:

- `github.com/fatih/color`: terminal colors.
- `github.com/briandowns/spinner`: terminal loading animations.

Wrap external UI modules behind internal packages/helpers so future changes do not leak through the whole codebase.

## Git Hygiene

- Do not revert user changes.
- Check `git status --short` before and after substantial work.
- Keep generated files out of commits.
- Do not commit unless the user asks.
- Avoid destructive Git commands unless explicitly requested.

## User Preference

The user likes to understand the architecture before code appears.

When the task is architectural or ambiguous:

- explain the next step first
- name the tradeoffs
- keep the plan practical
- then implement only when asked

When the user asks to implement:

- make the change end to end
- keep the code consistent with the current app
- test it
- summarize what changed without drowning them in detail

