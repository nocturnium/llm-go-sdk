package mcp

import (
	"context"
	"fmt"
)

// ListResources returns every resource the server advertises, following
// pagination. It mirrors [Client.ListTools]: each page is requested with the
// previous page's cursor and the loop checks ctx before each request so a
// canceled context stops paging promptly.
//
// Resources are an optional capability; check ServerCapabilities().Resources for
// nil before calling if you want to avoid a CodeMethodNotFound RPCError from a
// server that does not implement them.
func (c *Client) ListResources(ctx context.Context) ([]Resource, error) {
	var all []Resource
	cursor := ""
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var res listResourcesResult
		if err := c.call(ctx, methodResourcesList, listResourcesParams{Cursor: cursor}, &res); err != nil {
			return nil, fmt.Errorf("mcp: list resources: %w", err)
		}
		all = append(all, res.Resources...)
		if res.NextCursor == "" || res.NextCursor == cursor {
			// A server that returns the same non-empty cursor forever would loop
			// until ctx cancellation; stop when the cursor fails to advance.
			break
		}
		cursor = res.NextCursor
	}
	return all, nil
}

// ReadResource fetches the contents of a single resource by URI. The returned
// result may carry multiple content items (text and/or base64 blob).
func (c *Client) ReadResource(ctx context.Context, uri string) (*ReadResourceResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var res ReadResourceResult
	if err := c.call(ctx, methodResourcesRead, readResourceParams{URI: uri}, &res); err != nil {
		return nil, fmt.Errorf("mcp: read resource %q: %w", uri, err)
	}
	return &res, nil
}
