package llms

// MediaRate is a price in USD per media unit.
type MediaRate struct {
	// Unit is the billing unit.
	Unit MediaUnit
	// USD is the price per unit in US dollars.
	USD float64
}

// MediaPricing holds rates keyed by "provider:model".
// Configure the table before concurrent use; concurrent mutation is unsupported.
// Sora rates are the 720p base; higher resolutions are unpriced.
// gpt-4o-transcribe and gpt-4o-mini-transcribe are token-billed and priced
// by the provider converter, including separate audio/text input rates.
// gpt-4o-mini-tts is priced only with StreamSpeech SSE done usage; Synthesize
// has no usage and remains unpriced (CreateSpeechStream exposes the usage).
// gpt-transcribe is intentionally absent because its price is unverified.
var MediaPricing = map[string]MediaRate{
	// Gemini native media, verified 2026-09-05. Image rows use 1K and Veo
	// rows use 720p with audio. Converters report exact size/resolution Cost;
	// TTS converters include input tokens as well as output tokens.
	"gemini:gemini-2.5-flash-image":        {Unit: MediaUnitImage, USD: 0.039},
	"gemini:gemini-3.1-flash-image":        {Unit: MediaUnitImage, USD: 0.067},
	"gemini:gemini-3.1-flash-lite-image":   {Unit: MediaUnitImage, USD: 0.0336},
	"gemini:gemini-3-pro-image":            {Unit: MediaUnitImage, USD: 0.134},
	"gemini:veo-3.1-generate-preview":      {Unit: MediaUnitSecond, USD: 0.4},
	"gemini:veo-3.1-fast-generate-preview": {Unit: MediaUnitSecond, USD: 0.1},
	"gemini:veo-3.1-lite-generate-preview": {Unit: MediaUnitSecond, USD: 0.05},
	"gemini:gemini-3.1-flash-tts-preview":  {Unit: MediaUnitMTokenOut, USD: 20},
	"gemini:gemini-2.5-flash-preview-tts":  {Unit: MediaUnitMTokenOut, USD: 10},
	"gemini:gemini-2.5-pro-preview-tts":    {Unit: MediaUnitMTokenOut, USD: 20},

	// ElevenLabs API pricing, https://elevenlabs.io/pricing/api (2026-09-05).
	"elevenlabs:eleven_v3":                {Unit: MediaUnitKChar, USD: 0.10},
	"elevenlabs:eleven_multilingual_v2":   {Unit: MediaUnitKChar, USD: 0.10},
	"elevenlabs:eleven_v3_conversational": {Unit: MediaUnitKChar, USD: 0.05},
	"elevenlabs:eleven_flash_v2_5":        {Unit: MediaUnitKChar, USD: 0.05},
	"elevenlabs:eleven_turbo_v2_5":        {Unit: MediaUnitKChar, USD: 0.05},
	"elevenlabs:eleven_turbo_v2":          {Unit: MediaUnitKChar, USD: 0.05},
	"elevenlabs:eleven_flash_v2":          {Unit: MediaUnitKChar, USD: 0.05},
	"elevenlabs:scribe_v2":                {Unit: MediaUnitMinute, USD: 0.0036667},
	"elevenlabs:eleven_text_to_sound_v2":  {Unit: MediaUnitMinute, USD: 0.12},
	"elevenlabs:music_v2":                 {Unit: MediaUnitMinute, USD: 0.15},
	"openai:gpt-image-2":                  {Unit: MediaUnitMTokenOut, USD: 30},
	"openai:gpt-image-1.5":                {Unit: MediaUnitMTokenOut, USD: 32},
	"openai:gpt-image-1":                  {Unit: MediaUnitMTokenOut, USD: 40},
	"openai:gpt-image-1-mini":             {Unit: MediaUnitMTokenOut, USD: 8},
	"openai:gpt-4o-mini-tts":              {Unit: MediaUnitMTokenOut, USD: 12},
	"openai:tts-1":                        {Unit: MediaUnitKChar, USD: 0.015},
	"openai:tts-1-hd":                     {Unit: MediaUnitKChar, USD: 0.030},
	"openai:whisper-1":                    {Unit: MediaUnitMinute, USD: 0.006},
	"openai:sora-2":                       {Unit: MediaUnitSecond, USD: 0.10},
	"openai:sora-2-pro":                   {Unit: MediaUnitSecond, USD: 0.30},
}

// GetMediaRate returns the rate for provider/model and whether it is known.
func GetMediaRate(provider, model string) (MediaRate, bool) {
	rate, ok := MediaPricing[provider+":"+model]
	return rate, ok
}

// MediaCost returns reported USD cost first, otherwise a matching unit's estimate.
// Missing pricing or a unit mismatch returns (0, false), never a known free price.
func MediaCost(provider, model string, u MediaUsage) (cost float64, ok bool) {
	if u.Cost != nil {
		return *u.Cost, true
	}
	rate, found := GetMediaRate(provider, model)
	if !found || rate.Unit != u.Unit {
		return 0, false
	}
	return u.Quantity * rate.USD, true
}

// MediaTotal accumulates media usage for a provider/model and unit.
type MediaTotal struct {
	// Unit is the accumulated quantity's billing unit.
	Unit MediaUnit
	// Quantity is the total number of units consumed.
	Quantity float64
	// Cost is known spend in USD; inspect Unpriced for missing estimates.
	Cost float64
	// Requests is the number of recorded requests.
	Requests int
	// Unpriced is the number of requests without a matching rate or reported cost.
	Unpriced int
}

// RecordMedia accumulates media usage independently of token ModelUsage.
// Totals are keyed by "provider:model:unit" so incompatible quantities never mix.
func (t *CostTracker) RecordMedia(provider, model string, u MediaUsage) {
	cost, priced := MediaCost(provider, model, u)
	key := provider + ":" + model + ":" + string(u.Unit)
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.media == nil {
		t.media = make(map[string]MediaTotal)
	}
	total := t.media[key]
	total.Unit = u.Unit
	total.Quantity += u.Quantity
	total.Cost += cost
	total.Requests++
	if !priced {
		total.Unpriced++
	}
	t.media[key] = total
}

// MediaTotals returns an independent snapshot keyed by "provider:model:unit".
func (t *CostTracker) MediaTotals() map[string]MediaTotal {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make(map[string]MediaTotal, len(t.media))
	for key, total := range t.media {
		out[key] = total
	}
	return out
}
