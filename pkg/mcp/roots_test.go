package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// TestRootsStaticSetIsServed pins the common case.
func TestRootsStaticSetIsServed(t *testing.T) {
	m := newMockTransport()
	c := mustClient(t, m, WithRoots(
		Root{URI: "file:///workspace", Name: "workspace"},
		Root{URI: "file:///tmp/data"},
	))

	if c.ClientCapabilities().Roots == nil {
		t.Fatal("roots configured but not advertised")
	}
	// A static set cannot change, so listChanged must not be claimed.
	if c.ClientCapabilities().Roots.ListChanged {
		t.Error("a static root set must not advertise listChanged")
	}

	m.emitRequest(requestFrame("1", methodRootsList, nil))
	resp := decodeResponse(t, awaitResponses(t, m, 1)[0])
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	var result rootsListResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result.Roots) != 2 || result.Roots[0].Name != "workspace" {
		t.Errorf("roots = %+v, want the configured set", result.Roots)
	}
}

// TestRootsInvalidURIFailsConstruction pins that a bad static root is a wiring
// bug surfaced immediately, not silently normalized. A root that does not mean
// what the caller wrote is worse than no root.
func TestRootsInvalidURIFailsConstruction(t *testing.T) {
	cases := []struct {
		name string
		uri  string
	}{
		{"empty", ""},
		// Two slashes make the first segment an authority, so this silently means
		// something other than a local path.
		{"two slashes makes an authority", "file://relative/path"},
		// A remote host is not a location this client has.
		{"remote authority", "file://example.com/workspace"},
		{"wrong scheme", "https://example.com/x"},
		{"no scheme", "/workspace"},
		{"parent traversal", "file:///workspace/../etc"},
		{"dot segment", "file:///workspace/./sub"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newMockTransport()
			_, err := newClient(context.Background(), m, buildConfig([]Option{
				WithRoots(Root{URI: c.uri}),
			}))
			if err == nil {
				t.Fatalf("expected construction to fail for root URI %q", c.uri)
			}
			if !strings.Contains(err.Error(), "root URI") {
				t.Errorf("error = %v, want it to name the offending root URI", err)
			}
		})
	}
}

// TestRootsValidURIsAccepted guards against the validator being so strict it
// rejects legitimate paths.
func TestRootsValidURIsAccepted(t *testing.T) {
	for _, uri := range []string{
		"file:///",
		"file:///workspace",
		"file:///workspace/nested/deep",
		"file:///workspace/with-dash_and.dot",
	} {
		if err := validateRootURI(uri); err != nil {
			t.Errorf("validateRootURI(%q) = %v, want nil", uri, err)
		}
	}
}

// TestRootsDynamicHandlerAdvertisesListChanged pins that only a dynamic set
// claims listChanged, since only it can change.
func TestRootsDynamicHandlerAdvertisesListChanged(t *testing.T) {
	m := newMockTransport()
	c := mustClient(t, m, WithRootsHandler(func(context.Context) ([]Root, error) {
		return []Root{{URI: "file:///dynamic"}}, nil
	}))

	caps := c.ClientCapabilities().Roots
	if caps == nil {
		t.Fatal("roots handler registered but not advertised")
	}
	if !caps.ListChanged {
		t.Error("a dynamic root handler must advertise listChanged")
	}
}

// TestRootsDynamicHandlerIsConsultedPerRequest pins that the handler runs on
// each request rather than being snapshotted at construction.
func TestRootsDynamicHandlerIsConsultedPerRequest(t *testing.T) {
	m := newMockTransport()
	calls := 0
	_ = mustClient(t, m, WithRootsHandler(func(context.Context) ([]Root, error) {
		calls++
		return []Root{{URI: "file:///run", Name: "run"}}, nil
	}))

	m.emitRequest(requestFrame("1", methodRootsList, nil))
	awaitResponses(t, m, 1)
	m.emitRequest(requestFrame("2", methodRootsList, nil))
	awaitResponses(t, m, 2)

	if calls != 2 {
		t.Errorf("handler called %d times, want 2 (consulted per request)", calls)
	}
}

// TestRootsDynamicHandlerErrorSurfacesToServer pins that a failure is reported
// rather than answered with an empty list — a server must be able to tell "no
// roots" from "could not determine roots".
func TestRootsDynamicHandlerErrorSurfacesToServer(t *testing.T) {
	m := newMockTransport()
	_ = mustClient(t, m, WithRootsHandler(func(context.Context) ([]Root, error) {
		return nil, errors.New("disk enumeration failed")
	}))

	m.emitRequest(requestFrame("1", methodRootsList, nil))
	resp := decodeResponse(t, awaitResponses(t, m, 1)[0])
	if resp.Error == nil {
		t.Fatal("a handler error must surface as an error, not an empty list")
	}
}

// TestRootsDynamicHandlerInvalidURIIsNotPublished pins that a handler returning
// a malformed root is reported rather than sent. The static path validates at
// construction; the dynamic path cannot, so it validates per response.
func TestRootsDynamicHandlerInvalidURIIsNotPublished(t *testing.T) {
	m := newMockTransport()
	_ = mustClient(t, m, WithRootsHandler(func(context.Context) ([]Root, error) {
		return []Root{{URI: "file:///ok"}, {URI: "../escape"}}, nil
	}))

	m.emitRequest(requestFrame("1", methodRootsList, nil))
	resp := decodeResponse(t, awaitResponses(t, m, 1)[0])
	if resp.Error == nil {
		t.Fatal("a malformed root must not be published to the server")
	}
	if !strings.Contains(resp.Error.Message, "root URI") {
		t.Errorf("message = %q, want it to name the offending root", resp.Error.Message)
	}
}

// TestRootsNotAdvertisedWhenUnconfigured pins the default: a client that exposes
// no roots says so, and such requests are answered MethodNotFound.
func TestRootsNotAdvertisedWhenUnconfigured(t *testing.T) {
	m := newMockTransport()
	c := mustClient(t, m)
	if c.ClientCapabilities().Roots != nil {
		t.Error("roots advertised without being configured")
	}

	m.emitRequest(requestFrame("1", methodRootsList, nil))
	resp := decodeResponse(t, awaitResponses(t, m, 1)[0])
	if resp.Error == nil || resp.Error.Code != CodeMethodNotFound {
		t.Errorf("response = %+v, want MethodNotFound", resp.Error)
	}
}

// TestRootsChangedNotifiesOnlyForDynamicRoots pins that the notification is sent
// only when the server was told the set can change. Telling a server to re-read
// a set that cannot change is noise it has no reason to act on.
func TestRootsChangedNotifiesOnlyForDynamicRoots(t *testing.T) {
	t.Run("dynamic notifies", func(t *testing.T) {
		m := newMockTransport()
		c := mustClient(t, m, WithRootsHandler(func(context.Context) ([]Root, error) { return nil, nil }))

		before := len(m.notifications)
		if err := c.RootsChanged(context.Background()); err != nil {
			t.Fatalf("RootsChanged: %v", err)
		}
		if len(m.notifications) != before+1 ||
			m.notifications[len(m.notifications)-1] != methodNotificationsRootsChanged {
			t.Errorf("notifications = %v, want a roots/list_changed", m.notifications)
		}
	})

	t.Run("static is a no-op", func(t *testing.T) {
		m := newMockTransport()
		c := mustClient(t, m, WithRoots(Root{URI: "file:///static"}))

		before := len(m.notifications)
		if err := c.RootsChanged(context.Background()); err != nil {
			t.Fatalf("RootsChanged: %v", err)
		}
		if len(m.notifications) != before {
			t.Error("a static root set must not emit list_changed")
		}
	})

	t.Run("unconfigured is a no-op", func(t *testing.T) {
		m := newMockTransport()
		c := mustClient(t, m)
		if err := c.RootsChanged(context.Background()); err != nil {
			t.Fatalf("RootsChanged: %v", err)
		}
	})
}

// TestRootsNotAdvertisedOnTransportsThatCannotServeIt mirrors the sampling case:
// a capability whose request can never arrive must not be advertised.
func TestRootsNotAdvertisedOnTransportsThatCannotServeIt(t *testing.T) {
	m := newMockTransport()
	m.inboundUnsupported = true

	c := mustClient(t, m, WithRoots(Root{URI: "file:///workspace"}))
	if c.ClientCapabilities().Roots != nil {
		t.Error("roots advertised on a transport that cannot deliver the request")
	}
}
