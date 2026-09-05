package llms

import (
	"sync"
	"testing"
)

var _ = map[ModelUsage]struct{}{}

func TestMediaCost(t *testing.T) {
	MediaPricing["test:media"] = MediaRate{Unit: MediaUnitImage, USD: 0.5}
	defer delete(MediaPricing, "test:media")
	reported := 3.0
	for _, tc := range []struct {
		name, model string
		usage       MediaUsage
		cost        float64
		ok          bool
	}{
		{"reported", "media", MediaUsage{Unit: MediaUnitSecond, Quantity: 10, Cost: &reported}, 3, true},
		{"reported unknown model", "missing", MediaUsage{Cost: &reported}, 3, true},
		{"estimated", "media", MediaUsage{Unit: MediaUnitImage, Quantity: 4}, 2, true},
		{"mismatch", "media", MediaUsage{Unit: MediaUnitSecond, Quantity: 4}, 0, false},
		{"unknown", "missing", MediaUsage{Unit: MediaUnitImage, Quantity: 4}, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cost, ok := MediaCost("test", tc.model, tc.usage)
			if cost != tc.cost || ok != tc.ok {
				t.Fatalf("got %v, %v", cost, ok)
			}
		})
	}
}

func TestCostTracker_RecordMedia_Concurrent(t *testing.T) {
	t.Parallel()
	tracker := NewCostTracker(map[string]Pricing{"test:tokens": {Input: 1}})
	tracker.Record(Provider("test"), "tokens", Usage{PromptTokens: 1000000})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cost := 0.5
			for j := 0; j < 10; j++ {
				tracker.RecordMedia("test", "media", MediaUsage{Unit: MediaUnitImage, Quantity: 2, Cost: &cost})
				tracker.RecordMedia("test", "media", MediaUsage{Unit: MediaUnitSecond, Quantity: 3})
				_ = tracker.MediaTotals()
				_ = tracker.GetTotalCost()
			}
		}()
	}
	wg.Wait()
	totals := tracker.MediaTotals()
	if got := totals["test:media:image"]; got != (MediaTotal{Unit: MediaUnitImage, Quantity: 400, Cost: 100, Requests: 200}) {
		t.Fatalf("image totals: %+v", got)
	}
	if got := totals["test:media:second"]; got != (MediaTotal{Unit: MediaUnitSecond, Quantity: 600, Requests: 200, Unpriced: 200}) {
		t.Fatalf("second totals: %+v", got)
	}
	delete(totals, "test:media:image")
	if len(tracker.MediaTotals()) != 2 || tracker.GetTotalCost() != 101 {
		t.Fatal("incorrect total or aliased snapshot")
	}
	tracker.Reset()
	if tracker.GetTotalCost() != 0 || len(tracker.MediaTotals()) != 0 {
		t.Fatal("reset retained media")
	}
}

func TestMediaPricing_GeminiReconciliation(t *testing.T) {
	expected := map[string]MediaRate{
		"gemini-2.5-flash-image":        {Unit: MediaUnitImage, USD: 0.039},
		"gemini-3.1-flash-image":        {Unit: MediaUnitImage, USD: 0.067},
		"gemini-3.1-flash-lite-image":   {Unit: MediaUnitImage, USD: 0.0336},
		"gemini-3-pro-image":            {Unit: MediaUnitImage, USD: 0.134},
		"veo-3.1-generate-preview":      {Unit: MediaUnitSecond, USD: 0.4},
		"veo-3.1-fast-generate-preview": {Unit: MediaUnitSecond, USD: 0.1},
		"veo-3.1-lite-generate-preview": {Unit: MediaUnitSecond, USD: 0.05},
		"gemini-3.1-flash-tts-preview":  {Unit: MediaUnitMTokenOut, USD: 20},
		"gemini-2.5-flash-preview-tts":  {Unit: MediaUnitMTokenOut, USD: 10},
		"gemini-2.5-pro-preview-tts":    {Unit: MediaUnitMTokenOut, USD: 20},
	}
	for model, want := range expected {
		t.Run(model, func(t *testing.T) {
			got, ok := GetMediaRate("gemini", model)
			if !ok || got != want {
				t.Fatalf("rate = %+v, %t; want %+v", got, ok, want)
			}
			exact := 1.23
			cost, ok := MediaCost("gemini", model, MediaUsage{Unit: want.Unit, Quantity: 2, Cost: &exact})
			if !ok || cost != exact {
				t.Fatalf("exact cost did not override base: %v, %t", cost, ok)
			}
		})
	}
}

// Thin media models are an explicit reconciliation allowlist: these rates use
// media units, and need not appear in the token-priced chat capability registry.
func TestMediaPricing_ThinProviderReconciliation(t *testing.T) {
	expected := map[string]MediaRate{
		"togetherai:cartesia/sonic":                   {Unit: MediaUnitKChar, USD: 0.065},
		"togetherai:canopylabs/orpheus-3b-0.1-ft":     {Unit: MediaUnitKChar, USD: 0.015},
		"togetherai:hexgrad/Kokoro-82M":               {Unit: MediaUnitKChar, USD: 0.004},
		"togetherai:openai/whisper-large-v3":          {Unit: MediaUnitMinute, USD: 0.0015},
		"togetherai:black-forest-labs/FLUX.1-schnell": {Unit: MediaUnitMegapixel, USD: 0.0027},
		"togetherai:black-forest-labs/FLUX.2-dev":     {Unit: MediaUnitImage, USD: 0.0154},
		"togetherai:black-forest-labs/FLUX.2-pro":     {Unit: MediaUnitImage, USD: 0.03},
		"togetherai:google/imagen-4.0-fast":           {Unit: MediaUnitMegapixel, USD: 0.02},
		"togetherai:openai/gpt-image-1.5":             {Unit: MediaUnitImage, USD: 0.034},
		"togetherai:openai/sora-2":                    {Unit: "", USD: 0.8},
		"groq:canopylabs/orpheus-v1-english":          {Unit: MediaUnitKChar, USD: 0.022},
		"groq:canopylabs/orpheus-arabic-saudi":        {Unit: MediaUnitKChar, USD: 0.04},
		"groq:whisper-large-v3":                       {Unit: MediaUnitMinute, USD: 0.00185},
		"groq:whisper-large-v3-turbo":                 {Unit: MediaUnitMinute, USD: 0.000667},
		"mistral:voxtral-mini-latest":                 {Unit: MediaUnitMinute, USD: 0.003},
		"mistral:voxtral-mini-tts-2603":               {Unit: MediaUnitKChar, USD: 0.016},
		"featherless:hexgrad/Kokoro-82M":              {Unit: MediaUnitKChar, USD: 0.004},
		"featherless:canopylabs/orpheus-3b-0.1-ft":    {Unit: MediaUnitKChar, USD: 0.015},
		"featherless:ResembleAI/chatterbox":           {Unit: MediaUnitKChar, USD: 0.025},
		"zai:cogvideox-3":                             {Unit: "", USD: 0.20},
		"zai:glm-image":                               {Unit: MediaUnitImage, USD: 0.015},
		"zai:cogview-4-250304":                        {Unit: MediaUnitImage, USD: 0.01},
	}
	for key, want := range expected {
		if got, ok := MediaPricing[key]; !ok || got != want {
			t.Errorf("%s: got %+v, %t; want %+v", key, got, ok, want)
		}
	}
	if _, ok := MediaCost("togetherai", "black-forest-labs/FLUX.1-schnell", MediaUsage{}); ok {
		t.Error("unknown size was priced")
	}
}

func TestMediaPricing_RemovedUnverifiedVideoRows(t *testing.T) {
	for _, model := range []string{"google/veo-3.1", "Wan-AI/wan2.7"} {
		if _, ok := GetMediaRate("togetherai", model); ok {
			t.Errorf("unverified video row retained: %s", model)
		}
	}
}
