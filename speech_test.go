package llms

import (
	"reflect"
	"testing"
)

func testSpeechSpeedPointer() *float64 { value := 1.25; return &value }

func TestApplySpeechOptions(t *testing.T) {
	if got := ApplySpeechOptions(); !reflect.DeepEqual(got, &SpeechOptions{}) {
		t.Fatalf("defaults: %+v", got)
	}
	got := ApplySpeechOptions(WithSpeechModel("Model"), WithSpeechVoice("Voice"), WithSpeechLanguage("Language"), WithSpeechInstructions("Instructions"), WithSpeechSpeed(1.25), WithSpeechFormat(AudioFormat{Container: "wav", Encoding: "pcm", SampleRate: 24000, BitRate: 384000}), WithSpeechTimestamps(true), WithSpeechExtra(map[string]any{"custom": true}))
	want := &SpeechOptions{Model: "Model", Voice: "Voice", Language: "Language", Instructions: "Instructions", Speed: testSpeechSpeedPointer(), Format: AudioFormat{Container: "wav", Encoding: "pcm", SampleRate: 24000, BitRate: 384000}, Timestamps: true, Extra: map[string]any{"custom": true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestApplyTranscribeOptions(t *testing.T) {
	if got := ApplyTranscribeOptions(); !reflect.DeepEqual(got, &TranscribeOptions{}) {
		t.Fatalf("defaults: %+v", got)
	}
	got := ApplyTranscribeOptions(WithTranscribeModel("Model"), WithTranscribeLanguage("Language"), WithTranscribePrompt("Prompt"), WithTranscribeDiarization(true), WithTranscribeWordTimestamps(true), WithTranscribeKeyterms([]string{"term"}), WithTranscribeExtra(map[string]any{"custom": true}))
	want := &TranscribeOptions{Model: "Model", Language: "Language", Prompt: "Prompt", Diarize: true, WordTimestamps: true, Keyterms: []string{"term"}, Extra: map[string]any{"custom": true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

type speechCapabilityStub struct {
	SpeechSynthesizer
	Transcriber
}

func TestSpeechCapabilities(t *testing.T) {
	for _, value := range []any{nil, 42, speechCapabilityStub{}} {
		_, want := value.(speechCapabilityStub)
		if SupportsSpeech(value) != want || SupportsTranscription(value) != want {
			t.Fatal("incorrect capability assertion")
		}
		s, ok := AsSpeechSynthesizer(value)
		if ok != want || (s == nil) == want {
			t.Fatal("incorrect speech cast")
		}
		tr, ok := AsTranscriber(value)
		if ok != want || (tr == nil) == want {
			t.Fatal("incorrect transcription cast")
		}
	}
}
