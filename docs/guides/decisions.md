# Decision Models

A decision model reads a **state** (text, JSON, or images) and answers a set of
**typed questions** with calibrated probabilities. It does not generate text.
Use one where you would otherwise ask a chat model to pick a label and parse its
reply: routing, triage, classification, extraction.

## The `Decider` interface

```go
type Decider interface {
    Decide(ctx context.Context, req DecisionRequest, opts ...DecisionOption) (*DecisionResponse, error)
}
```

`Decider` is optional and separate from `llms.LLM`. Check for it at runtime:

```go
decider, ok := llms.AsDecider(client)
if !ok {
    return errors.New("this provider has no decision models")
}
```

`llms.HasDecision(client)` reads the `Capabilities.Decision` flag instead.

## Question kinds

| Kind | Question fields | Answer fields |
|------|-----------------|---------------|
| `DecisionKindNoul` | `Instructions`, optional `TrueGuidance`/`FalseGuidance` | `Probability` of "yes" |
| `DecisionKindChoice` | `Choices` (ordered `Label` + `Guidance`) | `Choice`, `Probabilities`, `Confidence` |
| `DecisionKindScore` | `Levels`, ordered lowest first | `Score` (fractional level index), `Legend`, `Probabilities`, `Confidence` |
| `DecisionKindText` | `Instructions` | `Value`, `Span` (rune offsets into the state), `Guess`, `PNull` |
| `DecisionKindDate` | `Instructions` | `Value` (ISO date), `Guess`, `PNull` |
| `DecisionKindNumber` | `Decimals`, `Unit` | `Value`, `Guess`, `PNull` |
| `DecisionKindList` | `Items` (text, number or date) | `Value` (a list), `Confidences` |
| `DecisionKindEstimate` | `Min`, `Max`, `Step` | `Value` and `Estimate` (median, p10, p90, p_below, p_above) |
| `DecisionKindBox` | `Instructions` naming the target | `Box` edges. No provider accepts box questions yet; both return `ErrInvalidParameters` |

`Choices` is a slice because option order is visible to the model. A Go map
would sort the options.

When a model cannot determine a value, the answer sets `Null`. A null answer
can still carry `Guess`, `Span` and `PNull`. A refusal sets `Refused` and `Null`.
`Raw` always holds the provider's answer object.

## Example

```go
resp, err := decider.Decide(ctx, llms.DecisionRequest{
    State: "Help! My payouts have been failing for 3 days.",
    Questions: map[string]llms.DecisionQuestion{
        "urgent": {Kind: llms.DecisionKindNoul, Instructions: "Does this convey urgency?"},
        "team": {Kind: llms.DecisionKindChoice, Instructions: "Which team should handle this?",
            Choices: []llms.DecisionChoice{
                {Label: "billing", Guidance: "Payments, invoicing, refunds"},
                {Label: "technical", Guidance: "Bugs, outages, integrations"},
            }},
        "frustration": {Kind: llms.DecisionKindScore, Instructions: "How frustrated is the customer?",
            Levels: []string{"calm", "frustrated", "furious"}},
    },
})
if err != nil {
    return err
}
if p := resp.Answers["urgent"].Probability; p != nil { // nil on a null or refused answer
    fmt.Println(*p, resp.Answers["team"].Choice)
}
```

## Providers

| Provider | Models | Kinds | Notes |
|----------|--------|-------|-------|
| openrouter | `typesafe/jev-1.14` (default) and every model listed with `ModelTypeDecision` | noul, choice, score | Endpoint `/api/alpha/decisions`. `Usage.Cost` is OpenRouter's reported charge. Assets and box questions are refused until the asset reference syntax is confirmed. |
| cloudflare | `@cf/cloudflare/clef-flash` (default), `clef`, `clef-omni` | noul, choice, score | Box questions are refused. Every question needs `Instructions`. Up to 64 questions with IDs of letters, digits, `_`, `.`, `-`. Up to four embedded PNG, JPEG or WebP images in `Assets`. |
| cloudflare | `@cf/cloudflare/clairic` | text, date, number, list, estimate | String state only. `ReferenceDate` resolves dates without a year. `Passes` and `Breakdown` report billing passes and token subtotals. |

Pick the model per call with `llms.WithDecisionModel`, or per client with the
provider's `WithDecisionModel` option. A Cloudflare model outside the table
returns `ErrDecisionNotSupported` rather than a guessed request.

List decision models with the normal model listing:

```go
models, err := lister.ListModels(ctx, llms.WithModelTypes(llms.ModelTypeDecision))
```

## Errors

| Condition | Error |
|-----------|-------|
| A kind, field or asset the model cannot serve | `ErrInvalidParameters`, before any request |
| Insufficient credits or balance (402, Cloudflare code 2021) | `ErrQuotaExceeded`, not retryable |
| Unknown model | `ErrModelNotFound` |
| Payload too large (413) | `ErrContextLengthExceeded` |
| Rate limit, edge timeout (524), overload | `ErrRateLimited`, `ErrTimeout`, `ErrServiceUnavailable`, retryable |

## Privacy

The state is sent to the provider verbatim. The SDK never logs it, traces it or
places it in its own error text, including in `LLM_DEBUG_REQUESTS` debug mode,
which redacts decision request and error bodies. A provider's error message is
passed through as written. `Raw` and a `state_text` metadata entry
can echo the state back; treat them as sensitive too.

## Cost

Decision models bill input tokens only. OpenRouter reports the charge in
`Usage.Cost`. For Cloudflare, estimate it from the response:

```go
cost, ok := llms.EstimateCost(llms.ProviderCloudflare, resp.Model, resp.Usage)
if !ok {
    // No rate card for this model; treat the call as unpriced rather than free.
}
```

Rate cards exist for Clef, Clef-flash, Clef-omni and Clairic (from Cloudflare's
pricing page, 2026-11-14).

Clairic bills the state and any images once per pass. Its reported input tokens
already include every pass, so the estimate needs no adjustment.
