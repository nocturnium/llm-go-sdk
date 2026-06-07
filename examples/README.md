# Examples

Runnable examples demonstrating the features of the llms package.

## Prerequisites

Set at least one API key environment variable:

```bash
export OPENAI_API_KEY="sk-..."
export ANTHROPIC_API_KEY="sk-ant-..."
export GEMINI_API_KEY="..."
```

## Running Examples

From the `llms` directory:

```bash
go run ./examples/basic
go run ./examples/streaming
go run ./examples/tools
go run ./examples/vision
go run ./examples/embeddings
go run ./examples/resilience
go run ./examples/fallback
go run ./examples/cost-tracking
```

## Example Descriptions

| Example | Description |
|---------|-------------|
| **basic** | Simple chat completion with messages and options |
| **streaming** | Real-time streaming responses via channels |
| **tools** | Function/tool calling with execution loop |
| **vision** | Image analysis with multi-modal messages |
| **embeddings** | Text embeddings for semantic search |
| **resilience** | Circuit breakers and retry patterns |
| **fallback** | Multi-provider failover chains |
| **cost-tracking** | Token usage and cost estimation |

## Provider Compatibility

| Example | OpenAI | Anthropic | Gemini | TogetherAI | Featherless |
|---------|--------|-----------|--------|------------|-------------|
| basic | ✅ | ✅ | ✅ | ✅ | ✅ |
| streaming | ✅ | ✅ | ✅ | ✅ | ✅ |
| tools | ✅ | ✅ | ✅ | ✅ | ✅ |
| vision | ✅ | ✅ | ✅ | ❌ | ❌ |
| embeddings | ✅ | ❌ | ✅ | ❌ | ❌ |
| resilience | ✅ | ✅ | ✅ | ✅ | ✅ |
| fallback | ✅ | ✅ | ✅ | ✅ | ✅ |
| cost-tracking | ✅ | ✅ | ✅ | ✅ | ✅ |
