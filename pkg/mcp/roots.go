package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

// Root is a filesystem location the host makes known to a server, so the server
// can scope its work to directories the host actually intends it to touch.
//
// A root is advisory: it tells the server where to look. This SDK serves no file
// reads, so publishing a root grants a server no access it did not already have —
// what it does grant is knowledge of a path, which is why registration validates
// the URI rather than normalizing it silently.
type Root struct {
	// URI must be an absolute file:// URI.
	URI string `json:"uri"`
	// Name is an optional human-readable label.
	Name string `json:"name,omitempty"`
}

// RootsHandler returns the roots to expose. Use it when the set changes over the
// client's lifetime; for a fixed set use [WithRoots].
//
// Returning an error surfaces to the server as a JSON-RPC error rather than an
// empty list, so a server can tell "no roots" from "could not determine roots".
type RootsHandler func(ctx context.Context) ([]Root, error)

// rootsListResult is the wire shape of a roots/list response.
type rootsListResult struct {
	Roots []Root `json:"roots"`
}

// WithRoots exposes a fixed set of roots to the server.
//
// Each URI is validated at construction: it must be an absolute `file://` URI
// with no traversal segments. An invalid root fails client construction rather
// than being silently normalized — a root that does not mean what the caller
// wrote is worse than no root at all.
//
// Transport boundary: server-initiated requests are delivered over stdio only,
// so roots registered on an HTTP client are never requested and the capability
// is not advertised.
func WithRoots(roots ...Root) Option {
	return func(c *config) {
		c.roots = append(c.roots, roots...)
	}
}

// WithRootsHandler exposes a dynamic set of roots, consulted per request.
//
// Unlike [WithRoots] the URIs cannot be validated up front, so they are
// validated on each response; an invalid root is reported to the server as an
// error rather than sent. Registering a dynamic handler also advertises the
// listChanged capability, since the set can change — call [Client.RootsChanged]
// to tell the server it has.
func WithRootsHandler(h RootsHandler) Option {
	return func(c *config) {
		c.rootsHandler = h
		c.rootsListChanged = true
	}
}

// buildRootsHandler resolves the configured roots into a request handler,
// returning (nil, nil) when roots are not configured.
func buildRootsHandler(cfg config) (requestHandler, error) {
	if cfg.rootsHandler != nil {
		handler := cfg.rootsHandler
		return func(ctx context.Context, _ json.RawMessage) (any, error) {
			roots, err := handler(ctx)
			if err != nil {
				return nil, err
			}
			if err := validateRoots(roots); err != nil {
				// The handler produced something unusable; tell the server rather
				// than publishing a malformed or ambiguous path.
				return nil, &RPCError{Code: CodeInternalError, Message: err.Error()}
			}
			return rootsListResult{Roots: roots}, nil
		}, nil
	}

	if len(cfg.roots) == 0 {
		return nil, nil
	}
	// Validate once, at construction: a static set cannot change, so a bad URI is
	// a wiring bug the caller should learn about immediately.
	if err := validateRoots(cfg.roots); err != nil {
		return nil, err
	}
	roots := append([]Root(nil), cfg.roots...)
	return func(context.Context, json.RawMessage) (any, error) {
		return rootsListResult{Roots: roots}, nil
	}, nil
}

// validateRoots checks every root's URI.
func validateRoots(roots []Root) error {
	for _, r := range roots {
		if err := validateRootURI(r.URI); err != nil {
			return err
		}
	}
	return nil
}

// validateRootURI requires an absolute file:// URI with no traversal segments.
//
// This is advertisement hygiene, not sandboxing: the SDK serves no file reads,
// so a root cannot itself leak content. What it prevents is publishing a path
// that does not mean what the caller wrote — a relative or `..`-laden URI would
// be interpreted by the server against its own working directory, which is not
// the host's.
func validateRootURI(raw string) error {
	if raw == "" {
		return fmt.Errorf("mcp: root URI is empty")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("mcp: root URI %q is not a valid URI: %w", raw, err)
	}
	if u.Scheme != "file" {
		return fmt.Errorf("mcp: root URI %q must use the file:// scheme (got %q)", raw, u.Scheme)
	}
	// A file URI's authority must be empty or "localhost" (RFC 8089). Anything
	// else names a remote host — "file://example.com/x" is not a local path, and
	// publishing it as a root would tell the server about a location this host
	// does not have. It is also what a caller writing "file://relative/path"
	// accidentally produces: the first segment becomes the authority, so the URI
	// silently means something other than what they wrote.
	if u.Host != "" && u.Host != "localhost" {
		return fmt.Errorf("mcp: root URI %q must have no authority (got host %q); "+
			"a local path needs three slashes, e.g. file:///path", raw, u.Host)
	}
	if u.Path == "" {
		return fmt.Errorf("mcp: root URI %q has no path", raw)
	}
	if !strings.HasPrefix(u.Path, "/") {
		return fmt.Errorf("mcp: root URI %q must be absolute", raw)
	}
	// Reject traversal rather than cleaning it: silently rewriting a caller's
	// path would publish a location they did not write.
	if u.Path != filepath.ToSlash(filepath.Clean(u.Path)) {
		return fmt.Errorf("mcp: root URI %q must be a clean absolute path (no . or .. segments)", raw)
	}
	return nil
}

// RootsChanged notifies the server that the set of roots has changed, prompting
// it to call roots/list again.
//
// It is a no-op unless a dynamic handler was registered with
// [WithRootsHandler]: a server that was never told this client's roots can
// change has no reason to re-read them, and a static set cannot change.
func (c *Client) RootsChanged(ctx context.Context) error {
	if c.clientCaps.Roots == nil || !c.clientCaps.Roots.ListChanged {
		return nil
	}
	note, err := encodeNotification(methodNotificationsRootsChanged, nil)
	if err != nil {
		return err
	}
	return c.transport.notify(ctx, note)
}
