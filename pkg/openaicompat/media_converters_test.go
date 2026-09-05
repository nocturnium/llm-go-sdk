package openaicompat

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

func TestMediaImageConverters(t *testing.T) {
	opts := &llms.ImageOptions{Model: "override", N: 2, Size: "1024x1024", Quality: "high", OutputFormat: "jpeg", Extra: map[string]any{"n": 3, "background": "transparent"}}
	req := BuildImageRequest("default", "moon", opts)
	if req.Model != "override" || req.Size != opts.Size || req.Quality != "high" {
		t.Fatalf("request: %+v", req)
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}
	if body["n"] != float64(3) || body["background"] != "transparent" {
		t.Fatal(body)
	}
	if BuildImageRequest("default", "moon", &llms.ImageOptions{}).Model != "default" {
		t.Fatal("missing default")
	}
	for _, format := range []string{"", "png", "jpeg", "webp"} {
		out, err := ConvertImageResponse(&ImageResponse{Data: []ImageData{{B64JSON: "aGk=", RevisedPrompt: "revision"}, {URL: "https://example.com/image"}}, Usage: &ImageUsage{OutputTokens: 40}}, "model", format)
		if err != nil {
			t.Fatal(err)
		}
		expected := format
		if expected == "" {
			expected = "png"
		}
		if string(out.Images[0].Data) != "hi" || out.Images[0].RevisedPrompt != "revision" || out.Images[1].URL == "" || out.Images[0].MIMEType != "image/"+expected || out.Usage.Quantity != 0.00004 {
			t.Fatalf("response: %+v", out)
		}
	}
	out, err := ConvertImageResponse(&ImageResponse{}, "model", "")
	if err != nil || out.Images == nil || out.Usage.Unit != "" {
		t.Fatalf("empty: %+v %v", out, err)
	}
	if _, err := ConvertImageResponse(&ImageResponse{Data: []ImageData{{B64JSON: "!"}}}, "", ""); err == nil {
		t.Fatal("accepted malformed base64")
	}
	if _, err := json.Marshal(ImageGenerationRequest{ExtraBody: map[string]any{"bad": make(chan int)}}); err == nil {
		t.Fatal("accepted invalid JSON")
	}
	if _, err := marshalMediaExtra(make(chan int), nil); err == nil {
		t.Fatal("accepted invalid value")
	}
	if _, err := marshalMediaExtra([]int{1}, nil); err == nil {
		t.Fatal("accepted non-object")
	}
}

func TestMediaSpeechConverters(t *testing.T) {
	req := BuildSpeechRequest("default", "你好", &llms.SpeechOptions{})
	if req.Model != "default" || req.ResponseFormat != "mp3" || req.Voice != "alloy" {
		t.Fatalf("defaults: %+v", req)
	}
	speed := 1.5
	req = BuildSpeechRequest("default", "你好", &llms.SpeechOptions{Model: "custom", Voice: "nova", Instructions: "soft", Speed: &speed, Format: llms.AudioFormat{Container: "wav"}})
	if req.Model != "custom" || req.Voice != "nova" || req.Instructions != "soft" || *req.Speed != speed {
		t.Fatalf("request: %+v", req)
	}
	out := ConvertSpeechResponse([]byte("RIFF"), "audio/wav", req, nil)
	if out.Usage.Quantity != 0 || out.Usage.Unit != "" || out.Format.Container != "wav" || out.Audio.MIMEType != "audio/wav" {
		t.Fatalf("response: %+v", out)
	}
	out = ConvertSpeechResponse(nil, "", req, &ImageUsage{OutputTokens: 20})
	if out.Usage.Unit != llms.MediaUnitMTokenOut || out.Usage.Quantity != .00002 {
		t.Fatal(out.Usage)
	}
	if chunk := convertSpeechEvent(SpeechStreamEvent{Audio: "aGk="}); string(chunk.Data) != "hi" || chunk.Err != nil {
		t.Fatal(chunk)
	}
	if chunk := convertSpeechEvent(SpeechStreamEvent{Audio: "!"}); chunk.Err == nil {
		t.Fatal("invalid base64")
	}
	if chunk := convertSpeechEvent(SpeechStreamEvent{Err: llms.ErrTimeout}); !errors.Is(chunk.Err, llms.ErrTimeout) {
		t.Fatal(chunk)
	}
}

func TestMediaTranscriptionConverters(t *testing.T) {
	for _, tc := range []struct {
		diarize, words bool
		format         string
		granularities  []string
	}{
		{false, false, "json", nil}, {false, true, "verbose_json", []string{"word"}}, {true, false, "diarized_json", nil}, {true, true, "diarized_json", nil},
	} {
		req := BuildTranscriptionRequest("default", llms.MediaInput{Data: []byte("audio")}, &llms.TranscribeOptions{Diarize: tc.diarize, WordTimestamps: tc.words, Language: "en", Prompt: "words"})
		if req.ResponseFormat != tc.format || !reflect.DeepEqual(req.TimestampGranularities, tc.granularities) || req.Model != "default" || req.Language != "en" || req.Prompt != "words" {
			t.Fatalf("request: %+v", req)
		}
	}
	resp := &TranscriptionResponse{Text: "Hello", Language: "en", Duration: 120, Segments: []TranscriptionSegment{{Text: "Hello", Start: 1, End: 2, Speaker: "A"}}, Words: []TranscriptionWord{{Word: "Hello", Start: 1, End: 2}}}
	out := ConvertTranscriptionResponse(resp, "model")
	if out.Usage.Quantity != 2 || out.Usage.Unit != llms.MediaUnitMinute || out.Segments[0].Speaker != "A" || out.Words[0].End != 2 || out.DurationSeconds != 120 {
		t.Fatalf("response: %+v", out)
	}
	if out := ConvertTranscriptionResponse(&TranscriptionResponse{}, ""); out.Usage.Unit != "" || out.Segments == nil || out.Words == nil {
		t.Fatal(out)
	}
}

func TestMediaVideoConverters(t *testing.T) {
	for _, tc := range []struct{ resolution, aspect, size string }{
		{"", "", "1280x720"}, {"", "9:16", "720x1280"}, {"720p", "16:9", "1280x720"}, {"720p", "9:16", "720x1280"}, {"1024p", "16:9", "1792x1024"}, {"1024p", "9:16", "1024x1792"}, {"garbage", "1:1", "1280x720"}, {"720x1280", "", "720x1280"}, {"1280x720", "", "1280x720"}, {"1024x1792", "", "1024x1792"}, {"1792x1024", "", "1792x1024"},
	} {
		if got := videoSize(tc.resolution, tc.aspect); got != tc.size {
			t.Fatalf("%+v: %s", tc, got)
		}
	}
	if req := BuildVideoRequest("default", "moon", &llms.VideoOptions{}); req.Seconds != "" || req.Model != "default" || req.Size != "1280x720" {
		t.Fatal(req)
	}
	for _, frame := range []llms.MediaInput{{URL: "https://example.com/image"}, {FileID: "file_1"}, {Data: []byte("hi"), MIMEType: "image/png"}} {
		req := BuildVideoRequest("default", "moon", &llms.VideoOptions{Model: "custom", DurationSeconds: 4, FirstFrame: &frame})
		if req.Model != "custom" || req.Seconds != "4" || req.InputReference == "" {
			t.Fatal(req)
		}
	}
	for _, tc := range []struct {
		wire  string
		state llms.JobState
	}{{"queued", llms.JobQueued}, {"in_progress", llms.JobRunning}, {"completed", llms.JobSucceeded}, {"failed", llms.JobFailed}, {"unknown", llms.JobFailed}} {
		progress := 50.0
		status := ConvertVideoStatus(&VideoObject{Status: tc.wire, Progress: &progress})
		if status.State != tc.state || *status.Progress != .5 {
			t.Fatal(status)
		}
	}
	for _, code := range []string{"moderation_failed", "CONTENT_POLICY_VIOLATION", "internal_error"} {
		status := ConvertVideoStatus(&VideoObject{Status: "failed", Error: &VideoError{Code: code, Message: "failed"}})
		if status.Err == nil {
			t.Fatal("missing cause")
		}
		if code != "internal_error" {
			var moderation *llms.ModerationError
			if status.State != llms.JobModerated || !errors.As(status.Err, &moderation) || !errors.Is(status.Err, llms.ErrContentFiltered) {
				t.Fatal(status)
			}
		} else if status.State != llms.JobFailed {
			t.Fatal(status)
		}
	}
}

func TestTranscriptionUsage(t *testing.T) {
	for _, tc := range []struct {
		name, model, payload string
		unit                 llms.MediaUnit
		quantity             float64
		cost                 *float64
	}{
		{"duration", "whisper-1", `{"duration":999,"usage":{"type":"duration","seconds":120}}`, llms.MediaUnitMinute, 2, nil},
		{"tokens", "gpt-4o-transcribe", `{"duration":999,"usage":{"type":"tokens","total_tokens":25,"input_tokens":16,"input_token_details":{"text_tokens":4,"audio_tokens":12},"output_tokens":9}}`, llms.MediaUnitMTokenOut, .000009, floatPointer(.000172)},
		{"mini", "gpt-4o-mini-transcribe", `{"usage":{"type":"tokens","input_token_details":{"text_tokens":4,"audio_tokens":12},"output_tokens":9}}`, llms.MediaUnitMTokenOut, .000009, floatPointer(.000086)},
		{"unpriced", "gpt-4o-transcribe-diarize", `{"usage":{"type":"tokens","output_tokens":9}}`, llms.MediaUnitMTokenOut, .000009, nil},
		{"unknown usage", "whisper-1", `{"duration":120,"usage":{"type":"unknown"}}`, "", 0, nil},
		{"absent", "whisper-1", `{"duration":120}`, llms.MediaUnitMinute, 2, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var response TranscriptionResponse
			if err := json.Unmarshal([]byte(tc.payload), &response); err != nil {
				t.Fatal(err)
			}
			out := ConvertTranscriptionResponse(&response, tc.model)
			if out.Usage.Unit != tc.unit || out.Usage.Quantity != tc.quantity {
				t.Fatal(out.Usage)
			}
			if tc.cost == nil {
				if out.Usage.Cost != nil {
					t.Fatal("unexpected price")
				}
			} else if out.Usage.Cost == nil || *out.Usage.Cost != *tc.cost {
				t.Fatalf("cost: %+v", out.Usage)
			}
			if tc.name == "duration" && out.DurationSeconds != 120 {
				t.Fatal(out.DurationSeconds)
			}
		})
	}
}

func floatPointer(v float64) *float64 { return &v }

func TestVideoStatusCostAndUnknown(t *testing.T) {
	for _, obj := range []VideoObject{{Status: "mystery"}, {Status: "mystery", Error: &VideoError{Code: "oops"}}} {
		out := ConvertVideoStatus(&obj)
		if out.State != llms.JobFailed || out.Err == nil || !strings.Contains(out.Err.Error(), "mystery") {
			t.Fatal(out)
		}
	}
	out := ConvertVideoStatus(&VideoObject{Status: "failed", Error: &VideoError{Code: "moderation_failed"}})
	var moderation *llms.ModerationError
	if !errors.As(out.Err, &moderation) || moderation.Provider != "openai" || out.Cost != nil {
		t.Fatal(out)
	}
	out = ConvertVideoStatus(&VideoObject{Status: "completed", Model: "sora-2", Seconds: "4", Size: "1280x720"})
	if out.Cost == nil || *out.Cost != .4 {
		t.Fatal(out)
	}
	for _, obj := range []VideoObject{{Status: "completed", Model: "unknown", Seconds: "4", Size: "1280x720"}, {Status: "completed", Model: "sora-2", Seconds: "0", Size: "1280x720"}, {Status: "completed", Model: "sora-2", Seconds: "4", Size: "1024x1792"}} {
		if out := ConvertVideoStatus(&obj); out.Cost != nil {
			t.Fatal(out)
		}
	}
}
