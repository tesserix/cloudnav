package aws

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/tesserix/cloudnav/internal/provider"
)

// pimSrcAWSSSO tags entries coming from AWS IAM Identity Center /
// configured SSO profiles. Kept distinct from Azure PIM surfaces so the
// TUI source badge / filter can switch on it.
const pimSrcAWSSSO = "aws-sso"

// ssoProbeTimeoutHint is surfaced in the diagnostic row when no profiles
// have an active session. Keeps the "run this to fix it" copy in one
// place.
const ssoProbeTimeoutHint = "run: aws sso login --profile <name>"

type ssoProfile struct {
	name        string
	startURL    string
	region      string
	accountID   string
	roleName    string
	ssoSession  string
	accountName string
}

func (a *AWS) ListEligibleRoles(ctx context.Context) ([]provider.PIMRole, error) {
	profiles, err := loadSSOProfiles(awsConfigPath())
	if err != nil {
		return nil, err
	}
	if len(profiles) == 0 {
		return nil, fmt.Errorf("aws: no SSO profiles in ~/.aws/config — run `aws configure sso` first")
	}

	// Probe every profile in parallel to see which ones currently have
	// an active SSO session. Capped so a user with 50 profiles doesn't
	// spawn 50 concurrent aws processes — one wave of 8 is plenty and
	// still finishes in under a second for typical session caches.
	activeProfiles := a.probeSSOProfiles(ctx, profiles)

	roles := make([]provider.PIMRole, 0, len(profiles))
	anyActive := false
	for _, p := range profiles {
		scopeName := p.accountName
		if scopeName == "" {
			scopeName = p.accountID
		}
		role := provider.PIMRole{
			ID:        p.name,
			RoleName:  p.roleName,
			Scope:     p.accountID,
			ScopeName: scopeName,
			Source:    pimSrcAWSSSO,
		}
		if activeProfiles[p.name] {
			role.Active = true
			// AWS doesn't expose the SSO session expiry through the CLI
			// in a reliable way; leave ActiveUntil empty rather than
			// fake a value. The UI already renders "ACTIVE" without a
			// countdown when ActiveUntil is empty.
			anyActive = true
		}
		roles = append(roles, role)
	}

	// When nothing has an active session, prepend a single diagnostic
	// row so the user immediately sees why Activate would no-op. Same
	// pattern the Azure multi-tenant fix uses. Includes the first
	// non-empty sso_start_url so the user can jump straight to the
	// Identity Center portal without digging through their config.
	if !anyActive {
		hint := ssoProbeTimeoutHint
		if url := firstStartURL(profiles); url != "" {
			hint = hint + "  (portal: " + url + ")"
		}
		roles = append([]provider.PIMRole{{
			ID:        "diag:aws-sso:no-session",
			RoleName:  "⚠ no active AWS SSO session",
			ScopeName: hint,
			Source:    pimSrcAWSSSO,
		}}, roles...)
	}
	return roles, nil
}

// firstStartURL returns the sso_start_url of the first profile that has
// one configured. Used to enrich the diagnostic row so the user sees
// exactly which portal to sign into rather than having to open their
// aws config file to find it.
func firstStartURL(profiles []ssoProfile) string {
	for _, p := range profiles {
		if p.startURL != "" {
			return p.startURL
		}
	}
	return ""
}

// probeSSOProfiles runs `aws sts get-caller-identity --profile X` against
// every profile in parallel. A successful response = a valid SSO session
// that the user can actually use; anything else (missing token, expired
// session, unresolvable start-url) falls into the "needs login" bucket.
func (a *AWS) probeSSOProfiles(ctx context.Context, profiles []ssoProfile) map[string]bool {
	out := map[string]bool{}
	var mu sync.Mutex
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for _, p := range profiles {
		wg.Add(1)
		go func(p ssoProfile) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if _, err := a.aws.Run(ctx, "sts", "get-caller-identity", "--profile", p.name, "--output", "json"); err == nil {
				mu.Lock()
				out[p.name] = true
				mu.Unlock()
			}
		}(p)
	}
	wg.Wait()
	return out
}

func (a *AWS) ActivateRole(ctx context.Context, role provider.PIMRole, justification string, _ int) error {
	_ = justification
	if strings.HasPrefix(role.ID, "diag:") {
		return fmt.Errorf("that row is a diagnostic — %s, then reopen PIM", ssoProbeTimeoutHint)
	}
	if role.ID == "" {
		return fmt.Errorf("aws: empty profile name for activation")
	}
	cmd := exec.CommandContext(ctx, "aws", "sso", "login", "--profile", role.ID)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func awsConfigPath() string {
	if v := os.Getenv("AWS_CONFIG_FILE"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".aws", "config")
}

func loadSSOProfiles(path string) ([]ssoProfile, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read aws config: %w", err)
	}
	defer func() { _ = f.Close() }()

	sections := map[string]map[string]string{}
	var current string
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			current = strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")
			sections[current] = map[string]string{}
			continue
		}
		if current == "" {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		k := strings.TrimSpace(line[:eq])
		v := strings.TrimSpace(line[eq+1:])
		sections[current][k] = v
	}
	if err := s.Err(); err != nil {
		return nil, err
	}

	profiles := make([]ssoProfile, 0, len(sections))
	for name, kv := range sections {
		role := kv["sso_role_name"]
		account := kv["sso_account_id"]
		if role == "" || account == "" {
			continue
		}
		profileName := strings.TrimPrefix(name, "profile ")
		profiles = append(profiles, ssoProfile{
			name:       profileName,
			startURL:   kv["sso_start_url"],
			region:     kv["sso_region"],
			accountID:  account,
			roleName:   role,
			ssoSession: kv["sso_session"],
		})
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].name < profiles[j].name })
	return profiles, nil
}
