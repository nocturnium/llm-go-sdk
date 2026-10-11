# llmadk

Run any [llm-go-sdk](https://github.com/nocturnium/llm-go-sdk) provider as the
model behind a [Google ADK Go](https://github.com/google/adk-go) agent.

```bash
go get github.com/nocturnium/llm-go-sdk/llmadk
```

```go
model, err := llmadk.NewModel(claude) // any llms.LLM
agent, err := llmagent.New(llmagent.Config{Name: "assistant", Model: model, Tools: tools})
```

See the [guide](../docs/guides/adk.md) for options, how requests are translated,
and the provider compatibility matrix, and `examples/agent` for a complete
program.

## Testing

- `make test`: unit, end-to-end (real ADK agents over in-memory and sqlite
  sessions) and fixture replay tests, offline.
- `make integration`: the live release gate against provider APIs; set
  `LLMADK_RECORD=1` to refresh the fixtures under `testdata/fixtures`. It reads
  `OPENAI_API_KEY`, `ANTHROPIC_API_KEY` (or `CLAUDE_API_KEY`), `GEMINI_API_KEY`,
  `OPENROUTER_API_KEY` and `ZAI_API_KEY` (or `ZAI_TOKEN`), skipping a provider
  without one.
