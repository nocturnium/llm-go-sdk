// Package elevenlabs provides native ElevenLabs speech, transcription, image
// generation/editing and video generation, independently of chat.
//
// # Native transport justification
//
// ElevenLabs is not OpenAI-shaped: it authenticates with xi-api-key, uses
// voice-addressed raw audio routes, a native timestamp envelope, Scribe multipart
// fields, and asynchronous Flows states. These protocols cannot use BaseProvider.
// Like Infinity, this client does not implement llms.LLM and is deliberately
// absent from the LLM-typed registry and pkg/providers/all. Construct it directly.
// Use llms.SupportsSpeech/SupportsTranscription/SupportsImageGeneration and
// SupportsVideoGeneration, or call Capabilities directly (not GetCapabilities).
//
// # Authentication and defaults
//
// Set ELEVENLABS_API_KEY or WithAPIKey; LLM_API_KEY is the final fallback.
// Default host is https://api.elevenlabs.io. WithBaseURL accepts regional hosts
// api.us.elevenlabs.io, api.eu.residency.elevenlabs.io,
// api.in.residency.elevenlabs.io and api.sg.residency.elevenlabs.io (include HTTPS).
// Requests default to 120s; streams are also bounded by this timeout and ctx.
// HTTPS and public IPs are required unless explicitly opted out.
//
// Speech defaults to eleven_flash_v2_5 and premade Rachel
// (21m00Tcm4TlvDq8ikWAM). WithVoice or llms.WithSpeechVoice overrides the voice.
// Audio defaults to mp3_44100_128; BitRate is bits/second, e.g. 64000.
// Empty container/sample rate use mp3/44100; empty compressed bitrate uses 128000.
// PCM, WAV, u-law and A-law do not carry a bitrate selector. Encoding is unsupported.
// TTS usage always uses Unicode rune count / 1000 (KChar). The character-cost
// header is a plan-scaled credit counter, not a character count: valid nonnegative
// integers are preserved as Metadata["character_cost"] (int64), without changing usage.
// All 28 output formats are supported, including mp3_24000_48, pcm_32000 and
// WAV at 8000/16000/22050/24000/32000/44100/48000 Hz.
// StreamSpeech supports TTS raw chunks only; timestamps use Synthesize with
// llms.WithSpeechTimestamps(true). SynthesizeDialogue accepts ordered voice lines,
// retains voice_settings and language_code, and defaults independently to eleven_v3,
// the only model supporting that route. Other explicit models are rejected.
// Speech Extra passes additional native fields through. Typed model_id, text/prompt,
// voice_settings and language_code remain authoritative, even when omitted;
// SFX/music extra duration and boolean fields are validated before sending.
// Dialog inputs remain authoritative. Caller-owned Extra maps are not modified.
//
// # Sound effects and music
//
// Synthesize routes model IDs starting with eleven_text_to_sound to
// /v1/sound-generation; Extra accepts duration_seconds (0.5-30), loop and
// prompt_influence (0-1). IDs starting with music_v route to /v1/music;
// Extra accepts music_length_ms (3000-600000, default 10000) and force_instrumental.
// Verified models are eleven_text_to_sound_v2 and music_v2/music_v1.
// Their usage is requested generated seconds / 60 (minute). Auto-length sound
// effects have unknown duration and an empty usage unit; no duration is guessed.
//
// # Transcription
//
// Scribe defaults to scribe_v2 and word timestamps. Input accepts Data plus a
// supported MIMEType or an HTTPS source_url (replacing deprecated cloud_storage_url),
// never FileID. Keyterms are repeated fields.
// Extra accepts num_speakers (1-32), timestamps_granularity and tag_audio_events.
// WordTimestamps overrides the granularity to word. Duration/minute usage is
// taken from audio_duration_secs, even without timestamps. Only when it is zero
// does duration fall back to the maximum end in the provider words array, including
// spacing and audio events. Unit is empty when neither duration source is available.
// Metadata preserves transcription_id and language_probability.
//
// # Flows
//
// Image/video are beta, best-effort Pro-plan features: HTTP 402 wraps
// llms.ErrPlanRequired. Images default to gemini-3.1-flash-lite-image; videos to
// veo-3.1-fast-generate-001. GenerateImage synchronously polls then downloads;
// GenerateVideo returns *llms.PollingVideoJob. Use bounded contexts or
// WithPollPolicy. Cancel is unsupported. Signed assets are fetched through the
// SSRF-validated transport without credentials, retaining URL and a 55m expiry.
// Moderation is input-stage until generating is observed, then output-stage;
// failed Flows are not charged. Image credit pricing is unknown; video usage
// records specified seconds but Cost remains nil. Seedance defaults server-side
// to five seconds when omitted (usage remains unknown).
//
// Images map Size (1K/2K/4K/512), AspectRatio, Quality (gpt-image only), Seed
// (Seedream only). Extra merges last, including native asset/generation reference
// objects, webhook and background (gpt-image-1/1.5). EditImage accepts inline Data
// references only; URLs and FileID are rejected. N may be zero or one.
// Video maps duration, audio (default true), first/last frames, aspect, resolution,
// negative prompt and seed. Extra merges last and accepts enhance_prompt and
// native asset/generation frames. ReferenceImages is unsupported. Unsupported
// image NegativePrompt, OutputFormat, SafetyTolerance, video OutputFormat, speech
// Instructions and transcription Prompt have no verified mapping and are ignored.
//
// # Example
//
//	client, err := elevenlabs.New()
//	if err != nil { log.Fatal(err) }
//	response, err := client.Synthesize(ctx, "Hi.")
//	_ = response
//	_ = err
package elevenlabs
