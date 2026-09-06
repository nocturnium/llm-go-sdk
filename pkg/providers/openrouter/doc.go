// Package openrouter provides OpenRouter chat, embeddings and media generation.
// Chat and images use openaicompat.BaseProvider. Audio uses shared converters
// and transport to preserve optional voices and audio upload filenames. Native
// video is required because OpenRouter's duration, frame_images, job states and
// unsigned_urls do not match OpenAI's VideoObject. Speech usage lookup preserves
// headers via the shared binary transport; verbose transcription avoids the
// compatible client's OpenAI-only model/format allowlist.
//
// # Authentication
//
// Set OPENROUTER_API_KEY or use WithAPIKey. LLM_API_KEY is the final fallback.
// WithSiteURL and WithAppName add HTTP-Referer and X-Title attribution headers.
// HTTPS and public IPs are required unless explicitly opted out. The default
// request timeout is 120 seconds; discovery is additionally capped at 30 seconds.
// Streams and video Wait should always receive a bounded context.
//
// # Default models
//
// Chat defaults to google/gemini-3.5-flash-lite, verified with curl against
// https://openrouter.ai/api/v1/models on 2026-09-05. It is a current inexpensive
// Google chat model. Embeddings require WithEmbeddingModel or WithEmbedModel.
// Images default to google/gemini-3.1-flash-lite-image; transcription defaults to
// openai/whisper-1. Override media models through their per-request options.
//
// Speech defaults to fish-audio/s2.1-pro, live-verified on 2026-09-05 via GET
// https://openrouter.ai/api/v1/models?output_modalities=speech.
//
// ListSpeechModels returns this filtered catalog with reported input_char pricing.
//
// Video defaults to google/veo-3.1-lite, verified with curl against
// https://openrouter.ai/api/v1/videos/models on 2026-09-05. Its published 720p
// rate without audio is $0.03/second, the lowest directly comparable per-second
// rate. Token-priced models cannot be ranked without a token conversion formula.
// Use WithVideoAudio(false), WithVideoResolution("720p") and a supported duration
// (minimum 4 seconds) for that rate. It does not support 480p. The opt-in 480p
// live test uses x-ai/grok-imagine-video at its minimum duration of 1 second,
// verified in the same catalog ($0.05/second).
//
// # Supported features
//
// Chat streaming, tools, vision and JSON mode are routing capabilities; support
// and token limits vary by model. Images support synchronous generation; edits
// and speech SSE are disabled. GenerateImage forwards AspectRatio and Seed as
// aspect_ratio and seed; WithImageExtra supplies resolution, input_references,
// and other native parameters, merged last. NegativePrompt and SafetyTolerance
// have no verified wire mapping and are ignored. Video accepts first/last frames
// as URLs or inline images; pass native input_references and callback_url via
// WithVideoExtra. ReferenceImages is rejected because its wire schema is unverified.
// Video extras include size, provider, upscale_factor, creativity and model-specific
// passthrough keys; typed fields are reserved and cannot be overridden by extras.
// callback_url must use HTTPS. NegativePrompt and output_format are ignored:
// neither appears in the video request schema at https://openrouter.ai/openapi.json
// checked on 2026-09-05. Frames use type:image_url, nested image_url.url and frame_type.
// Moderation is input-stage until in_progress is observed, then output-stage.
// An unspecified video duration leaves the usage unit empty.
// WithSpeechExtra forwards input_references and provider; fixed speech fields win.
//
// Speech defaults to mp3 and omits an unset voice; providers choose defaults or
// require an explicit WithSpeechVoice. WithUsageLookup enables one optional
// GET /generation?id=... after speech to populate MediaUsage.Cost; it is disabled
// by default. Lookup failures leave Cost nil and return successful audio.
// SpeechResponse.Metadata["generation_id"] retains X-Generation-Id when present.
// GenerationCost(ctx, id) retrieves the cost later and exposes lookup errors.
// Missing generation IDs or costs leave usage unpriced. Image, transcription and
// video usage.cost is retained directly; no MediaPricing estimates are installed.
// Audio discovery endpoints do not exist; use the public /models filters instead.
//
// # Example
//
//	client, err := openrouter.New(openrouter.WithAppName("My app"))
//	if err != nil {
//		log.Fatal(err)
//	}
//	answer, err := llms.Call(ctx, client, "Hello!")
package openrouter
