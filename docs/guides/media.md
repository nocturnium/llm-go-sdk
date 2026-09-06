# Media Generation

The media core defines provider-independent interfaces for images, video, speech,
and transcription. OpenAI, OpenRouter, ElevenLabs, Gemini and the thin providers below implement these contracts using shared and native media routes. Media capabilities are separate
from the `llms.LLM` chat interface.

## Interfaces

| Interface | Operations | Result |
| --- | --- | --- |
| `ImageGenerator` | `GenerateImage` | `ImageResponse` |
| `ImageEditor` | `EditImage` | `ImageResponse` |
| `VideoGenerator` / `VideoJob` | `GenerateVideo`, then `Poll`, `Wait`, or `Cancel` | `VideoResponse` |
| `SpeechSynthesizer` | `Synthesize`, `StreamSpeech` | `SpeechResponse` or `AudioChunk` stream |
| `Transcriber` | `Transcribe` | `Transcription` |

A successful `StreamSpeech` stream ends with a Data-less `AudioChunk` whose
`Usage` carries the request's accounting (character or token based, per
provider) when it is known; skip chunks with empty `Data` when concatenating
audio. Failed streams end with an `Err` chunk and report no usage.

Each request uses its own options: `ImageOption`, `VideoOption`, `SpeechOption`,
or `TranscribeOption`. Constructors carry the capability prefix, such as
`WithImageAspectRatio`, `WithVideoDuration`, `WithSpeechVoice`, and
`WithTranscribeDiarization`.

## Checking capabilities

`Supports*(any)` checks whether a value implements a media interface.
`AsImageGenerator`, `AsImageEditor`, `AsVideoGenerator`, `AsSpeechSynthesizer`,
and `AsTranscriber` also return the typed interface.

`Has*(LLM)` reads the corresponding `Capabilities` flag, unwrapping middleware:
`HasImageGeneration`, `HasVideoGeneration`, `HasSpeech`, and `HasTranscription`.
A flag and an interface assertion answer different questions; callers still handle
unsupported operations and provider errors.

## Fetching assets

A `MediaAsset` can contain inline `Data`, a download `URL`, or a `CloudURI`.
Given an asset returned by a media operation:

```go
func fetchAsset(ctx context.Context, asset *llms.MediaAsset) ([]byte, error) {
    data, err := asset.Fetch(ctx, nil)
    if err != nil {
        return nil, err
    }
    return data, nil
}
```

`Fetch` returns cached bytes first, otherwise downloads and caches the URL before
expiry. Expired URLs return `ErrAssetExpired`; cloud URIs require provider-specific
retrieval. Downloads enforce HTTPS and SSRF protection, and strip credential
headers on cross-host redirects. `FetchWithOptions` accepts explicit headers and
independent HTTP/private-IP opt-outs. Serialize access to an asset because fetching
mutates `Data`.

## Waiting for video

Given a `VideoGenerator`, submit a job and wait with a bounded context:

```go
func generateVideo(ctx context.Context, generator llms.VideoGenerator) (*llms.VideoResponse, error) {
    ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
    defer cancel()
    job, err := generator.GenerateVideo(ctx, "Clouds over a mountain",
        llms.WithVideoDuration(4))
    if err != nil {
        return nil, err
    }
    return job.Wait(ctx)
}
```

`Wait` handles polling. Failed jobs match `ErrJobFailed`; moderated jobs return a
`ModerationError` matching `ErrContentFiltered`. Cancellation may be unsupported,
in which case `Cancel` returns `ErrJobCancelNotSupported`.

## Cost tracking

`MediaPricing` stores USD rates by `provider:model` and billing unit. OpenAI, ElevenLabs, Gemini, Together AI, Groq, Featherless, Mistral and Z.AI rates are included;
configure any custom rates before concurrent use. Provider-reported `MediaUsage.Cost`
takes precedence over the table. `MediaCost` returns `ok=false` for missing rates
or mismatched units.

Use `CostTracker.RecordMedia(provider, model, usage)` to record usage and
`MediaTotals()` to read a snapshot keyed by `provider:model:unit`. Inspect each
total's `Unpriced` count for missing estimates. `GetTotalCost()` includes known
media and token costs. Media accounting leaves `ModelUsage` unchanged.

## Providers

OpenAI-compatible providers inherit the interfaces even when media flags are off;
unsupported operations return the corresponding `Err*NotSupported`.

| Provider | Images | Video | Speech | Transcription |
| --- | --- | --- | --- | --- |
| Gemini | Native generation + inline edits (`gemini-3.1-flash-image`); paid quota | Veo LRO + authenticated MP4 (`veo-3.1-lite-generate-preview`); paid quota | PCM-only TTS (`gemini-3.1-flash-tts-preview`); no streaming | Interactions (`gemini-3.5-transcribe`) |
| OpenAI | Generation + edits (`gpt-image-1.5`); ignores `Seed`, `AspectRatio`, `NegativePrompt`, `SafetyTolerance` | Async MP4 (`sora-2`); ignores `Audio`, `LastFrame`, `ReferenceImages`, `Seed`, `NegativePrompt` | Binary + SSE (`gpt-4o-mini-tts`) | Multipart (`gpt-4o-mini-transcribe`) |
| ElevenLabs | Flows generation + edits (`gemini-3.1-flash-lite-image`), Pro plan+ | Flows async (`veo-3.1-fast-generate-001`), Pro plan+ | Binary/chunked TTS, timestamps, dialogue, SFX/music | Scribe multipart (`scribe_v2`) |
| OpenRouter | Generation (`google/gemini-3.1-flash-lite-image`); native `/images` | Native async MP4 (`google/veo-3.1-lite`) | Binary mp3/pcm; optional cost lookup | Multipart JSON/verbose JSON (`openai/whisper-1`) |
| Together AI | Generation (`black-forest-labs/FLUX.1-schnell`), base64 or eager URL download | Native `/v2/videos` (`ByteDance/Seedance-2.5`) | Binary + raw SSE (`hexgrad/Kokoro-82M`) | Multipart (`openai/whisper-large-v3`) |
| Groq | — | — | WAV only (`canopylabs/orpheus-v1-english`), 200 characters | Transcription + translation (`whisper-large-v3-turbo`) |
| Featherless | — | — | Binary + SSE (`hexgrad/Kokoro-82M`) | — |
| Mistral | — | — | JSON base64 (`voxtral-mini-tts-2603`), no streaming | Multipart (`voxtral-mini-latest`) |
| Z.AI | One URL image (`cogview-4-250304`), eager download | Native async (`cogvideox-3`) | — | Multipart (`glm-asr-2512`), clips up to 30 seconds |

### Thin OpenAI-compatible providers

These routes and prices use the supplied first-party documentation verification
from 2026-09-05. **Live operation remains unverified:** Together, Groq, Featherless
and Mistral had no keys during verification; the Z.AI account returned code `1113`
(insufficient balance) on every media route. Integration tests are gated on
`TOGETHER_API_KEY`, `GROQ_API_KEY`, `FEATHERLESS_API_KEY`, `MISTRAL_API_KEY` and
`ZAI_API_KEY` respectively, and skip quota or plan errors. The generic `LLM_API_KEY`
fallback alone does not enable live tests.

Media models are independent of chat models; select alternatives with the
per-request `WithImageModel`, `WithSpeechModel`, `WithTranscribeModel`, and
`WithVideoModel` options. Image edits and streaming transcription are unsupported.
All native requests and downloads use the client's timeout and HTTP/private-IP
policy; no API credentials are sent to generated asset URLs.

- **Together AI:** defaults to `https://api.together.xyz/v1`. Images default to
  explicit JPEG and base64, with eager URL downloads using a User-Agent. Size
  `WxH` maps to width/height; AspectRatio, Seed, NegativePrompt and OutputFormat
  map directly. Extras merge last. Usage follows the model's `MediaPricing` unit:
  per-image rates count images; megapixel rates (including Schnell and Imagen 4.0
  Fast) require effective width/height, otherwise Unit stays empty. Quality and
  SafetyTolerance are ignored. Speech defaults to `af_bella`; Language maps to
  `language` and Format.SampleRate to `sample_rate`. Containers are WAV, MP3 and
  raw; `mulaw` belongs in Extra `response_encoding`. Speech counts Unicode
  characters / 1000. StreamSpeech defaults to raw and rejects other containers;
  it decodes `{object:"audio.tts.chunk", b64}` SSE frames until `[DONE]`, then
  emits the same per-character usage Synthesize reports.
  Transcription defaults to `verbose_json` for duration accounting; Extra can
  select `json`. It accepts uploads up to 80 MiB or a URL sent as the `file`
  string, maps diarization to `diarize`, and sends bracket-free
  `timestamp_granularities`. Words retain `speaker_id`.
  Video uses `/v2/videos`, stripping only a trailing `/v1` segment from BaseURL
  and preserving proxy prefixes. The default `ByteDance/Seedance-2.5` is the
  cheapest matching entry on the [pricing page](https://www.together.ai/pricing)
  and [catalog](https://docs.together.ai/docs/serverless/models) linked from the
  [video guide](https://docs.together.ai/docs/inference/videos/overview), checked
  2026-09-05; no static estimate is supplied for that default.
  Duration is sent as a string `seconds`; reported seconds take precedence for
  usage. Queued and unknown states keep polling; failed/cancelled states stop.
  Resolution, AspectRatio, Audio, Seed, NegativePrompt and OutputFormat map to
  native fields. First/last frames require URLs; typed ReferenceImages is rejected
  because its schema is unverified, while native `media.reference_images` can be
  passed through Extra. `outputs.cost` is retained as `Metadata["outputs_cost"]`:
  its billing unit is unverified, so it never becomes USD Cost. Known flat-job
  pricing rows use an empty unit and become explicit Cost with seconds retained.
- **Groq:** speech defaults to WAV and voice `autumn` only for
  `canopylabs/orpheus-v1-english`. Other models, including Arabic, require an
  explicit Voice. More than 200 Unicode characters or a non-WAV container
  (including Extra `response_format`) fails before HTTP. Speech streaming is
  unsupported. `Translate` uses `/audio/translations` with Transcribe's options.
  Both accept inline audio or URLs and default to `verbose_json` for duration;
  Extra can select JSON or text. Word timestamps use `timestamp_granularities[]`.
  Diarization is unsupported. Uploads are capped at 25 MiB; Groq also documents
  a 100 MiB developer tier, which this conservative SDK cap does not enable.
- **Featherless:** speech defaults to `af_bella` and MP3; Opus, AAC, FLAC, WAV and
  PCM are also accepted, subject to model support. Unary usage counts Unicode
  characters / 1000. The [request pricing table](https://featherless.ai/docs/request-pricing-and-credits)
  confirms Kokoro, Orpheus and Chatterbox rates of $0.004, $0.015 and $0.025 per
  thousand characters. StreamSpeech sets `stream:true` and `stream_format:sse`,
  decoding `speech.audio.delta/done`. A successful stream ends
  with a Data-less `AudioChunk` whose `Usage` is the reported
  `usage.input_characters` (KChar), or the request's Unicode character count when
  the done event carries none. `featherless.WithSpeechUsageHandler` additionally
  receives the reported usage plus the model; the callback runs before channel
  close and must return promptly and handle concurrent streams. Failed streams
  report no usage. Instructions and speed (0.25–4) are
  accepted, with effects dependent on the model.
- **Mistral:** speech maps Voice to `voice_id`; `ref_audio` is available through
  Extra when no voice is specified. JSON `audio_data` is decoded from base64.
  MP3 is the default; PCM, WAV, FLAC and Opus are accepted, with MIME types
  `audio/mpeg`, `audio/L16`, `audio/wav`, `audio/flac` and `audio/ogg` respectively.
  Only moderation/content error codes map to ModerationError; a plain 403 retains
  normal status classification. Streaming speech is unsupported. Transcription
  accepts uploads up to 25 MiB or `file_url`, maps timestamps and diarization to
  `timestamp_granularities` and `diarize`, and sends repeated `context_bias`
  fields for Keyterms. `usage.prompt_audio_seconds` bills minutes (seconds / 60).
  Prompt has no verified mapping and is ignored.
- **Z.AI:** images omit `n` and `response_format`; those extras are rejected.
  Other extras merge last; effective model/prompt and quality are validated.
  Seed, NegativePrompt, AspectRatio, OutputFormat and SafetyTolerance are ignored.
  Images retain URL and a thirty-day expiry and eagerly fill Data. Filtering
  without image data returns ModerationError. Async images are unsupported.
  Video maps duration (5/10), Resolution to Size, Audio to `with_audio`, and URL
  first/last frames to `image_url`. Extra accepts native `quality` and `fps`
  (30/60); polling uses `/async-result/{id}`. The `cogvideox-3` flat $0.20 rate
  comes from MediaPricing and becomes explicit Cost, retaining seconds quantity.
  Transcription accepts inline WAV (`audio/wav`) or MP3 (`audio/mpeg`, `audio/mp3`)
  up to 25 MiB, maps Keyterms to `hotwords[]`, and remains unpriced. Code `1113`
  matches ErrQuotaExceeded; `1301` maps to input-stage ModerationError.
  The workspace `.env` uses `ZAI_TOKEN`, while the SDK and live-test gate read
  `ZAI_API_KEY`; export the latter explicitly to run live tests.

Speech Language is ignored by Groq, Featherless and Mistral. All media routes
remain live-unverified; the changes above are covered by mock HTTP tests.

Transcription charges use duration only when the vendor reports it; missing
duration is not inferred from upload size. Native video jobs use `PollingVideoJob`;
Cancel returns `ErrJobCancelNotSupported`. Always use bounded contexts for Wait
and streams. Video extras use native vendor field names; resolved duration is
validated and used for accounting.

### OpenRouter

`GenerateImage` maps `WithImageAspectRatio` and `WithImageSeed` to native body
keys. Pass `resolution` (for example `"1K"`) and `input_references` through
`WithImageExtra`; extras merge last and override typed options. The provider's
`media_type` overrides the requested image MIME type. Image edits, image streaming
on the core interface, speech SSE and streaming transcription are unsupported.
`NegativePrompt` and `SafetyTolerance` have no verified image wire mapping.

`ListImageModels` and `ListVideoModels` return typed native discovery entries.
Speech defaults to mp3 and `fish-audio/s2.1-pro`, live-verified on 2026-09-05 via
[GET /models?output_modalities=speech](https://openrouter.ai/api/v1/models?output_modalities=speech).
`ListSpeechModels` returns this catalog and reported `pricing.input_char`.
An unset voice is omitted; providers choose their defaults or require an explicit
`WithSpeechVoice`. Transcription uploads use `audio.<ext>` filenames derived from
`MediaInput.MIMEType` and include the content type. Empty or unsupported MIME types
return `ErrInvalidParameters` listing supported audio types.

Image, transcription and video responses retain `usage.cost` without static
`MediaPricing` rows. Transcription usage with seconds and no type bills minutes.
`openrouter.WithUsageLookup()` enables one post-speech `GET /generation?id=...`
using `X-Generation-Id`. Missing IDs or costs leave usage unknown. Lookup failures
leave Cost nil and return successful audio.
`SpeechResponse.Metadata["generation_id"]` retains `X-Generation-Id` even when
lookup is disabled. Call `client.GenerationCost(ctx, id)` to retrieve cost later
or handle lookup errors explicitly. `WithSpeechExtra` forwards `input_references`
and `provider`; standard speech fields cannot be overridden by extras.
This lookup is disabled by default.

Video sends integer `duration`, `resolution`, `aspect_ratio`, `generate_audio`,
`seed`, and first/last `frame_images`. Frames accept URL or inline image Data;
FileID and typed ReferenceImages are rejected because their mappings are unverified.
Frames serialize as `{type:"image_url", image_url:{url:...}, frame_type:...}`.
Pass native `input_references` and HTTPS-only `callback_url` through `WithVideoExtra`.
Additional extras (`size`, `provider`, `upscale_factor`, `creativity` and model-specific
passthrough keys) merge last; typed field names remain reserved, including when
unset. `NegativePrompt` and `output_format` are ignored because neither appears in
the [video request schema](https://openrouter.ai/openapi.json), checked 2026-09-05.
Moderation uses input stage until `in_progress` has been observed, then output stage.
An unspecified duration leaves the video usage unit empty.
`Wait` downloads every content index and retains each unsigned URL. Expired jobs
wrap `ErrAssetExpired`; moderation maps to `ModerationError`. Cancellation is
unsupported. The default `google/veo-3.1-lite` has the lowest directly comparable
published per-second rate ($0.03 at 720p without audio) in discovery on 2026-09-05;
use a supported duration (minimum 4 seconds), `WithVideoResolution("720p")` and
`WithVideoAudio(false)` for that rate. Token-priced models cannot be ranked without
a token conversion formula. The separately gated 480p live test uses the listed
`x-ai/grok-imagine-video` model at one second ($0.05 minimum clip).

### OpenAI

OpenAI image edits and transcription accept inline `MediaInput.Data` uploads.
Set `MIMEType` so audio receives the correct upload extension. Word timestamps
select `verbose_json`, supported only with `whisper-1`; diarization selects
`diarized_json`, supported only with `gpt-4o-transcribe-diarize`. Incompatible
model/format combinations (including requesting both options) fail with
`ErrInvalidParameters` before a request is sent. The default format is `json`,
which also carries usage. Explicit usage takes precedence over `duration`:
Whisper duration usage bills minutes; GPT transcription token usage accounts for
audio input, text input, and text output. The two verified GPT transcription
models have converter-computed `Usage.Cost`; unverified models remain unpriced.
Select `text`, `srt`, or `vtt` through `WithTranscribeExtra` using the
`response_format` key; the raw text or subtitles are returned in `Text`.
Streaming transcription is not implemented.

Speech defaults to voice `alloy` and container `mp3`. `tts-1` and `tts-1-hd` do
not support SSE. Binary speech has no reported usage and leaves the billing unit empty.
`gpt-4o-mini-tts` can be priced only from SSE done token usage, which `StreamSpeech`
delivers as a terminal Data-less `AudioChunk` carrying `Usage` (`CreateSpeechStream`
exposes the raw done event).

Video supports 4, 8, or 12 seconds and defaults to landscape 720p. Sora prices
cover only 720p; higher-resolution results remain unpriced. Job cancellation is
unsupported. Always pass a bounded context to streams and video `Wait`.


### ElevenLabs

Construct `elevenlabs.New()` directly using `ELEVENLABS_API_KEY` (or
`WithAPIKey`, then `LLM_API_KEY` fallback). It implements all five media interfaces
but no chat interface, so it is absent from the registry and `pkg/providers/all`.
`WithBaseURL` supports the regional HTTPS hosts. HTTPS and public-IP restrictions
apply to API calls and signed content downloads. The default timeout is 120 seconds.

TTS defaults to `eleven_flash_v2_5`, Rachel (`21m00Tcm4TlvDq8ikWAM`), and
`mp3_44100_128`. Override voice with `elevenlabs.WithVoice` or `llms.WithSpeechVoice`.
Use `AudioFormat{Container:"mp3", SampleRate:44100, BitRate:64000}` for
`mp3_44100_64`; BitRate is bits/second. Empty container/sample rate use mp3/44100,
empty compressed bitrate uses 128000; uncompressed formats have no bitrate selector.
`WithVoiceSettings` controls native voice settings. `WithSpeechTimestamps(true)`
selects the JSON timestamp route and converts character timing to milliseconds;
normalized alignment is retained in Metadata. StreamSpeech emits raw chunks, then a
terminal usage chunk (or an error); drain it or cancel its context. A full buffer loses one buffered
audio chunk on the error path to guarantee terminal error delivery without blocking.
Timestamps are unary only. `SynthesizeDialogue` accepts ordered
`DialogueLine{Text, VoiceID}` values and retains voice settings and language. It
defaults to `eleven_v3` independently of the client speech model; that is the only
model supporting this route, and other explicit models are rejected.
TTS usage always uses Unicode rune count / 1000 with unit KChar. `character-cost`
is a plan-scaled credit counter, not a character count; valid nonnegative integer
headers populate `Metadata["character_cost"]` as int64 credits without changing usage.

All 28 output formats are supported: the MP3 set includes `mp3_24000_48`, PCM
includes `pcm_32000`, and WAV supports 8000, 16000, 22050, 24000, 32000, 44100
and 48000 Hz. The existing MP3, Opus, PCM, u-law and A-law formats remain supported.

Speech Extra passes through additional native fields. Typed `model_id`, `text`/`prompt`,
`voice_settings` and `language_code` remain authoritative, including when omitted;
SFX/music extra duration and boolean fields are validated. Dialogue inputs also
remain authoritative, and the caller's Extra map is not modified.

**SFX/music routing:** `Synthesize` sends resolved model IDs beginning with
`eleven_text_to_sound` to `/v1/sound-generation`; use model
`eleven_text_to_sound_v2` and `WithSpeechExtra` keys `duration_seconds` (0.5–30),
`prompt_influence` (0–1) and `loop`. IDs beginning with `music_v` route to
`/v1/music` (`music_v2` or `music_v1`); extras are `music_length_ms`
(3000–600000, default 10000) and `force_instrumental`. Usage is the requested
seconds / 60 in minutes. Auto-length SFX leaves usage unknown because the response
contains no verified duration. Neither route supports StreamSpeech or timestamps.

Transcription accepts Data plus a supported audio/video MIMEType, or an HTTPS URL
as `source_url` (replacing deprecated `cloud_storage_url`); FileID is rejected. Upload filenames preserve the MIME
extension and content type. Scribe defaults to word timestamps; diarization and
repeated keyterms map directly. Extras accept `num_speakers` (1–32),
`timestamps_granularity` (`word`, `character`, `none`) and `tag_audio_events`.
Only entries of type `word` become TranscriptWords. Duration and minute usage
prefer `audio_duration_secs`, including with `timestamps_granularity: "none"`.
Only when that duration is zero do they fall back to the maximum end in the provider
words array (including spacing/audio events). Unit is empty when neither duration
source is available.

**Flows plan gate:** image and video are beta reseller APIs requiring Pro or above.
HTTP 402 wraps `ErrPlanRequired`, including the provider message. GenerateImage
creates, polls, then downloads; GenerateVideo returns `*llms.PollingVideoJob`.
Use bounded contexts or `elevenlabs.WithPollPolicy`; cancellation is unsupported.
Downloads retain signed URL, bytes, MIME type and a conservative 55-minute expiry.
No API key is sent to content URLs. Moderation is input-stage until `generating`
is observed, then output-stage; failed Flows are not charged.

Image options map AspectRatio, Size (`1K`, `2K`, `4K`, `512`), Seed (Seedream only),
and Quality (`low`, `medium`, `high`, gpt-image only). Extra merges last, including
`webhook`, native asset/generation references, and `background` for gpt-image-1/1.5.
EditImage accepts inline Data and image MIMEType; URLs and FileID are rejected.
Only one image is generated per call. Video maps Duration, Audio (default true),
FirstFrame/LastFrame, AspectRatio, Resolution, NegativePrompt and Seed. Extra merges
last, including `enhance_prompt` and native asset/generation frames.
Veo durations are 4/6/8 seconds; Seedance supports 1–15 (server default 5).
Typed ReferenceImages is rejected. Image NegativePrompt/OutputFormat/SafetyTolerance,
video OutputFormat, speech Instructions and transcription Prompt have no verified
mapping and are ignored. Flows image credit pricing remains unknown (empty Unit);
video usage records explicitly specified duration in seconds, with Cost nil.


### Gemini

Construct `gemini.New()` with `GEMINI_API_KEY` or `WithAPIKey`; `GOOGLE_API_KEY`
and then `LLM_API_KEY` are fallbacks. Media models are independent of the chat
model. Override defaults with `gemini.WithImageModel`, `WithVideoModel`,
`WithSpeechModel`, `WithSpeechVoice`, and `WithTranscriptionModel`, or the
corresponding per-request `llms` options. Media methods use the shared HTTP
client timeout (5 minutes by default); `WithTimeout` changes it.

Native images use `generateContent`, defaulting to size `1K` and aspect `1:1`.
Case-insensitive sizes `512`, `1K`, `2K`, and `4K` map to `imageConfig.imageSize`; supported sizes
and aspect ratios depend on the model. Edits accept inline Data with an image
MIMEType; URL/FileID inputs are rejected. Text response parts are ignored.
Only one candidate is requested. Imagen is not supported.

Veo defaults to 8 seconds, `720p`, `16:9`, with audio. Duration accepts 4/6/8,
resolution accepts case-insensitive `720p`/`1080p`/`4k`, and aspect accepts `16:9`/`9:16`.
Inline first/last frames and up to three reference images are supported, along
with Seed and NegativePrompt. `WithVideoExtra` accepts `personGeneration`.
Disabling audio is rejected because its native wire mapping is unverified.
`Wait` downloads MP4 using `x-goog-api-key`, retains the URL and bytes, and sets
a conservative 47-hour expiry. Download hosts must be
`generativelanguage.googleapis.com` or the configured baseURL host (including its
port). HTTP/private-IP opt-outs never allow arbitrary download hosts; transport
HTTPS and SSRF protections still apply independently. The terminal operation is
retained by Poll and reused by Wait without another status request.
Filtering returns an output-stage `ModerationError`; failed operations match
`ErrJobFailed`. Cancellation is unsupported. Use a bounded context or
`gemini.WithPollPolicy` for video and transcription polling.

Image/video models require paid quota. Free-tier keys can return HTTP 429 with
`free_tier` or `limit: 0`; live image/video tests skip those conditions. Video
live tests additionally require `LLM_SDK_LIVE_VIDEO=1`.

TTS defaults to voice `Kore` and returns raw mono PCM s16le at 24000 Hz. The
response MIME rate is parsed into `Format.SampleRate`. Container must be unset
or `pcm`; other containers return `ErrInvalidParameters`. No conversion to WAV
or compressed audio is performed. Without Instructions, input is prefixed with
`Say: ` to avoid the model's text-output refusal; with Instructions, the prompt
is `<Instructions>: <text>`. `StreamSpeech` returns `ErrSpeechStreamNotSupported`.
`WithSpeechExtra` accepts `multiSpeakerVoiceConfig` with native
`speakerVoiceConfigs` entries (`speaker`, `voiceConfig.prebuiltVoiceConfig.voiceName`)
in place of the single voice.

Transcription accepts inline Data or an audio URL with an audio MIMEType.
Caller URLs use the client HTTP/private-IP policy; HTTPS/public hosts are the default.
Language maps to `language_codes`, Keyterms to `custom_vocabulary`. Default mode
is `smart`; Diarize and WordTimestamps select `verbatim` with speaker labels and
word timing. `WithTranscribeExtra` accepts `mode` (`smart`, `verbatim`, or the
native verbatim object). Smart mode cannot combine with diarization/timestamps;
custom vocabulary cannot combine with either, even via extras. Conflicts return
`ErrInvalidParameters` before I/O. Word annotations populate Words, Speaker and
duration (maximum word end); text items are concatenated. In-progress interactions
are polled to completion, including unknown non-terminal statuses; failed/cancelled
statuses and error objects terminate polling. Transcription.Language stays empty
because the verified response does not report a detected language.

Image costs use the published per-model size table; missing sizes leave Unit empty
and Cost nil so MediaCost cannot fall back to an unrelated base rate.
Veo costs use model/resolution rates with audio. Root `MediaPricing` rows are
only the 1K/720p base estimates. Missing exact image/video model/variant prices
remain Unpriced in RecordMedia. TTS Cost includes prompt and candidate tokens, with output
million-token usage. Transcription Cost uses $2/M audio-input tokens and $12/M
text-output tokens from a per-model rate table. Output tokens sum text-modality
`candidates_tokens_details` across `model_invocation_token_counts`;
`total_output_tokens` is used only when the invocation array is absent.
Minute quantity is **derived** as audio tokens / 25 / 60,
not measured audio duration. Unknown models remain without converter Cost.

Image Seed/NegativePrompt/Quality/OutputFormat/SafetyTolerance, video OutputFormat,
speech Language/Speed/Timestamps, and transcription Prompt have no verified native
mapping and are ignored. Other extras are rejected rather than forwarded as guessed
wire fields.
