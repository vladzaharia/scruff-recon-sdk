package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
)

// Request describes an HTTP call abstractly, before it is turned into an
// *http.Request.
//
// The indirection exists because the two networks authenticate differently in a
// way that a materialised *http.Request cannot accommodate. Recon adds an
// Authorization header; SCRUFF injects identity parameters into the query string
// for GET and into the form body for POST. An Authenticator handed an
// *http.Request could do the first but not the second, because the body is
// already sealed by then. Handing it a Request lets both work the same way.
type Request struct {
	Method string
	// Path is appended to the client's base URL. It may already contain a
	// query string; Query is merged on top.
	Path  string
	Query url.Values

	// Header carries extra headers. Content-Type is set automatically from
	// whichever body field is populated.
	Header http.Header

	// Exactly one body field may be set.

	// JSON is marshalled as the request body.
	JSON any
	// Form is sent as application/x-www-form-urlencoded.
	Form url.Values
	// Multipart sends multipart/form-data. Field order is preserved, which
	// matters for SCRUFF: the app emits parts in a fixed order and matching it
	// keeps requests indistinguishable from the real client.
	Multipart []Part
	// Raw is sent verbatim, with RawContentType.
	Raw            []byte
	RawContentType string

	// MultipartContentType overrides the multipart subtype. SCRUFF's chat send
	// uses multipart/mixed rather than the multipart/form-data default.
	MultipartContentType string
}

// Part is one part of a multipart body. A Part with a Filename is sent as a
// file part; otherwise it is a plain field.
type Part struct {
	Name     string
	Value    string
	Filename string
	Data     []byte
	MimeType string
}

// SetQuery sets a query parameter, allocating Query if needed.
func (r *Request) SetQuery(key, value string) {
	if r.Query == nil {
		r.Query = url.Values{}
	}
	r.Query.Set(key, value)
}

// SetForm sets a form field, allocating Form if needed.
func (r *Request) SetForm(key, value string) {
	if r.Form == nil {
		r.Form = url.Values{}
	}
	r.Form.Set(key, value)
}

// AddPart appends a plain multipart field.
func (r *Request) AddPart(name, value string) {
	r.Multipart = append(r.Multipart, Part{Name: name, Value: value})
}

// AddFilePart appends a multipart file part.
func (r *Request) AddFilePart(name, filename, mimeType string, data []byte) {
	r.Multipart = append(r.Multipart, Part{
		Name: name, Filename: filename, MimeType: mimeType, Data: data,
	})
}

// HasParam reports whether key is already set anywhere this request carries
// parameters — query string, form body, or multipart part.
//
// Checking all three matters: a multipart request keeps its fields in Multipart
// rather than Form, so a Form-only check would wrongly conclude the key is
// absent and add a duplicate.
func (r *Request) HasParam(key string) bool {
	if r.Query.Has(key) {
		return true
	}
	if r.Form.Has(key) {
		return true
	}
	for _, p := range r.Multipart {
		if p.Name == key {
			return true
		}
	}
	return false
}

// SetParam writes key=value wherever this request carries its parameters: the
// query string for bodyless methods, the form or multipart body otherwise.
//
// This is what lets an Authenticator inject identity uniformly without caring
// which verb it is decorating.
func (r *Request) SetParam(key, value string) {
	switch {
	case r.Multipart != nil:
		r.AddPart(key, value)
	case r.JSON != nil, r.Raw != nil:
		// Bodies we must not disturb; fall back to the query string.
		r.SetQuery(key, value)
	case r.Method == http.MethodGet, r.Method == http.MethodDelete, r.Method == http.MethodHead:
		r.SetQuery(key, value)
	default:
		r.SetForm(key, value)
	}
}

// Build materialises the request against a base URL.
func (r *Request) Build(ctx context.Context, baseURL string) (*http.Request, error) {
	full, err := r.url(baseURL)
	if err != nil {
		return nil, err
	}

	var body io.Reader
	contentType := ""

	switch {
	case r.JSON != nil:
		buf, err := json.Marshal(r.JSON)
		if err != nil {
			return nil, fmt.Errorf("core: marshal request body: %w", err)
		}
		body, contentType = bytes.NewReader(buf), "application/json"

	case r.Multipart != nil:
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		for _, p := range r.Multipart {
			if err := writePart(mw, p); err != nil {
				return nil, err
			}
		}
		if err := mw.Close(); err != nil {
			return nil, fmt.Errorf("core: close multipart: %w", err)
		}
		contentType = mw.FormDataContentType()
		if r.MultipartContentType != "" {
			// Preserve the generated boundary, swap the subtype.
			if i := strings.Index(contentType, ";"); i >= 0 {
				contentType = r.MultipartContentType + contentType[i:]
			}
		}
		body = &buf

	case r.Form != nil:
		body = strings.NewReader(r.Form.Encode())
		contentType = "application/x-www-form-urlencoded"

	case r.Raw != nil:
		body = bytes.NewReader(r.Raw)
		contentType = r.RawContentType
	}

	req, err := http.NewRequestWithContext(ctx, r.Method, full, body)
	if err != nil {
		return nil, fmt.Errorf("core: build request: %w", err)
	}
	for k, vs := range r.Header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	if contentType != "" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", contentType)
	}
	return req, nil
}

func (r *Request) url(baseURL string) (string, error) {
	full := r.Path
	if !strings.HasPrefix(full, "http://") && !strings.HasPrefix(full, "https://") {
		full = strings.TrimSuffix(baseURL, "/") + "/" + strings.TrimPrefix(full, "/")
	}
	if len(r.Query) == 0 {
		return full, nil
	}
	u, err := url.Parse(full)
	if err != nil {
		return "", fmt.Errorf("core: parse url %q: %w", full, err)
	}
	q := u.Query()
	for k, vs := range r.Query {
		q.Del(k)
		for _, v := range vs {
			q.Add(k, v)
		}
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func writePart(mw *multipart.Writer, p Part) error {
	if p.Filename == "" {
		if err := mw.WriteField(p.Name, p.Value); err != nil {
			return fmt.Errorf("core: write field %q: %w", p.Name, err)
		}
		return nil
	}
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition",
		fmt.Sprintf(`form-data; name=%q; filename=%q`, p.Name, p.Filename))
	if p.MimeType != "" {
		h.Set("Content-Type", p.MimeType)
	}
	w, err := mw.CreatePart(h)
	if err != nil {
		return fmt.Errorf("core: create part %q: %w", p.Name, err)
	}
	if _, err := w.Write(p.Data); err != nil {
		return fmt.Errorf("core: write part %q: %w", p.Name, err)
	}
	return nil
}
