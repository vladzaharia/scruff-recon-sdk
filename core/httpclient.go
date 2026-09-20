package core

import (
	"net"
	"net/http"
	"time"
)

// NewHTTPClient returns an *http.Client with timeouts suited to these APIs.
//
// The default timeout is generous because several SCRUFF grid endpoints are
// genuinely slow — the vendor's own client raises them to 60 seconds. Per-path
// overrides belong on Transport.Timeouts.
func NewHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   25 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			MaxIdleConns:          20,
			MaxIdleConnsPerHost:   8,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   15 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			ForceAttemptHTTP2:     true,
		},
	}
}
