package core

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

// MediaRef points at a downloadable asset.
//
// Both networks hand out absolute URLs that must be used verbatim — Recon's CDN
// paths are unsigned but opaque, and SCRUFF's chat and album URLs are
// CloudFront-signed with an expiry and a rotating host. Never reconstruct these
// from parts.
type MediaRef struct {
	URL      string
	MimeType string
	Width    int
	Height   int
	// Filename is a suggested name, when the API supplies one.
	Filename string
	// Cookies must accompany the request for some assets. SCRUFF's HLS
	// manifests are signed by cookie rather than by query string.
	Cookies map[string]string
	// Expires, when non-zero, is a hint that the URL is short-lived and should
	// be re-resolved rather than cached.
	Expires bool
}

// Blob is downloaded media.
type Blob struct {
	Data     []byte
	MimeType string
	Filename string
}

// MediaResolver turns an opaque network-specific media identifier into a
// fetchable MediaRef. SCRUFF needs this because chat media URLs are minted per
// request; Recon does not, because its URLs arrive inline.
type MediaResolver interface {
	ResolveMedia(ctx context.Context, id string) (MediaRef, error)
}

// Downloader fetches a MediaRef.
type Downloader interface {
	Download(ctx context.Context, ref MediaRef) (Blob, error)
}

// MaxMediaBytes bounds a single download. Recon allows 50 MiB uploads, so this
// leaves headroom without letting a bad URL exhaust memory.
const MaxMediaBytes = 100 << 20

// DownloadMedia fetches ref using hc.
//
// decorate, if non-nil, may add headers — Recon's CDN reads through the gateway
// for chat attachments and wants a bearer token; SCRUFF's CDNs want none.
func DownloadMedia(ctx context.Context, hc Doer, ref MediaRef, decorate func(*http.Request)) (Blob, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ref.URL, nil)
	if err != nil {
		return Blob{}, fmt.Errorf("core: build media request: %w", err)
	}
	for k, v := range ref.Cookies {
		req.AddCookie(&http.Cookie{Name: k, Value: v})
	}
	if decorate != nil {
		decorate(req)
	}
	if hc == nil {
		hc = NewHTTPClient()
	}

	resp, err := hc.Do(req)
	if err != nil {
		return Blob{}, fmt.Errorf("core: download media: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, MaxErrorBodyBytes))
		return Blob{}, &APIError{
			Method:     http.MethodGet,
			URL:        req.URL.Redacted(),
			StatusCode: resp.StatusCode,
			Body:       TruncateBody(body),
			Header:     resp.Header,
		}
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxMediaBytes+1))
	if err != nil {
		return Blob{}, fmt.Errorf("core: read media: %w", err)
	}
	if len(data) > MaxMediaBytes {
		return Blob{}, fmt.Errorf("core: media exceeds %d bytes", MaxMediaBytes)
	}

	mime := ref.MimeType
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		mime = ct
	}
	if mime == "" {
		mime = http.DetectContentType(data)
	}
	return Blob{Data: data, MimeType: mime, Filename: ref.Filename}, nil
}
