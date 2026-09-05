// Package gemini provides a Google Gemini LLM client using native HTTP.
//
// This package implements the llms.LLM interface for Google's Gemini API,
// supporting Gemini 2.5 and 3.x chat families and native media models.
// Native transport is required for contents/parts, Veo operations and Interactions.
//
// # Configuration
//
// The client reads the API key from environment variables by default:
//   - GEMINI_API_KEY (primary)
//   - GOOGLE_API_KEY (alternative)
//   - LLM_API_KEY (fallback)
//
// Or provide it explicitly with WithAPIKey.
//
// # Quick Start
//
//	client, err := gemini.New()
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	resp, err := client.Call(ctx, "Hello!")
//
// # Supported Features
//
//   - Chat completions (chat models)
//   - Streaming responses
//   - Function/tool calling
//   - Vision (supported chat models)
//   - Embeddings (text-embedding-004)
//   - JSON mode
//   - Native image generation and inline image editing
//   - Veo video jobs with authenticated MP4 downloads
//   - PCM-only speech synthesis (no speech streaming)
//   - Interactions transcription with optional speaker labels and word timestamps
//
// # Configuration Options
//
//	client, err := gemini.New(
//	    gemini.WithAPIKey("..."),
//	    gemini.WithModel("gemini-2.5-flash"),
//	    gemini.WithEmbeddingModel("text-embedding-004"),
//	    gemini.WithHTTPClient(customHTTPClient),
//	)
//
// # Embeddings
//
//	client, err := gemini.New()
//	embedder, ok := llms.AsEmbedder(client)
//	if ok {
//	    vectors, err := llms.EmbedDocuments(ctx, embedder, texts,
//	        llms.WithTaskType(llms.TaskTypeRetrievalDocument),
//	    )
//	}
//
// # Default Model
//
// The default chat model is gemini-2.5-flash. Override with WithModel.
// Media defaults are gemini-3.1-flash-image, veo-3.1-lite-generate-preview,
// gemini-3.1-flash-tts-preview (voice Kore), and gemini-3.5-transcribe.
// Override with WithImageModel, WithVideoModel, WithSpeechModel, WithSpeechVoice,
// and WithTranscriptionModel. Image and video models require paid quota.
// Speech returns raw mono PCM s16le at 24 kHz; its default prompt prefix is
// "Say: ", or "<Instructions>: " when speech instructions are supplied.
// WithPollPolicy controls video and transcription polling.
//
// # Thread Safety
//
// The Client is safe for concurrent use from multiple goroutines.
package gemini
