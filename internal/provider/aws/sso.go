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

	"github.com/tesserix/cloudnav/internal/provider"
)

type ssoProfile struct {
	name        string
	startURL    string
	region      string
	accountID   string
	roleName    string
	ssoSession  string
	accountName string
}

func (a *AWS) ListEligibleRoles(_ context.Context) ([]provider.PIMRole, error) {
	profiles, err := loadSSOProfiles(awsConfigPath())
	if err != nil {
		return nil, err
	}
	if len(profiles) == 0 {
		return nil, fmt.Errorf("aws: no SSO profiles in ~/.aws/config — run `aws configure sso` first")
	}
	roles := make([]provider.PIMRole, 0, len(profiles))
	for _, p := range profiles {
		scopeName := p.accountName
		if scopeName == "" {
			scopeName = p.accountID
		}
		roles = append(roles, provider.PIMRole{
			ID:        p.name,
			RoleName:  p.roleName,
			Scope:     p.accountID,
			ScopeName: scopeName,
		})
	}
	return roles, nil
}

func (a *AWS) ActivateRole(ctx context.Context, role provider.PIMRole, justification string, _ int) error {
	_ = justification
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
