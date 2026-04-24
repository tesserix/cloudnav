package azure

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
)

// fetchWithToken issues a bearer-authenticated GET and returns the body. Any
// 4xx/5xx collapses into a single error that puts the server's reason FIRST
// so the caller's status bar (which truncates by terminal width) surfaces
// something useful instead of an HTTP-status prefix.
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
		return nil, fmt.Errorf("%s [HTTP %d]", trimAPIErr(body), resp.StatusCode)
	}
	return body, nil
}

// graphPOST issues a Microsoft Graph POST with a tenant-scoped Graph token.
// Shared by Entra directory-role and PIM-for-Groups activation. Uses
// doWithRetry so a 429 on Graph doesn't abort activation.
func graphPOST(ctx context.Context, token, url string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := doWithRetry(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("%s [HTTP %d on Graph POST]", trimAPIErr(raw), resp.StatusCode)
	}
	return nil
}
