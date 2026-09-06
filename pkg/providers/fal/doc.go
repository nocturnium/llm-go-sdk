// Package fal provides native fal.ai image, video, speech and transcription
// generation through the fal queue API, independently of chat.
//
// # Native transport justification
//
// fal is not OpenAI-shaped: every application is addressed by its own path
// (fal-ai/flux/schnell), authenticated with "Authorization: Key", submitted to
// https://queue.fal.run, then tracked through status, result and cancel routes
// with model-specific JSON bodies. These protocols cannot use BaseProvider.
// Like ElevenLabs, this client does not implement llms.LLM and is deliberately
// absent from the LLM-typed registry and pkg/providers/all. Construct it directly.
// Use llms.SupportsImageGeneration/SupportsVideoGeneration/SupportsSpeech/
// SupportsTranscription, or call Capabilities directly (not GetCapabilities).
//
// # Authentication and defaults
//
// Set FAL_KEY or WithAPIKey; LLM_API_KEY is the final fallback. The default
// host is https://queue.fal.run; WithQueuePriority("low") sets the
// X-Fal-Queue-Priority submit header. Requests default to 120s each; queue
// polling is bounded by ctx and WithPollPolicy (1s initial, 10s maximum by
// default). HTTPS and public IPs are required unless explicitly opted out.
//
// Default applications: images fal-ai/flux/schnell, video
// fal-ai/minimax/hailuo-02/standard/text-to-video, speech
// fal-ai/kokoro/american-english (voice af_heart) and transcription
// fal-ai/whisper. Override each with the With*Model options or the per-call
// llms.With*Model options. Tracking URLs returned by the queue are used when
// they share the queue origin; otherwise {base}/{namespace}/requests/{id}
// routes are derived, where the namespace is the first two segments of the
// model ID (fal-ai/flux for fal-ai/flux/schnell). Request routes under the full
// model path are rejected by fal.
//
// Image, speech and transcription calls poll synchronously: canceling ctx
// abandons the queued request without sending a cancel, and fal may still bill
// it. A submit whose request_id fails validation is likewise abandoned. Only
// video jobs expose Cancel.
//
// # Asset handling
//
// Every result is a hosted file. Images, video and audio are downloaded
// eagerly through the SSRF-validated transport without credentials, so the API
// key never reaches fal.media hosts. MediaAsset retains URL, bytes and MIME
// type; ExpiresAt stays zero because fal does not publish a TTL. Result
// responses carrying X-Fal-Billable-Units populate Metadata["billable_units"].
//
// # Pricing
//
// Pricing is NOT tabulated: fal publishes per-model prices only on client-side
// rendered pages that could not be verified. Usage reports Unit and Quantity
// (megapixels, seconds, kchar, minutes) and leaves Cost nil; image quantity is
// zero when fal omits a dimension so the unit never changes.
//
// # Images
//
// GenerateImage maps Size "WxH" to an image_size object (winning over
// AspectRatio), AspectRatio 1:1/4:3/3:4/16:9/9:16 to fal presets, Count to
// num_images, Seed and Format (jpeg/png). NegativePrompt, Quality and
// SafetyTolerance have no verified mapping and are rejected; Extra merges last
// with prompt, image_size, num_images, seed and output_format reserved. When
// every image carries has_nsfw_concepts the call returns an output-stage
// ModerationError; partial flags drop those images and record
// Metadata["nsfw_indices"].
//
// # Video
//
// GenerateVideo returns *llms.PollingVideoJob. Hailuo accepts DurationSeconds 6
// or 10 (omitted bills 6); Resolution, AspectRatio and the remaining typed
// options are not configurable and are rejected. Extra merges last with prompt
// and duration reserved. Cancel issues PUT .../cancel; already completed jobs
// return ErrInvalidParameters.
//
// # Speech and transcription
//
// Synthesize sends the text as Kokoro's prompt field with Voice (validated for
// the default application only), Speed and Extra. Only an empty or wav Format
// container is accepted; Timestamps, Language and Instructions are rejected.
// StreamSpeech returns ErrSpeechStreamNotSupported. Transcribe accepts an HTTPS
// URL or inline Data up to 25 MB (sent as a base64 data URI); FileID is
// rejected. Language, Prompt, Diarize (plus Extra num_speakers) and
// WordTimestamps (chunk_level word) map directly; Translate sets task
// translate. Segments come from chunks; Words are populated only for word
// chunk_level. Minute usage derives from the last chunk end. Chunks with
// malformed timestamps are skipped, counted in Metadata["skipped_chunks"], and
// usage is suppressed only when every chunk was skipped.
//
// # Error mapping
//
// 401/403 wrap ErrAuthenticationFailed, 429 ErrRateLimited, 400/422
// ErrInvalidParameters (content_policy_violation becomes an input-stage
// ModerationError and no_media_generated ErrIncompleteResponse), 404
// ErrModelNotFound, timeouts ErrTimeout, other 5xx ErrServiceUnavailable and
// any other 4xx ErrInvalidParameters. Only the body error_type is consulted;
// the X-Fal-Error-Type header is not read because APIError does not expose
// response headers. *httpclient.APIError remains in the chain for errors.As.
//
// # Example
//
//	client, err := fal.New()
//	if err != nil { log.Fatal(err) }
//	response, err := client.GenerateImage(ctx, "a lighthouse at dawn")
//	_ = response
//	_ = err
package fal
