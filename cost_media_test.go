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
