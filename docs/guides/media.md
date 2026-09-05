# Media Generation

The media core defines provider-independent interfaces for images, video, speech,
and transcription. D0 supplies the contracts and transport helpers; provider
implementations follow in D1 and later packets. Media capabilities are separate
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
        llms.WithVideoDuration(5))
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

`MediaPricing` stores USD rates by `provider:model` and billing unit. It is empty
in D0; configure rates before concurrent use. Provider-reported `MediaUsage.Cost`
takes precedence over the table. `MediaCost` returns `ok=false` for missing rates
or mismatched units.

Use `CostTracker.RecordMedia(provider, model, usage)` to record usage and
`MediaTotals()` to read a snapshot keyed by `provider:model:unit`. Inspect each
total's `Unpriced` count for missing estimates. `GetTotalCost()` includes known
media and token costs. Media accounting leaves `ModelUsage` unchanged.

## Providers

D1+ will fill this table as provider implementations ship.

| Provider | Images | Video | Speech | Transcription |
| --- | --- | --- | --- | --- |
