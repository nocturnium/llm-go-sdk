package observability

// float64Ptr returns a pointer to the given float64, for building CallOptions
// whose sampling parameters (Temperature/TopP) are *float64 so an explicit 0.0
// is distinguishable from "unset".
func float64Ptr(v float64) *float64 { return &v }

// intPtr returns a pointer to the given int, for building CallOptions whose
// integer parameters (MaxTokens) are *int so an explicit 0 is distinguishable
// from "unset".
func intPtr(v int) *int { return &v }
