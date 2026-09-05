package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"sort"
	"strings"
)

// MultipartFile describes one in-memory upload.
type MultipartFile struct {
	// Field is the form field name.
	Field string
	// Filename is the uploaded filename.
	Filename string
	// ContentType is the file MIME type; empty defaults to application/octet-stream.
	ContentType string
	// Data contains the file bytes.
	Data []byte
}

// DoMultipart sends fields and files to the absolute URL path and decodes JSON into out.
// A nil out skips decoding. It shares DoJSON's retry and error handling policy;
// the encoded body is retained so every retry sends the complete form.
// Fields are sorted by name. Headers override defaults, including Content-Type.
func (c *Client) DoMultipart(ctx context.Context, method, path string, fields map[string]string, files []MultipartFile, headers map[string]string, out any) error {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		value := fields[name]
		if err := writer.WriteField(name, value); err != nil {
			return fmt.Errorf("write multipart field: %w", err)
		}
	}
	for _, file := range files {
		if err := writeMultipartFile(writer, file); err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("close multipart body: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, method, path, bytes.NewReader(body.Bytes()))
	if err != nil {
		return fmt.Errorf("create multipart request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := c.doWithRetry(ctx, req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return c.handleErrorResponse(resp)
	}
	if out != nil {
		if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseSize)).Decode(out); err != nil {
			return fmt.Errorf("decode multipart response: %w", err)
		}
	}
	return nil
}

func writeMultipartFile(writer *multipart.Writer, file MultipartFile) error {
	contentType := file.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if strings.ContainsAny(contentType, "\r\n") {
		return fmt.Errorf("invalid multipart content type")
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{"name": file.Field, "filename": file.Filename}))
	header.Set("Content-Type", contentType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return fmt.Errorf("create multipart file: %w", err)
	}
	if _, err := part.Write(file.Data); err != nil {
		return fmt.Errorf("write multipart file: %w", err)
	}
	return nil
}
