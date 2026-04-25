package gcp

import (
	"context"
	"errors"
	"time"

	"golang.org/x/oauth2/google"
)

// checkADC verifies Application Default Credentials are available
// and produce a usable token, without spawning gcloud. ADC resolves
// in this order (per Google's spec):
//
//  1. GOOGLE_APPLICATION_CREDENTIALS env var → service-account JSON.
//  2. gcloud's well-known location (~/.config/gcloud/application_
//     default_credentials.json) — populated by `gcloud auth
//     application-default login`.
//  3. Compute / Cloud Run metadata server (when running on GCP).
//  4. Workload identity federation.
//
// Any of those resolving to a TokenSource that can mint a valid
// access token counts as logged in. We do require a token mint
// (not just construction of credentials) because stale gcloud
// creds with expired refresh tokens will construct successfully
// but fail at first use.
func (g *GCP) checkADC(ctx context.Context) error {
	// Tight timeout — this is on the hot path of cloudnav startup
	// and the metadata server / refresh round-trip should be
	// sub-second. If it isn't, fall through to the gcloud CLI
	// path which gives the user a more familiar error.
	c, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	// FindDefaultCredentials runs the resolution chain above.
	// "https://www.googleapis.com/auth/cloud-platform" is the
	// universal scope every cloud.google.com SDK uses by default.
	creds, err := google.FindDefaultCredentials(c, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return err
	}
	if creds == nil || creds.TokenSource == nil {
		return errors.New("gcp: ADC resolved no usable token source")
	}
	tok, err := creds.TokenSource.Token()
	if err != nil {
		return err
	}
	if tok == nil || tok.AccessToken == "" {
		return errors.New("gcp: ADC produced an empty access token")
	}
	return nil
}
