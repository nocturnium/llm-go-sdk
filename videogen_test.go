package llms

import (
	"context"
	"errors"
	"fmt"

	"reflect"
	"testing"
	"time"

	"github.com/nocturnium/llm-go-sdk/v6/internal/httpclient"
)

func testVideoAudioPointer() *bool { value := true; return &value }

func testVideoSeedPointer() *int64 { value := int64(7); return &value }

func testVideoFirstFramePointer() *MediaInput { value := MediaInput{FileID: "frame"}; return &value }

func testVideoLastFramePointer() *MediaInput { value := MediaInput{FileID: "frame"}; return &value }

func TestApplyVideoOptions(t *testing.T) {
	if got := ApplyVideoOptions(); !reflect.DeepEqual(got, &VideoOptions{}) {
		t.Fatalf("defaults: %+v", got)
	}
	got := ApplyVideoOptions(WithVideoModel("Model"), WithVideoDuration(3), WithVideoResolution("Resolution"), WithVideoAspectRatio("AspectRatio"), WithVideoAudio(true), WithVideoSeed(int64(7)), WithVideoNegativePrompt("NegativePrompt"), WithVideoFirstFrame(MediaInput{FileID: "frame"}), WithVideoLastFrame(MediaInput{FileID: "frame"}), WithVideoReferenceImages([]MediaInput{{FileID: "ref"}}), WithVideoFormat("OutputFormat"), WithVideoExtra(map[string]any{"custom": true}))
	want := &VideoOptions{Model: "Model", DurationSeconds: 3, Resolution: "Resolution", AspectRatio: "AspectRatio", Audio: testVideoAudioPointer(), Seed: testVideoSeedPointer(), NegativePrompt: "NegativePrompt", FirstFrame: testVideoFirstFramePointer(), LastFrame: testVideoLastFramePointer(), ReferenceImages: []MediaInput{{FileID: "ref"}}, OutputFormat: "OutputFormat", Extra: map[string]any{"custom": true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

type videoCapabilityStub struct{ VideoGenerator }

func TestVideoCapabilities(t *testing.T) {
	for _, value := range []any{nil, 42, videoCapabilityStub{}} {
		_, want := value.(videoCapabilityStub)
		if SupportsVideoGeneration(value) != want {
			t.Fatal("incorrect capability assertion")
		}
		g, ok := AsVideoGenerator(value)
		if ok != want || (g == nil) == want {
			t.Fatal("incorrect video cast")
		}
	}
}

func TestPollingVideoJob_Wait(t *testing.T) {
	cause := errors.New("provider failed")
	for _, tc := range []struct {
		state JobState
		want  error
	}{
		{JobSucceeded, nil}, {JobFailed, ErrJobFailed}, {JobModerated, ErrContentFiltered}, {JobCancelled, context.Canceled},
	} {
		t.Run(string(tc.state), func(t *testing.T) {
			polls, results := 0, 0
			job := &PollingVideoJob{JobID: "job", Policy: httpclient.PollPolicy{Initial: time.Microsecond},
				PollFn: func(context.Context) (*JobStatus, error) {
					polls++
					if polls == 1 {
						return &JobStatus{State: JobRunning}, nil
					}
					return &JobStatus{State: tc.state, Err: cause}, nil
				},
				ResultFn: func(context.Context) (*VideoResponse, error) { results++; return &VideoResponse{Model: "video"}, nil },
			}
			response, err := job.Wait(context.Background())
			if !errors.Is(err, tc.want) {
				t.Fatalf("Wait error %v, want %v", err, tc.want)
			}
			if job.ID() != "job" || polls != 2 {
				t.Fatalf("id=%s polls=%d", job.ID(), polls)
			}
			if tc.state == JobSucceeded {
				if response.Model != "video" || results != 1 {
					t.Fatal("missing result")
				}
			} else if results != 0 || response != nil {
				t.Fatal("fetched failed result")
			}
			if tc.state == JobFailed && !errors.Is(err, cause) {
				t.Fatal("lost failure cause")
			}
			if tc.state == JobModerated {
				var moderation *ModerationError
				if !errors.As(err, &moderation) || moderation.Stage != ModerationOutput || len(moderation.Reasons) != 1 {
					t.Fatalf("bad moderation: %v", err)
				}
			}
		})
	}
}

func TestPollingVideoJob_Errors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	job := &PollingVideoJob{}
	if _, err := job.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := job.Poll(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := job.Cancel(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := job.Cancel(context.Background()); !errors.Is(err, ErrJobCancelNotSupported) {
		t.Fatal(err)
	}
	if _, err := job.Wait(context.Background()); !errors.Is(err, ErrInvalidParameters) {
		t.Fatal(err)
	}
	job.PollFn = func(context.Context) (*JobStatus, error) { return nil, nil }
	if _, err := job.Wait(context.Background()); !errors.Is(err, ErrInvalidParameters) {
		t.Fatal(err)
	}
	job.PollFn = func(context.Context) (*JobStatus, error) { return &JobStatus{State: JobSucceeded}, nil }
	if _, err := job.Wait(context.Background()); !errors.Is(err, ErrInvalidParameters) {
		t.Fatal(err)
	}
	cause := errors.New("callback error")
	job.ResultFn = func(context.Context) (*VideoResponse, error) { return nil, cause }
	if _, err := job.Wait(context.Background()); !errors.Is(err, cause) {
		t.Fatal(err)
	}
	job.CancelFn = func(context.Context) error { return cause }
	if err := job.Cancel(context.Background()); !errors.Is(err, cause) {
		t.Fatal(err)
	}
	job.PollFn = func(context.Context) (*JobStatus, error) { return nil, cause }
	if _, err := job.Wait(context.Background()); !errors.Is(err, cause) {
		t.Fatal(err)
	}
	moderation := &ModerationError{Stage: ModerationInput, Reasons: []string{"blocked"}, Charged: true, Provider: "test"}
	job.PollFn = func(context.Context) (*JobStatus, error) {
		return &JobStatus{State: JobModerated, Err: fmt.Errorf("wrapped: %w", moderation)}, nil
	}
	var gotModeration *ModerationError
	if _, err := job.Wait(context.Background()); !errors.As(err, &gotModeration) || gotModeration != moderation {
		t.Fatalf("lost moderation details: %v", err)
	}
	job.PollFn = func(context.Context) (*JobStatus, error) { return &JobStatus{State: JobModerated}, nil }
	if _, err := job.Wait(context.Background()); !errors.Is(err, ErrContentFiltered) {
		t.Fatal(err)
	}
}

func TestPollingVideoJob_WaitCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	job := &PollingVideoJob{PollFn: func(context.Context) (*JobStatus, error) { cancel(); return &JobStatus{State: JobRunning}, nil }}
	if _, err := job.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
