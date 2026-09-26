# Changelog

## [Unreleased]

### Added

- `NewModel` adapts any `llms.LLM` to ADK Go's `model.LLM`: every genai request
  field translated, ignored or rejected by name; streaming in ADK's
  partial-then-final shape; tool-call IDs unique across the session; reasoning
  round-trips through the session; structured output with tools through a
  `set_model_response` tool; pending results for calls long-running tools leave
  open.
- `Tools` exposes an `llms.ToolRegistry` to ADK agents.
- Live release gate and recorded fixtures for OpenAI (chat and Responses),
  Anthropic, Gemini and OpenRouter.
