package azure

import (
	"net"
	"net/http"
	"time"
)

// httpClient is the single HTTP client used for all Azure REST and Graph
// calls. Keep-alive + connection pooling matter: a single "load RG costs"
// action can fan out to dozens of requests, and opening a fresh TCP +
// TLS handshake per call visibly slows the TUI.
//
// Timeout is per-request, not per-connection — Azure's ARM can take up to
// ~60s for aggregate cost queries on big subscriptions, so we keep the
// outer bound generous and rely on ctx cancellation for user-initiated
// aborts.
var httpClient = &http.Client{
	Timeout: 60 * time.Second,
	Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          64,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
	},
}
