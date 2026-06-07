// Package testutil provides test scaffolding shared across the llm-go-sdk test
// suite. It lives under internal/ so these helpers are available to tests within
// the module without being part of the public API surface of the root package.
//
// Testing conventions for llm-go-sdk:
//
// Unit tests: Regular *_test.go files with no build tags. Use mock servers
// (httptest.NewServer) for testing API interactions. These run on every build.
//
// Live integration tests: Files with build tag //go:build integration
// These tests make actual API calls and require valid API keys.
// Run with: go test -tags=integration ./...
package testutil

import "os"

// TestingT is an interface that matches testing.T and testing.B methods used
// for skipping tests. This allows the helper to work with both test types.
type TestingT interface {
	Skipf(format string, args ...any)
	Helper()
}

// SkipIfNoAPIKey skips the test if the specified environment variable is not set.
// Use this in live integration tests to gracefully skip when credentials are missing.
//
// Example usage:
//
//	//go:build integration
//
//	func TestLiveAPI(t *testing.T) {
//	    testutil.SkipIfNoAPIKey(t, "OPENAI_API_KEY")
//	    // ... test code that calls real API
//	}
func SkipIfNoAPIKey(t TestingT, envVar string) {
	t.Helper()
	if os.Getenv(envVar) == "" {
		t.Skipf("skipping live integration test: %s not set", envVar)
	}
}

// RequireEnvAPIKey gets an API key from the environment or skips the test.
// Returns the API key value if set.
//
// Example usage:
//
//	func TestLiveAPI(t *testing.T) {
//	    apiKey := testutil.RequireEnvAPIKey(t, "OPENAI_API_KEY")
//	    client, _ := openai.New(openai.WithAPIKey(apiKey))
//	    // ... test code
//	}
func RequireEnvAPIKey(t TestingT, envVar string) string {
	t.Helper()
	key := os.Getenv(envVar)
	if key == "" {
		t.Skipf("skipping live integration test: %s not set", envVar)
	}
	return key
}
