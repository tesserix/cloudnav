// Package azure implements provider.Provider by wrapping the `az` CLI.
package azure

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tesserix/cloudnav/internal/cli"
	"github.com/tesserix/cloudnav/internal/provider"
)

type Azure struct {
	az *cli.Runner

	mu           sync.RWMutex
	tenants      map[string]string // tenantId → displayName
	subs         map[string]string // subscriptionId → name
	subTenants   map[string]string // subscriptionId → tenantId
	signedInOIDs map[string]string // tenantId → signed-in user's object-id (Graph)

	// Short-lived Root() cache. Navigating back to the clouds screen and
	// re-entering Azure would otherwise re-run `az account list` every
	// time; that call costs ~1–2s of CLI startup plus an ARM round-trip,
	// which is the bulk of the "loading azure..." wait users see.
	rootMu       sync.Mutex
	rootCache    []provider.Node
	rootCachedAt time.Time
}

// rootCacheTTL bounds how long a Root() result is reused within one process.
// Short enough that a freshly added subscription shows up quickly if the user
// presses `r` to refresh or restarts the app; long enough to make back/forward
// navigation feel instant.
const rootCacheTTL = 90 * time.Second

func New() *Azure {
	r := cli.New("az")
	r.Timeout = 2 * time.Minute
	return &Azure{az: r}
}

func (a *Azure) Name() string { return "azure" }

// tenantName returns the cached display name for a tenant or "" if unknown.
func (a *Azure) tenantName(id string) string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.tenants[id]
}

// subName returns the cached name for a subscription or "" if unknown.
func (a *Azure) subName(id string) string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.subs[id]
}

// tenantForSub returns the tenantId a subscription belongs to. Falls back to
// the active az context tenant when unknown.
func (a *Azure) tenantForSub(ctx context.Context, subID string) string {
	a.mu.RLock()
	t := a.subTenants[subID]
	a.mu.RUnlock()
	if t != "" {
		return t
	}
	// Lazy lookup: ask az directly for this subscription.
	out, err := a.az.Run(ctx, "account", "show", "--subscription", subID, "-o", "json")
	if err != nil {
		return ""
	}
	var s subJSON
	if err := json.Unmarshal(out, &s); err != nil {
		return ""
	}
	if s.TenantID != "" {
		a.mu.Lock()
		if a.subTenants == nil {
			a.subTenants = map[string]string{}
		}
		a.subTenants[subID] = s.TenantID
		a.mu.Unlock()
	}
	return s.TenantID
}

func (a *Azure) putSubs(m map[string]string) {
	a.mu.Lock()
	a.subs = m
	a.mu.Unlock()
}

func (a *Azure) putSubTenants(m map[string]string) {
	a.mu.Lock()
	a.subTenants = m
	a.mu.Unlock()
}

func (a *Azure) putTenants(m map[string]string) {
	a.mu.Lock()
	a.tenants = m
	a.mu.Unlock()
}

// fetchTenants is best-effort — failure is non-fatal, we just fall back to
// showing the tenantId when rendering.
func (a *Azure) fetchTenants(ctx context.Context) {
	out, err := a.az.Run(ctx, "rest", "--method", "GET",
		"--url", "https://management.azure.com/tenants?api-version=2022-09-01")
	if err != nil {
		return
	}
	var env struct {
		Value []struct {
			TenantID    string `json:"tenantId"`
			DisplayName string `json:"displayName"`
		} `json:"value"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		return
	}
	m := make(map[string]string, len(env.Value))
	for _, t := range env.Value {
		m[t.TenantID] = t.DisplayName
	}
	a.putTenants(m)
}

func (a *Azure) LoggedIn(ctx context.Context) error {
	_, err := a.az.Run(ctx, "account", "show", "-o", "json")
	return err
}

// LoginCommand returns the argv that runs Azure CLI's interactive login.
func (a *Azure) LoginCommand() (string, []string) {
	return "az", []string{"login"}
}

// InstallHint points first-time users at the Azure CLI installer.
func (a *Azure) InstallHint() string {
	return "install Azure CLI: https://learn.microsoft.com/cli/azure/install-azure-cli"
}

// InstallPlan returns the right native install command for the current OS.
// Prefers Homebrew on macOS (and Linux when available) because it doesn't
// need sudo and cleans up neatly.
func (a *Azure) InstallPlan(goos string) ([]provider.InstallStep, bool) {
	switch goos {
	case "darwin":
		return []provider.InstallStep{{
			Description: "brew install azure-cli",
			Bin:         "brew", Args: []string{"install", "azure-cli"},
		}}, true
	case "linux":
		if _, err := exec.LookPath("brew"); err == nil {
			return []provider.InstallStep{{
				Description: "brew install azure-cli",
				Bin:         "brew", Args: []string{"install", "azure-cli"},
			}}, true
		}
		return []provider.InstallStep{{
			Description: "curl | bash installer from Microsoft (will prompt for sudo)",
			Bin:         "sh", Args: []string{"-c", "curl -sL https://aka.ms/InstallAzureCLIDeb | sudo bash"},
			NeedsSudo: true,
		}}, true
	case "windows":
		return []provider.InstallStep{{
			Description: "winget install Microsoft.AzureCLI",
			Bin:         "winget", Args: []string{"install", "-e", "--id", "Microsoft.AzureCLI"},
		}}, true
	}
	return nil, false
}

// doTenantRequest sends a Management API call using a bearer token scoped to
// the subscription's home tenant. This avoids the "wrong tenant context"
// failures that `az rest` hits on cross-tenant subscriptions (e.g. a Prod
// login querying a DevTest cost or advisor endpoint).
func (a *Azure) doTenantRequest(ctx context.Context, subID, method, url string, body []byte) ([]byte, error) {
	tid := a.tenantForSub(ctx, subID)
	if tid == "" {
		return nil, fmt.Errorf("azure: could not resolve tenant for subscription %s — run 'az account list' to check access", subID)
	}
	token, err := a.tenantToken(ctx, tid)
	if err != nil {
		return nil, fmt.Errorf("azure: tenant %s not authenticated — run 'az login --tenant %s' and retry (%w)", tid, tid, err)
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("azure: %s %s -> %d: %s", method, url, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return b, nil
}

func (a *Azure) postJSONForSub(ctx context.Context, subID, url string, body []byte) ([]byte, error) {
	return a.doTenantRequest(ctx, subID, http.MethodPost, url, body)
}

func (a *Azure) getJSONForSub(ctx context.Context, subID, url string) ([]byte, error) {
	return a.doTenantRequest(ctx, subID, http.MethodGet, url, nil)
}

func (a *Azure) putJSONForSub(ctx context.Context, subID, url string, body []byte) ([]byte, error) {
	return a.doTenantRequest(ctx, subID, http.MethodPut, url, body)
}

type subJSON struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	State    string `json:"state"`
	TenantID string `json:"tenantId"`
	User     struct {
		Name string `json:"name"`
	} `json:"user"`
}

func (a *Azure) Root(ctx context.Context) ([]provider.Node, error) {
	// 1. In-memory cache — hottest path; survives back/forward in one session.
	if cached := a.readRootMem(); cached != nil {
		return cached, nil
	}

	// 2. Disk cache — cold-start speedup across process restarts. Serving
	// from disk still populates the in-memory maps the rest of the provider
	// depends on.
	if disk, ok := readRootDiskCache(); ok {
		a.hydrateFromCache(disk)
		nodes := cloneNodes(disk.Nodes)
		a.writeRootMem(nodes)
		return nodes, nil
	}

	// 3. Live fetch. `az account list` and the tenants API call are
	// independent, so run them concurrently — previously the tenants fetch
	// added a second az CLI startup to the critical path.
	var (
		wg      sync.WaitGroup
		listOut []byte
		listErr error
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		listOut, listErr = a.az.Run(ctx, "account", "list", "-o", "json")
	}()
	go func() {
		defer wg.Done()
		a.fetchTenants(ctx)
	}()
	wg.Wait()

	if listErr != nil {
		return nil, listErr
	}
	nodes, err := parseSubs(listOut)
	if err != nil {
		return nil, err
	}
	subCache := make(map[string]string, len(nodes))
	tenantCache := make(map[string]string, len(nodes))
	for i := range nodes {
		subCache[nodes[i].ID] = nodes[i].Name
		if t := nodes[i].Meta["tenantId"]; t != "" {
			tenantCache[nodes[i].ID] = t
		}
		if name := a.tenantName(nodes[i].Meta["tenantId"]); name != "" {
			nodes[i].Meta["tenantName"] = name
		}
	}
	a.putSubs(subCache)
	a.putSubTenants(tenantCache)

	a.writeRootMem(nodes)
	writeRootDiskCache(nodes, a.snapshotTenants(), tenantCache)

	return nodes, nil
}

// readRootMem returns a defensive copy of the in-memory cached nodes when
// they're still fresh, or nil when the caller should look elsewhere.
func (a *Azure) readRootMem() []provider.Node {
	a.rootMu.Lock()
	defer a.rootMu.Unlock()
	if a.rootCache == nil || time.Since(a.rootCachedAt) >= rootCacheTTL {
		return nil
	}
	return cloneNodes(a.rootCache)
}

// writeRootMem stores a defensive copy so callers can mutate returned slices
// without corrupting the cache.
func (a *Azure) writeRootMem(nodes []provider.Node) {
	a.rootMu.Lock()
	a.rootCache = cloneNodes(nodes)
	a.rootCachedAt = time.Now()
	a.rootMu.Unlock()
}

// hydrateFromCache restores the maps that other Azure methods depend on
// when we serve Root() entirely from disk. Without this, tenantForSub and
// friends would start empty and trigger lazy round-trips for every lookup.
func (a *Azure) hydrateFromCache(c *rootCacheFile) {
	subs := make(map[string]string, len(c.Nodes))
	for _, n := range c.Nodes {
		subs[n.ID] = n.Name
	}
	a.putSubs(subs)
	if len(c.SubTenants) > 0 {
		a.putSubTenants(c.SubTenants)
	}
	if len(c.Tenants) > 0 {
		a.putTenants(c.Tenants)
	}
}

// snapshotTenants returns a copy of the current tenants map for inclusion in
// the disk cache payload.
func (a *Azure) snapshotTenants() map[string]string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if len(a.tenants) == 0 {
		return nil
	}
	out := make(map[string]string, len(a.tenants))
	for k, v := range a.tenants {
		out[k] = v
	}
	return out
}

// cloneNodes returns a shallow copy of a []provider.Node slice. Meta is not
// deep-copied because we never mutate per-node meta after Root() builds them.
func cloneNodes(in []provider.Node) []provider.Node {
	if in == nil {
		return nil
	}
	out := make([]provider.Node, len(in))
	copy(out, in)
	return out
}

// InvalidateRootCache drops any memoized Root() result (in-memory and on
// disk) so the next call hits the az CLI fresh. Call this from the UI's
// refresh action when the user wants to force-refetch subscriptions — e.g.
// after an az login change or a subscription having been added.
func (a *Azure) InvalidateRootCache() {
	a.rootMu.Lock()
	a.rootCache = nil
	a.rootCachedAt = time.Time{}
	a.rootMu.Unlock()
	removeRootDiskCache()
}

func parseSubs(data []byte) ([]provider.Node, error) {
	var subs []subJSON
	if err := json.Unmarshal(data, &subs); err != nil {
		return nil, fmt.Errorf("parse az account list: %w", err)
	}
	nodes := make([]provider.Node, 0, len(subs))
	for _, s := range subs {
		nodes = append(nodes, provider.Node{
			ID:    s.ID,
			Name:  s.Name,
			Kind:  provider.KindSubscription,
			State: s.State,
			Meta: map[string]string{
				"tenantId": s.TenantID,
				"user":     s.User.Name,
			},
		})
	}
	return nodes, nil
}

// subIDFromScope extracts the subscription UUID from an Azure resource scope
// like "/subscriptions/<uuid>/resourceGroups/...".
func subIDFromScope(scope string) string {
	const prefix = "/subscriptions/"
	rest := strings.TrimPrefix(scope, prefix)
	if rest == scope {
		return ""
	}
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		return rest[:i]
	}
	return rest
}

func (a *Azure) Children(ctx context.Context, parent provider.Node) ([]provider.Node, error) {
	switch parent.Kind {
	case provider.KindSubscription:
		return a.resourceGroups(ctx, parent)
	case provider.KindResourceGroup:
		return a.resources(ctx, parent)
	default:
		return nil, fmt.Errorf("azure: no children for kind %q", parent.Kind)
	}
}

type rgJSON struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Location   string            `json:"location"`
	Tags       map[string]string `json:"tags"`
	Properties struct {
		ProvisioningState string `json:"provisioningState"`
	} `json:"properties"`
}

func (a *Azure) resourceGroups(ctx context.Context, sub provider.Node) ([]provider.Node, error) {
	// ARM doesn't expose createdTime on the group-list response and Azure
	// Resource Graph's resourcecontainers table only has it for a subset of
	// tenants, so we don't show it on the RG view. Per-resource creation
	// dates (below) come from $expand=createdTime which is reliable.
	out, err := a.az.Run(ctx, "group", "list", "--subscription", sub.ID, "-o", "json")
	if err != nil {
		return nil, err
	}
	return parseRGs(out, sub)
}

func parseRGs(data []byte, sub provider.Node) ([]provider.Node, error) {
	var rgs []rgJSON
	if err := json.Unmarshal(data, &rgs); err != nil {
		return nil, fmt.Errorf("parse az group list: %w", err)
	}
	nodes := make([]provider.Node, 0, len(rgs))
	parent := sub
	for _, r := range rgs {
		meta := map[string]string{
			"tenantId":       sub.Meta["tenantId"],
			"subscriptionId": sub.ID,
		}
		if tagsStr := formatTags(r.Tags); tagsStr != "" {
			meta["tags"] = tagsStr
		}
		nodes = append(nodes, provider.Node{
			ID:       r.ID,
			Name:     r.Name,
			Kind:     provider.KindResourceGroup,
			Location: r.Location,
			State:    r.Properties.ProvisioningState,
			Parent:   &parent,
			Meta:     meta,
		})
	}
	return nodes, nil
}

type resJSON struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Location    string            `json:"location"`
	Type        string            `json:"type"`
	CreatedTime string            `json:"createdTime"`
	ChangedTime string            `json:"changedTime"`
	Tags        map[string]string `json:"tags"`
}

func (a *Azure) resources(ctx context.Context, rg provider.Node) ([]provider.Node, error) {
	subID := rg.Meta["subscriptionId"]
	if subID == "" && rg.Parent != nil {
		subID = rg.Parent.ID
	}
	if subID == "" {
		return nil, fmt.Errorf("azure: resource group %q has no subscription context", rg.Name)
	}
	// $expand=createdTime,changedTime surfaces per-resource audit timestamps
	// that ARM doesn't return by default. Some providers / api-versions
	// reject this expand — fall back to the plain list so drilling always
	// works; we just lose the CREATED column for those resources.
	out, err := a.az.Run(ctx,
		"resource", "list",
		"--resource-group", rg.Name,
		"--subscription", subID,
		"--expand", "createdTime,changedTime",
		"-o", "json",
	)
	if err != nil {
		out, err = a.az.Run(ctx,
			"resource", "list",
			"--resource-group", rg.Name,
			"--subscription", subID,
			"-o", "json",
		)
		if err != nil {
			return nil, err
		}
	}
	return parseResources(out, rg, subID)
}

func parseResources(data []byte, rg provider.Node, subID string) ([]provider.Node, error) {
	var items []resJSON
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("parse az resource list: %w", err)
	}
	nodes := make([]provider.Node, 0, len(items))
	parent := rg
	for _, r := range items {
		meta := map[string]string{
			"type":           r.Type,
			"tenantId":       rg.Meta["tenantId"],
			"subscriptionId": subID,
		}
		if r.CreatedTime != "" {
			meta["createdTime"] = r.CreatedTime
		}
		if r.ChangedTime != "" {
			meta["changedTime"] = r.ChangedTime
		}
		if tagsStr := formatTags(r.Tags); tagsStr != "" {
			meta["tags"] = tagsStr
		}
		nodes = append(nodes, provider.Node{
			ID:       r.ID,
			Name:     r.Name,
			Kind:     provider.KindResource,
			Location: r.Location,
			State:    shortType(r.Type),
			Parent:   &parent,
			Meta:     meta,
		})
	}
	return nodes, nil
}

func (a *Azure) PortalURL(n provider.Node) string {
	base := "https://portal.azure.com"
	if t := n.Meta["tenantId"]; t != "" {
		base += "/#@" + t
	} else {
		base += "/#"
	}
	switch n.Kind {
	case provider.KindSubscription, provider.KindResourceGroup, provider.KindResource:
		return base + "/resource" + n.ID
	default:
		return base
	}
}

func (a *Azure) Details(ctx context.Context, n provider.Node) ([]byte, error) {
	switch n.Kind {
	case provider.KindResource, provider.KindResourceGroup:
		subID := n.Meta["subscriptionId"]
		if subID == "" {
			subID = subIDFromScope(n.ID)
		}
		if subID != "" {
			return a.az.Run(ctx, "resource", "show", "--ids", n.ID, "--subscription", subID, "-o", "json")
		}
		return a.az.Run(ctx, "resource", "show", "--ids", n.ID, "-o", "json")
	case provider.KindSubscription:
		return a.az.Run(ctx, "account", "show", "--subscription", n.ID, "-o", "json")
	default:
		return nil, fmt.Errorf("azure: no detail view for kind %q", n.Kind)
	}
}

// formatTags renders an Azure tags map as a stable, compact "k=v, k=v"
// string that fits nicely in a TUI column. Keys are sorted so the output
// is deterministic (tests and diffs) and two calls produce the same
// rendering regardless of Go's randomised map iteration order.
func formatTags(tags map[string]string) string {
	if len(tags) == 0 {
		return ""
	}
	keys := make([]string, 0, len(tags))
	for k := range tags {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(k)
		v := tags[k]
		if v != "" {
			b.WriteByte('=')
			b.WriteString(v)
		}
	}
	return b.String()
}

// shortType trims "Microsoft.Compute/virtualMachines" to "virtualMachines".
func shortType(t string) string {
	for i := len(t) - 1; i >= 0; i-- {
		if t[i] == '/' {
			return t[i+1:]
		}
	}
	return t
}
