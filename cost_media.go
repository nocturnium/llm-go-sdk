package llms

// MediaRate is a price in USD per media unit.
type MediaRate struct {
	// Unit is the billing unit.
	Unit MediaUnit
	// USD is the price per unit in US dollars.
	USD float64
}

// MediaPricing holds rates keyed by "provider:model". Providers fill this in D1+.
// Configure the table before concurrent use; concurrent mutation is unsupported.
var MediaPricing = map[string]MediaRate{}

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
