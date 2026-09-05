package httpclient

import (
	"context"
	"net/http"
)

// BinaryResponse contains a bounded binary response and a copy of its headers.
type BinaryResponse struct {
	// Data contains the response body.
	Data []byte
	// ContentType is the response's Content-Type header.
	ContentType string
	// Header contains response headers, including provider billing metadata.
	Header http.Header
}

// DoBinary sends an optional JSON body to the absolute URL path and returns bytes.
// It uses DoRawWithHeaders' SSRF validation, retries, errors, and 100 MB size cap.
func (c *Client) DoBinary(ctx context.Context, method, path string, body any, headers map[string]string) (*BinaryResponse, error) {
	data, header, err := c.DoRawWithHeaders(ctx, Request{Method: method, URL: path, Body: body, Headers: headers})
	if err != nil {
		return nil, err
	}
	return &BinaryResponse{Data: data, ContentType: header.Get("Content-Type"), Header: header}, nil
}
