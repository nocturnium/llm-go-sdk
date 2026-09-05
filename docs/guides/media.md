# Media Generation

The media core defines provider-independent interfaces for images, video, speech,
and transcription. OpenAI implements these contracts using shared media routes. Media capabilities are separate
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

`MediaPricing` stores USD rates by `provider:model` and billing unit. OpenAI rates are included;
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
