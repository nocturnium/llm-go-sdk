# Media Generation

The media core defines provider-independent interfaces for images, video, speech,
and transcription. OpenAI, OpenRouter and ElevenLabs implement these contracts using shared and native media routes. Media capabilities are separate
from the `llms.LLM` chat interface.

## Interfaces

| Interface | Operations | Result |
| --- | --- | --- |
| `ImageGenerator` | `GenerateImage` | `ImageResponse` |
| `ImageEditor` | `EditImage` | `ImageResponse` |
| `VideoGenerator` / `VideoJob` | `GenerateVideo`, then `Poll`, `Wait`, or `Cancel` | `VideoResponse` |
| `SpeechSynthesizer` | `Synthesize`, `StreamSpeech` | `SpeechResponse` or `AudioChunk` stream |
| `Transcriber` | `Transcribe` | `Transcription` |

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

`MediaPricing` stores USD rates by `provider:model` and billing unit. OpenAI and ElevenLabs rates are included;
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
| OpenAI | Generation + edits (`gpt-image-1.5`); ignores `Seed`, `AspectRatio`, `NegativePrompt`, `SafetyTolerance` | Async MP4 (`sora-2`); ignores `Audio`, `LastFrame`, `ReferenceImages`, `Seed`, `NegativePrompt` | Binary + SSE (`gpt-4o-mini-tts`) | Multipart (`gpt-4o-mini-transcribe`) |
| ElevenLabs | Flows generation + edits (`gemini-3.1-flash-lite-image`), Pro plan+ | Flows async (`veo-3.1-fast-generate-001`), Pro plan+ | Binary/chunked TTS, timestamps, dialogue, SFX/music | Scribe multipart (`scribe_v2`) |
| OpenRouter | Generation (`google/gemini-3.1-flash-lite-image`); native `/images` | Native async MP4 (`google/veo-3.1-lite`) | Binary mp3/pcm; optional cost lookup | Multipart JSON/verbose JSON (`openai/whisper-1`) |

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
`gpt-4o-mini-tts` can be priced only from SSE done token usage. `CreateSpeechStream` exposes
terminal token usage; the core `AudioChunk` interface has no usage field.

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
normalized alignment is retained in Metadata. StreamSpeech emits raw chunks and
terminal errors; drain it or cancel its context. A full buffer loses one buffered
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
