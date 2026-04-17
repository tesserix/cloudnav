package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type Lock struct {
	Name  string
	Level string
	Scope string
	RG    string
}

func (a *Azure) ResourceGroupLocks(ctx context.Context, subID string) (map[string][]Lock, error) {
	out, err := a.az.Run(ctx, "lock", "list", "--subscription", subID, "-o", "json")
	if err != nil {
		return nil, fmt.Errorf("azure lock list: %w", err)
	}
	return parseLocks(out)
}

func parseLocks(data []byte) (map[string][]Lock, error) {
	var items []struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Level string `json:"level"`
	}
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("parse locks: %w", err)
	}
	out := make(map[string][]Lock, len(items))
	for _, it := range items {
		rg := rgFromScope(it.ID)
		if rg == "" {
			continue
		}
		key := strings.ToLower(rg)
		out[key] = append(out[key], Lock{
			Name:  it.Name,
			Level: it.Level,
			Scope: it.ID,
			RG:    rg,
		})
	}
	return out, nil
}

func rgFromScope(scope string) string {
	const marker = "/resourceGroups/"
	i := strings.Index(strings.ToLower(scope), strings.ToLower(marker))
	if i < 0 {
		return ""
	}
	rest := scope[i+len(marker):]
	if j := strings.Index(rest, "/"); j >= 0 {
		return rest[:j]
	}
	return rest
}

func (a *Azure) CreateRGLock(ctx context.Context, subID, rg, name, level string) error {
	if level == "" {
		level = "CanNotDelete"
	}
	if name == "" {
		name = "cloudnav-" + strings.ToLower(level)
	}
	_, err := a.az.Run(ctx,
		"lock", "create",
		"--name", name,
		"--lock-type", level,
		"--resource-group", rg,
		"--subscription", subID,
	)
	return err
}

func (a *Azure) DeleteRGLock(ctx context.Context, subID, rg, lockName string) error {
	_, err := a.az.Run(ctx,
		"lock", "delete",
		"--name", lockName,
		"--resource-group", rg,
		"--subscription", subID,
	)
	return err
}

func (a *Azure) DeleteResourceGroup(ctx context.Context, subID, rg string) error {
	_, err := a.az.Run(ctx,
		"group", "delete",
		"--name", rg,
		"--subscription", subID,
		"--yes", "--no-wait",
	)
	return err
}
