# Vision & Multi-modal

Vision-capable models can analyze images alongside text: describe a photo, read a
chart, extract text from a screenshot, compare two pictures, and more. The SDK
exposes a small set of helpers for attaching images to messages, and the same
`GenerateContent` / `Stream` calls you already use carry the image through to the
provider.

Images are attached as **content parts** on a `llms.Message`. A message can hold
plain text (the `Content` field) *or* a list of `Parts` mixing text and images.
When `Parts` is non-empty it takes precedence over `Content`.

```go
import llms "github.com/nocturnium/llm-go-sdk/v6"
```

## Quick start

The fastest way to ask a question about an image at a URL:

```go
msg := llms.NewImageMessage("What do you see in this image?", "https://example.com/cat.png")

resp, err := client.GenerateContent(ctx, []llms.Message{msg})
if err != nil {
    log.Fatal(err)
}
fmt.Println(resp.Content)
```

`NewImageMessage(prompt, imageURL)` returns a `RoleUser` message containing one
text part and one image-URL part — it never returns an error.

## Constructing image messages

The SDK provides three message-level constructors and a set of lower-level
`ContentPart` builders.

### Message constructors

| Function | Signature | Notes |
| --- | --- | --- |
| `NewImageMessage` | `(prompt, imageURL string) Message` | User message: text + image URL. No error. |
| `NewImageFileMessage` | `(prompt, imagePath string) (Message, error)` | User message: text + image read from a local file. Errors if the file is missing, oversized, or an unsupported format. |
| `NewMultiPartMessage` | `(role Role, parts ...ContentPart) Message` | Full control: any role, any number of parts. |

### Content-part builders

`NewMultiPartMessage` is composed from `ContentPart` values. Build them with:

| Function | Signature | Source |
| --- | --- | --- |
| `NewTextPart` | `(text string) ContentPart` | Inline text |
| `NewImageURLPart` | `(url string) ContentPart` | Remote image by URL |
| `NewImageBase64Part` | `(data, mediaType string) ContentPart` | Pre-encoded base64 string |
| `NewImageFromBytes` | `(data []byte, mediaType string) ContentPart` | Raw bytes (encoded for you) |
| `NewImageFromFile` | `(path string) (ContentPart, error)` | Local file (media type auto-detected) |
| `NewImageFromReader` | `(r io.Reader, mediaType string) (ContentPart, error)` | Streaming source |

!!! note "URL vs. base64"
    `NewImageURLPart` does not set a media type — the provider fetches the URL.
    The base64 / file / bytes / reader builders embed the image inline and a
    media type is required (auto-detected for files).

### Mixing text and a remote image

```go
msg := llms.NewMultiPartMessage(llms.RoleUser,
    llms.NewTextPart("Look at this image and tell me:"),
    llms.NewTextPart("1. What colors do you see?"),
    llms.NewTextPart("2. What shapes are present?"),
    llms.NewImageURLPart("https://example.com/diagram.png"),
)

resp, err := client.GenerateContent(ctx, []llms.Message{msg})
```

### Multiple images in one message

Attach several image parts to compare or reason across them:

```go
msg := llms.NewMultiPartMessage(llms.RoleUser,
    llms.NewTextPart("Compare these two images. Are they the same or different?"),
    llms.NewImageURLPart("https://example.com/before.png"),
    llms.NewImageURLPart("https://example.com/after.png"),
)
```

### A system prompt plus an image

Vision messages compose with the rest of a conversation. Use a `RoleSystem`
message to steer the model, then attach the image on a user turn:

```go
conversation := []llms.Message{
    {Role: llms.RoleSystem, Content: "You are an art critic. Analyze images with an artistic perspective."},
    llms.NewImageMessage("What artistic elements do you notice here?", imageURL),
}

resp, err := client.GenerateContent(ctx, conversation)
```

## Sending a local image file

`NewImageFileMessage` reads the file, base64-encodes it, and detects the media
type from the file's magic bytes (falling back to the extension):

```go
msg, err := llms.NewImageFileMessage("Describe this image.", "diagram.png")
if err != nil {
    // missing file, > 20 MB, or unsupported format
    log.Fatal(err)
}

resp, err := client.GenerateContent(ctx, []llms.Message{msg})
```

If you need the image as a standalone part (for example to combine it with other
parts), use `NewImageFromFile` and `NewMultiPartMessage` directly:

```go
imgPart, err := llms.NewImageFromFile("receipt.jpg")
if err != nil {
    log.Fatal(err)
}

msg := llms.NewMultiPartMessage(llms.RoleUser,
    llms.NewTextPart("Extract the total amount and the date."),
    imgPart,
)
```

### From bytes or a reader

When the image lives in memory or behind an `io.Reader`, set the media type
explicitly:

```go
// From a []byte you already have in memory:
part := llms.NewImageFromBytes(pngBytes, llms.MediaTypePNG)

// From any reader (e.g. an http.Response body):
part, err := llms.NewImageFromReader(resp.Body, llms.MediaTypeJPEG)
if err != nil {
    log.Fatal(err)
}
```

## Supported formats and size limits

The SDK accepts four image formats, exposed as media-type constants:

| Constant | MIME type |
| --- | --- |
| `llms.MediaTypePNG` | `image/png` |
| `llms.MediaTypeJPEG` | `image/jpeg` |
| `llms.MediaTypeGIF` | `image/gif` |
| `llms.MediaTypeWebP` | `image/webp` |

The maximum inline image size is `llms.MaxImageSize` (**20 MB**). The file,
bytes, and reader builders reject anything larger before it ever reaches the
provider. Most providers enforce a similar 20 MB ceiling on base64 images, so
this guards both against local OOM and remote rejection.

!!! tip "Validate before sending"
    `llms.ValidateImageContent(img *ImageContent) error` checks that an
    `ImageContent` is well-formed — valid source (`url`/`base64`), non-empty
    data, a supported media type for base64 images, an estimated decoded size
    within `MaxImageSize`, and an `http(s)://` URL for URL images.

## Inspecting messages

A few helpers let you read image data back off a message:

```go
msg.HasParts()  // true if the message uses multi-part Parts
msg.HasImages() // true if any part is an image
msg.Images()    // []*llms.ImageContent — all image parts
msg.Text()      // concatenated text (Content, or all text parts joined by "\n")
```

Each `*llms.ImageContent` carries its `Source` (`llms.ImageSourceURL` or
`llms.ImageSourceBase64`), `MediaType`, and `Data` (the URL or the base64
string).

## Which providers support vision?

Vision is **model-dependent**. Check a client's capability flag at runtime
rather than hard-coding assumptions:

```go
if client.Capabilities().Vision {
    // safe to send images
}

// Or the interface-level helper, which works on any llms.LLM:
if llms.SupportsVision(client) {
    // ...
}
```

Providers whose default capability set advertises vision:

| Provider | Vision | Notes |
| --- | --- | --- |
| `openai` | Yes | GPT-4o and other GPT-4 vision models |
| `anthropic` | Yes | All Claude 3+ models |
| `gemini` | Yes | All Gemini models |
| `azure` | Yes | Model/deployment dependent |
| `groq` | Yes | Llama 3.2 Vision models |
| `mistral` | Yes | Pixtral models |
| `fireworks` | Yes | Llama 3.2 Vision |
| `togetherai` | Yes | Depends on model (e.g. Llama 3.2 Vision) |
| `synthetic` | Yes | Model dependent |
| `zai` | Yes | |
| `llamacpp` | Yes | Model dependent (LLaVA, etc.) |
| `cerebras` | No | |
| `deepseek` | No | Not currently supported |
| `perplexity` | No | |
| `featherless` | No | Most models do not support vision |
| `runpod` | No (default) | Depends on the deployed model |
| `ollama` | No (default) | Model dependent (llava, etc.) |
| `infinity` | No | Embeddings / reranking only |

!!! warning "Capability flags are provider-level defaults"
    The flag reflects whether the provider *can* serve vision with an
    appropriate model — not whether the specific model you configured supports
    it. For example, OpenAI reports `Vision: true`, but you must still pick a
    vision-capable model such as `gpt-4o`. Conversely, `ollama` and `runpod`
    default to `false` because it depends entirely on which model you load,
    even though they can run vision models. When in doubt, choose a known
    vision model and check `client.Model()`.

## Full runnable example

This sends a remote image to whichever vision-capable provider you have a key
for and prints the model's description.

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/providers/anthropic"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/providers/gemini"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/providers/openai"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client := createClient()
	if client == nil {
		log.Fatal("No API key found. Set OPENAI_API_KEY, ANTHROPIC_API_KEY, or GEMINI_API_KEY")
	}

	fmt.Printf("Using: %s (%s)\n", client.Provider(), client.Model())

	if !client.Capabilities().Vision {
		log.Fatalf("%s/%s does not support vision", client.Provider(), client.Model())
	}

	imageURL := "https://upload.wikimedia.org/wikipedia/commons/thumb/4/47/PNG_transparency_demonstration_1.png/280px-PNG_transparency_demonstration_1.png"

	msg := llms.NewImageMessage("What do you see in this image? Describe it briefly.", imageURL)

	resp, err := client.GenerateContent(ctx, []llms.Message{msg})
	if err != nil {
		log.Fatalf("vision request failed: %v", err)
	}
	fmt.Printf("Response: %s\n", resp.Content)
}

// createClient returns the first vision-capable client it can build from the
// environment. openai.New / anthropic.New / gemini.New return (*Client, error).
func createClient() llms.LLM {
	if os.Getenv("OPENAI_API_KEY") != "" {
		if c, err := openai.New(openai.WithModel("gpt-4o")); err == nil {
			return c
		}
	}
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		if c, err := anthropic.New(); err == nil {
			return c
		}
	}
	if os.Getenv("GEMINI_API_KEY") != "" || os.Getenv("GOOGLE_API_KEY") != "" {
		if c, err := gemini.New(); err == nil {
			return c
		}
	}
	return nil
}
```

!!! note "Streaming works too"
    Vision messages are just regular messages, so they also work with
    `client.Stream(ctx, []llms.Message{msg})` — the image is sent on the first
    request and the response streams back as text chunks.

## See also

- [Multi-modal example source](https://github.com/nocturnium/llm-go-sdk/blob/main/examples/vision/main.go)
- Structured outputs and tools — combine vision with `WithTools` or
  `GenerateTyped` to extract structured data from images.
