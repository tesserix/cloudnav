package azure

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// fetchWithToken issues a bearer-authenticated GET and returns the body. Any
// 4xx/5xx collapses into a single error that preserves the response payload
// so the caller's error message is useful.
func fetchWithToken(ctx context.Context, client *http.Client, url, token string) ([]byte, error) {
	_ = client // retained for signature compat; doWithRetry uses the package httpClient
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := doWithRetry(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

// graphPOST issues a Microsoft Graph POST with a tenant-scoped Graph token.
// Shared by Entra directory-role and PIM-for-Groups activation.
func graphPOST(ctx context.Context, token, url string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("graph POST %s -> %d: %s", url, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}
