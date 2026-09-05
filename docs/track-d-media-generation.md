# Track D — Media generation (image, video, speech, transcription)

Status: **PLANNED** (research complete 2026-09-05, no code yet).
Scope decision: additive, `feat:` minors on `/v6`. No breaking change is required.

## 1. Where the SDK stands

- No provider can call an image, video, TTS, or STT endpoint. `ModelTypeImage` / `ModelTypeAudio`
  exist only as listing metadata (`models.go:22,34`; `pkg/providers/openai/models.go:279-303`
  lists `dall-e-*`, `whisper-1`, `tts-*` with pricing deliberately omitted).
- Input side handles images only (`vision.go`); `pkg/openaicompat/converters.go:75` drops audio parts.
- No OpenRouter provider exists (one pricing comment at `cost.go:366`). No `ELEVENLABS_*` or
  `OPENROUTER_*` env constants in `apikey.go:46-72`.
- `Pricing` is token-only (`cost.go:15-43`); `Pricing.cost` takes `Usage`. `ModelUsage` must stay
  `comparable` (`cost.go:560-566`).
- No submit→poll→fetch job abstraction anywhere (`batch.go` is client-side fan-out).
- `internal/httpclient` has `DoJSON`, `DoRaw`, `DoStream`; no multipart upload, no binary download
  with content-type, no poll helper.
- Registry (`registry.go:61`) is `LLM`-typed. Media-only providers follow the `infinity` precedent:
  direct construction, absent from `pkg/providers/all`.
- Template to copy: `embeddings.go` (interface + `As*`/`Supports*` + own option type + sentinels),
  which also hosts `Reranker` as the second-capability precedent.

## 2. Provider landscape (first-party docs, 2026-09-05)

Legend: OAI = OpenAI-shaped route usable by one `openaicompat` client.

| Provider | Image | Video | TTS | STT | Transport | Verdict |
|---|---|---|---|---|---|---|
| OpenAI | `gpt-image-2` sync b64 + SSE partials | `sora-2[-pro]` async `/v1/videos` → MP4 bytes | `gpt-4o-mini-tts`, bytes | `gpt-transcribe`, multipart, SSE | OAI (canonical) | **P0** |
| Azure OpenAI | same via `/openai/v1/`, deployment=model, no webp | `sora-2` 720p only | same | same | OAI | free with P0 (base URL) |
| OpenRouter | own `POST /api/v1/images` (OAI-like body, `data[].b64_json`+`media_type`) | own `POST /api/v1/videos` 202 + `polling_url` + `/content` | `/api/v1/audio/speech` mp3/pcm | `/api/v1/audio/transcriptions` json or multipart | OAI for audio; native for image/video | **P1** |
| ElevenLabs | `POST /v1/flows/image` async, Pro plan+ (402 otherwise), wraps GPT-Image/Nano Banana/Seedream | `flows.video` async (Veo 3.1, Seedance) | `/v1/text-to-speech/{voice}` bytes, `/stream`, `/with-timestamps` | `/v1/speech-to-text` `scribe_v2` multipart | native, `xi-api-key` | **P2** |
| Gemini API | `gemini-3.1-flash-image` via `generateContent` `responseModalities`; compat `/v1beta/openai/images/generations` | Veo 3.1 `:predictLongRunning` + operation poll + Files download; compat `/v1beta/openai/videos` | `gemini-3.1-flash-tts-preview` → PCM s16le 24 kHz | `gemini-3.5-transcribe` (Interactions API only) | native + partial OAI | **P3** |
| Together | `/v1/images/generations` (+url/b64, FLUX.2, gpt-image-2) | `/v2/videos` async, per-video pricing | `/v1/audio/speech` (+SSE, WS) | `/v1/audio/transcriptions` (+diarize) | OAI + native video | **P4** |
| Groq | none | none | `/openai/v1/audio/speech` wav only, 200 chars, `canopylabs/orpheus-v1-english` | `/openai/v1/audio/transcriptions` whisper-large-v3[-turbo] | OAI | P4 |
| Featherless | none | none | `/v1/audio/speech` Kokoro/Orpheus/Chatterbox | none | OAI | P4 |
| Mistral | agent tool only | none | `voxtral-mini-tts-2603`, base64 JSON body (not OAI) | `/v1/audio/transcriptions` voxtral | OAI for STT | P4 (STT only) |
| Z.AI | `/images/generations` url-only, no `n` | `/videos/generations` + `/async-result/{id}` | none | `/audio/transcriptions` ≤30 s | ≈OAI | P4 |
| fal.ai | queue.fal.run, URL out | yes | yes | yes | native, `Key` auth | P5 #1 |
| Replicate | `/predictions`, `Prefer: wait`, 1 h URLs | yes | yes | yes | native | P5 #2 |
| Black Forest Labs | `/v1/flux-2-*` async, **10 min URL TTL** | `flux-3-video` | none | none | native, `x-key` | P5 #3 |
| Deepgram | none | none | `/v1/speak` bytes | `/v1/listen` | native, `Token` auth, official Go SDK | P5 #4 |
| xAI | `/v1/images/generations` (OAI) | `/v1/videos/generations` async | `/v1/tts` | yes | OAI images | P5 |
| Runway / Kling / Luma / MiniMax / Cartesia / AssemblyAI / Recraft / Ideogram / Stability | video-first or speech-first natives | | | | native | backlog |

Do not build: Fireworks media (deprecated 2026-06-10), Ollama image gen (removed v0.32.6),
llama.cpp, Anthropic, DeepSeek, Cerebras, Perplexity (no generation), HF router (chat-only compat;
provider-native proxying is a P5+ item), Imagen 4 (shut down 2026-08-17), Groq PlayAI (shut down),
`dall-e-3`, Bedrock Nova Canvas/Reel (EOL 2026-09-30), PlayHT (defunct), Suno/Udio (no API).

Cross-provider facts that shape the design:

- **Async is the norm for video and common for images.** 14 of 16 dedicated providers use
  create → handle → poll. Terminal vocabularies all map onto
  `{Queued, Running, Succeeded, Failed, Cancelled, Moderated}`.
- **Output arrives three ways**: base64/inline (OpenAI, Gemini, OpenRouter images, Mistral TTS, all
  TTS bytes), hosted URL with an expiry (Together, Z.AI, Runpod, ElevenLabs flows, BFL 10 min,
  Replicate 1 h, Gemini Files 2 d), cloud URI (Vertex, Bedrock).
- **Moderation is surfaced five ways** (HTTP 4xx, job terminal state, HTTP 200 + flag, partial
  results, and billed-or-not differs). Must normalise to one error.
- **Pricing units**: per image, per megapixel, per second, per 1M characters, per minute, per
  1M image-tokens (gpt-image-*), plus provider-reported cost (OpenRouter `usage.cost`, Runway,
  Ideogram, xAI). Provider-reported cost must win over the static table when present.
- **OAI-shape coverage is real but partial**: one compat client covers OpenAI, Azure, Together,
  Groq, Featherless, Mistral-STT, Z.AI (with url-only tolerance), Cartesia-STT, xAI/Recraft images,
  and OpenRouter audio. Everything else needs a native adapter.

## 3. Design

### 3.1 Root package: four capability interfaces (new files, mirror `embeddings.go`)

```go
// imagegen.go
type ImageGenerator interface {
    GenerateImage(ctx context.Context, prompt string, opts ...ImageOption) (*ImageResponse, error)
}
type ImageEditor interface { // optional second interface, same file
    EditImage(ctx context.Context, prompt string, images []MediaInput, opts ...ImageOption) (*ImageResponse, error)
}

// videogen.go — always a job; Wait is the ergonomic path
type VideoGenerator interface {
    GenerateVideo(ctx context.Context, prompt string, opts ...VideoOption) (VideoJob, error)
}
type VideoJob interface {
    ID() string
    Poll(ctx context.Context) (*JobStatus, error)
    Wait(ctx context.Context) (*VideoResponse, error) // provider-owned backoff
    Cancel(ctx context.Context) error                 // ErrNotSupported where absent
}

// speech.go
type SpeechSynthesizer interface {
    Synthesize(ctx context.Context, text string, opts ...SpeechOption) (*SpeechResponse, error)
    StreamSpeech(ctx context.Context, text string, opts ...SpeechOption) (<-chan AudioChunk, error)
}
type Transcriber interface {
    Transcribe(ctx context.Context, audio MediaInput, opts ...TranscribeOption) (*Transcription, error)
}
```

Each file also carries `As<X>(any) (<X>, bool)`, `Supports<X>(any) bool`, a private options struct
with `Apply<X>Options`, and sentinels `Err<X>NotSupported`. No changes to `LLM` or `CallOptions`.
`Capabilities` gains four bools (`ImageGeneration`, `VideoGeneration`, `Speech`, `Transcription`);
`capabilities_registry.go:647 ModelTypes()` maps them; `models.go` gains `ModelTypeVideo`.

### 3.2 Shared media types (`media.go`)

```go
type MediaInput struct { URL string; Data []byte; MIMEType string; FileID string } // one of
type MediaAsset struct {
    URL string; Data []byte; CloudURI string; MIMEType string
    ExpiresAt time.Time; Seed *int64; RevisedPrompt string
}
func (a *MediaAsset) Fetch(ctx context.Context, c *http.Client) ([]byte, error) // materialise before expiry
type JobState string // Queued Running Succeeded Failed Cancelled Moderated
type JobStatus struct { State JobState; Progress *float64; Error error; Cost *float64 }
type MediaUsage struct { Unit MediaUnit; Quantity float64; Cost *float64 } // Cost = provider-reported
type AudioChunk struct { Data []byte; Alignment *Alignment; Err error }
```

Option core set, decided from the ≥50% survey: image `Size WxH` **or** `AspectRatio`, `N`, `Seed`,
`NegativePrompt`, `Quality`, `OutputFormat`, `SafetyTolerance`; video adds `DurationSeconds`,
`Resolution`, `Audio bool`, `FirstFrame/LastFrame/ReferenceImages`; speech `Voice`, `Model`,
`Speed`, `Language`, `Format{Container,Encoding,SampleRate,BitRate}`, `Timestamps`; transcribe
`Model`, `Language`, `Diarize`, `WordTimestamps`, `Keyterms`, `Prompt`. Every option type keeps
`WithExtra(map[string]any)` because every provider has 10+ bespoke fields.

### 3.3 Errors

Reuse `ErrContentFiltered` (`errors.go`) as the sentinel; add `ModerationError{Stage Input|Output;
Reasons []string; Charged bool}` that wraps it so `errors.Is` works and callers can see stage and
billing. Extend `underlyingError()` for fal 422 `content_policy_violation`, Stability 403
`content_moderation`, Kling 1300/1301, MiniMax 1026/1027, BFL `Request/Content Moderated`, Runway
`SAFETY.*`. Add `ErrPlanRequired` for ElevenLabs 402 on flows and `ErrAssetExpired` for stale URLs.

### 3.4 Cost

New `MediaPricing map[string]MediaRate` keyed `provider:model` (mirror of `modePricing` at
`modes.go:55`), `MediaRate{Unit MediaUnit; USD float64}` with units `image | megapixel | second |
minute | kchar | mtoken_out`. `CostTracker` gets a separate `media map[string]mediaTotals` so
`ModelUsage` stays comparable, plus `RecordMedia(provider, model, MediaUsage)`. Resolution order:
provider-reported `MediaUsage.Cost` → `MediaPricing` → `ok=false` (never a guessed zero, same rule as
`cost.go:164-185`). Add `internal/testutil` reconciliation test mirroring
`AssertKnownModelTokenPricingReconcilesWithDefaultPricing`.

### 3.5 Transport (`internal/httpclient`)

- `DoMultipart(ctx, method, path, fields map[string]string, files []MultipartFile, out any)`.
- `DoBinary(ctx, ...) (data []byte, contentType string, headers http.Header, err)` (wraps `DoRaw`,
  keeps the 100 MB cap, surfaces `character-cost`, `X-Generation-Id`, `X-Fal-Billable-Units`).
- `Poll(ctx, fn func(ctx) (done bool, err error), PollPolicy{Initial, Max, Multiplier, Jitter})`.
- `MediaAsset.Fetch` goes through the same SSRF-validated client (`security.go:68 ValidateURL`,
  redirect key-strip at `client.go:195-214`) since asset URLs are attacker-influenceable.

### 3.6 `pkg/openaicompat`

Add `CreateImage`, `CreateImageStream`, `CreateSpeech`, `CreateTranscription`, `CreateVideo`,
`GetVideo`, `GetVideoContent` on `Client` plus wire types in `types.go`. `ProviderConfig` gains
`Media MediaCapabilities{Images, ImageEdits, Speech, Transcription, Videos bool; ImagesPath,
SpeechPath, TranscriptionsPath, VideosPath string}`. `BaseProvider` implements the root interfaces
and returns `Err<X>NotSupported` when the flag is off, so every thin provider inherits media for
free once its config is flipped. Path overrides cover OpenRouter (`/images`, `/videos`) and Groq
(`/openai/v1/...`).

### 3.7 Registration

Providers that also chat (OpenRouter, xAI later) register normally and join `pkg/providers/all`.
Media-only providers (ElevenLabs, fal, Replicate, BFL, Deepgram) are direct-construct only, like
`infinity`, and are documented as `n/a (no chat)` in `docs/providers.md`. A parallel
`RegisterMediaProvider` is deferred until a second media-only provider ships.

## 4. Packets (each a separate PR, conventional `feat(<scope>):` title, gated ≥ A-)

| # | Packet | Scope | Live gate | Acceptance |
|---|---|---|---|---|
| D0 | **Core types + transport** | `imagegen.go`, `videogen.go`, `speech.go`, `media.go`, `models.go`, `capabilities_registry.go`, `errors.go`, `cost.go` media table, `internal/httpclient` multipart/binary/poll, `api/v6.txt` regen, `docs/guides/media.md` skeleton | none | unit tests for options apply, `MediaAsset.Fetch` SSRF rejection, `Poll` backoff, `RecordMedia` precedence (reported cost > table > ok=false), `ModelUsage` still comparable (compile-time `_ = map[ModelUsage]struct{}{}`) |
| D1 | **openaicompat media + OpenAI** | §3.6, OpenAI provider flips all five flags, `knownModels` gets `gpt-image-2`, `gpt-image-1.5`, `gpt-4o-mini-tts`, `gpt-transcribe`, `sora-2[-pro]` with `MediaPricing` rows | `OPENAI_API_KEY` (present) | mock-server routes in `internal/testutil/mockserver.go:117`; `//go:build integration` `TestLiveOpenAI_GenerateImage`, `_Synthesize`, `_Transcribe`, `_VideoJobWait` (sora, gated on a second env flag because of cost); Azure verified by base-URL unit test only |
| D2 | **OpenRouter provider** | `pkg/providers/openrouter` thin openaicompat chat provider (useful alone) + native `/images`, `/videos` (202 + `polling_url` + `/content`), audio via compat paths; `usage.cost` feeds `MediaUsage.Cost`; `EnvOpenRouterAPIKey`; `.env.example` | `OPENROUTER_API_KEY` (**absent from `.env`**) | `TestLiveOpenRouter_Chat`, `_GenerateImage` (`google/gemini-3.1-flash-image`), `_Speech`, `_Transcribe`; video live test gated separately |
| D3 | **ElevenLabs provider** | `pkg/providers/elevenlabs` native: TTS (+`/stream` → `StreamSpeech`, `/with-timestamps` → `Alignment`), STT `scribe_v2` multipart, SFX and music as `Synthesize` with model routing, flows image/video as `ImageGenerator`/`VideoGenerator` with 402 → `ErrPlanRequired`; `character-cost` header → `MediaUsage`; `EnvElevenLabsAPIKey`; package doc justifies native transport per `AGENTS.md` step 2b | `ELEVENLABS_API_KEY` (present) | `TestLiveElevenLabs_Synthesize` (`eleven_flash_v2_5`, cheapest), `_StreamSpeech`, `_Transcribe`, `_SoundEffect`; flows image test skips on 402; **before coding, probe `GET /v1/flows/image/{id}` since the poll path is unverified** |
| D4 | **Gemini media** | native image via `generateContent` `responseModalities`, Veo 3.1 LRO → `VideoJob` (Files download, 2 d TTL), TTS PCM → `SpeechResponse` with `Format` set to `pcm_s16le/24000`, `gemini-3.5-transcribe` via Interactions; pricing rows; skip Imagen | `GEMINI_API_KEY` (**absent from `.env`**) | `TestLiveGemini_GenerateImage`, `_Synthesize`, `_Transcribe`; Veo gated separately |
| D5 | **Thin-provider enablement** | flip `Media` flags on togetherai (all + native `/v2/videos`), groq (speech wav-only + transcriptions), featherless (speech), mistral (transcriptions), zai (images url-only, transcriptions, native async video); pricing rows | per-provider keys (`ZAI_TOKEN` present) | one mock test per flag per provider; live for Z.AI |
| D6 | **Dedicated providers** | in ranked order and only on explicit go: fal, Replicate, BFL (auto-fetch under 10 min TTL), Deepgram; each its own packet | new keys | per-provider live smoke |
| D7 | **Docs + release** | `docs/guides/media.md` full, `docs/providers.md` matrix rows + counts (`README.md:134,216`, `mkdocs.yml:2`), `examples/media/main.go`, `docs/roadmap.md` Track D ticks, CHANGELOG | none | mkdocs builds; nitpick review clean |

D0 → D1 are sequential. D2, D3, D4 are independent after D1 and can run in parallel. D5 after D1.
D6 is opt-in. D7 closes.

## 5. Risks and open questions

- **Cost of live tests.** Video jobs cost $0.40 to $2.40 per clip. Gate video live tests on a second
  env var (`LLM_SDK_LIVE_VIDEO=1`) and cap duration at the minimum the model allows.
- **Verified 2026-09-05 by probe** (was unverified in research):
  - ElevenLabs flows: the create/GET paths `POST /v1/flows/image`,
    `GET /v1/flows/image/{id}`, `POST /v1/flows/video`, `GET /v1/flows/video/{id}`
    and the 402 plan gate were verified by probe and docs. Non-Pro accounts get
    402 `{"detail":{"code":"paid_plan_required"}}`; the `.env` key is Creator tier.
    Poll transitions `pending|generating|completed|failed`, completed `content_url`
    (signed, ~1 h) + `content_mime_type`, and failed `failure_reason` values
    `timeout|model_error|moderated|invalid_parameters|dependency_failed|charging_failed|internal_error`
    (not charged) come from the API reference and remain **unobserved on the Creator key**.
    Flows live tests therefore skip on 402; successful polling is covered by mock tests.
  - OpenAI TTS `stream_format:"sse"`: `data: {"type":"speech.audio.delta","audio":"<b64>"}` repeated,
    then `{"type":"speech.audio.done","usage":{"input_tokens","output_tokens","total_tokens"}}`,
    then `data: [DONE]`. Not supported on `tts-1`/`tts-1-hd`.
  - Mistral TTS: `POST /v1/audio/speech` JSON `{model, input, voice_id|ref_audio, response_format
    pcm|wav|mp3|flac|opus, stream}` → `{"audio_data":"<b64>"}`; streaming is the same path with
    `stream:true` (event-stream, event schema undocumented); moderation → 403.
- **Still unverified**: Gemini TTS `mimeType` string; `gpt-image-2` per-image price table.
- **Two OpenAI image pricing units** coexist (per 1M image tokens and per-image approximations).
  Store the token rate as source of truth and compute from `usage.output_tokens`.
- **URL TTLs** make `MediaAsset` a footgun if callers hold it. `Wait` on providers with TTL under
  15 minutes (BFL) fetches bytes eagerly.
- **ElevenLabs image/video is a beta reseller surface** gated to Pro plans. Ship it behind the same
  interfaces but document it as best-effort.
- **Deprecation cliffs** within 60 days: Kling `kling-v2-1-master` 2026-09-15, Nova Canvas/Reel
  2026-09-30, Cartesia `sonic-2` 2026-10-20. `ModelInfo` should gain `DeprecatedAt *time.Time`
  in D0.
- **`.env` hygiene.** The file holds live keys in plaintext. Rotate any key that has been pasted
  into an agent context.
