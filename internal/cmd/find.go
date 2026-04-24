package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/tesserix/cloudnav/internal/provider"
)

type searchMatch struct {
	Cloud     string `json:"cloud"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Type      string `json:"type,omitempty"`
	Location  string `json:"location,omitempty"`
	State     string `json:"state,omitempty"`
	Scope     string `json:"scope,omitempty"`
	ID        string `json:"id"`
	PortalURL string `json:"portal_url,omitempty"`

	Source  string `json:"source,omitempty"`
	Active  bool   `json:"active,omitempty"`
	Expires string `json:"expires,omitempty"`

	node provider.Node
	p    provider.Provider
}

type findOptions struct {
	Cloud         string
	Subscription  string
	ResourceGroup string
	Project       string
	Account       string
	Region        string
	JSON          bool
	Details       bool
	Limit         int
}

var findCmd = &cobra.Command{
	Use:     "find",
	Aliases: []string{"search"},
	Short:   "Search clouds, scopes, resources, and PIM/JIT roles",
	Long: `Discovery-oriented search for the cloudnav CLI.

Use this when you know part of a name, ID, scope, or role but don't want to
manually walk the hierarchy first.`,
}

var findScopesCmd = &cobra.Command{
	Use:   "scopes <query>",
	Short: "Search subscriptions, projects, accounts, regions, folders, and resource groups",
	Args:  cobra.ExactArgs(1),
	Example: `  cloudnav find scopes prod
  cloudnav find scopes finance --cloud azure
  cloudnav find scopes us-east --cloud aws
  cloudnav find scopes acme --details`,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := loadFindOptions(cmd)
		matches, warns, err := findScopeMatches(cmd.Context(), args[0], opts)
		if err != nil {
			return err
		}
		return emitFindResults(cmd.Context(), matches, warns, opts, renderNodeMatches)
	},
}

var findResourcesCmd = &cobra.Command{
	Use:   "resources <query>",
	Short: "Search resources inside a cloud or scoped hierarchy",
	Args:  cobra.ExactArgs(1),
	Example: `  cloudnav find resources web --cloud azure --subscription <sub-id>
  cloudnav find resources bucket --cloud gcp --project my-project
  cloudnav find resources lambda --cloud aws --region us-east-1
  cloudnav find resources vm-01 --cloud azure --subscription <sub-id> --details`,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := loadFindOptions(cmd)
		matches, warns, err := findResourceMatches(cmd.Context(), args[0], opts)
		if err != nil {
			return err
		}
		return emitFindResults(cmd.Context(), matches, warns, opts, renderNodeMatches)
	},
}

var findPIMCmd = &cobra.Command{
	Use:   "pim <query>",
	Short: "Search Azure PIM, GCP PAM/JIT, and AWS SSO elevation roles",
	Args:  cobra.ExactArgs(1),
	Example: `  cloudnav find pim admin
  cloudnav find pim billing --cloud azure
  cloudnav find pim prod --cloud gcp --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := loadFindOptions(cmd)
		matches, warns, err := findPIMMatches(cmd.Context(), args[0], opts)
		if err != nil {
			return err
		}
		if opts.Details {
			return fmt.Errorf("--details is not supported for PIM role searches")
		}
		return emitFindResults(cmd.Context(), matches, warns, opts, renderPIMMatches)
	},
}

func loadFindOptions(cmd *cobra.Command) findOptions {
	cloud, _ := cmd.Flags().GetString("cloud")
	sub, _ := cmd.Flags().GetString("subscription")
	rg, _ := cmd.Flags().GetString("resource-group")
	project, _ := cmd.Flags().GetString("project")
	account, _ := cmd.Flags().GetString("account")
	region, _ := cmd.Flags().GetString("region")
	asJSON, _ := cmd.Flags().GetBool("json")
	details, _ := cmd.Flags().GetBool("details")
	limit, _ := cmd.Flags().GetInt("limit")
	if limit <= 0 {
		limit = 50
	}
	return findOptions{
		Cloud:         strings.ToLower(strings.TrimSpace(cloud)),
		Subscription:  strings.TrimSpace(sub),
		ResourceGroup: strings.TrimSpace(rg),
		Project:       strings.TrimSpace(project),
		Account:       strings.TrimSpace(account),
		Region:        strings.TrimSpace(region),
		JSON:          asJSON,
		Details:       details,
		Limit:         limit,
	}
}

func findScopeMatches(ctx context.Context, query string, opts findOptions) ([]searchMatch, []error, error) {
	providers, err := pickFindProviders(opts.Cloud)
	if err != nil {
		return nil, nil, err
	}

	var (
		matches []searchMatch
		warns   []error
	)
	for _, p := range providers {
		if reachedLimit(len(matches), opts.Limit) {
			break
		}
		rows, err := collectScopeMatches(ctx, p, query, opts.Limit-len(matches))
		if err != nil {
			if opts.Cloud == "" || opts.Cloud == "all" {
				warns = append(warns, fmt.Errorf("%s: %w", p.Name(), err))
				continue
			}
			return nil, nil, err
		}
		matches = append(matches, rows...)
	}
	sortMatches(matches)
	return matches, warns, nil
}

func findResourceMatches(ctx context.Context, query string, opts findOptions) ([]searchMatch, []error, error) {
	providers, err := pickFindProviders(opts.Cloud)
	if err != nil {
		return nil, nil, err
	}

	var (
		matches []searchMatch
		warns   []error
	)
	for _, p := range providers {
		if reachedLimit(len(matches), opts.Limit) {
			break
		}
		rows, err := collectResourceMatches(ctx, p, query, opts, opts.Limit-len(matches))
		if err != nil {
			if opts.Cloud == "" || opts.Cloud == "all" {
				warns = append(warns, fmt.Errorf("%s: %w", p.Name(), err))
				continue
			}
			return nil, nil, err
		}
		matches = append(matches, rows...)
	}
	sortMatches(matches)
	return matches, warns, nil
}

func findPIMMatches(ctx context.Context, query string, opts findOptions) ([]searchMatch, []error, error) {
	providers, err := pickFindProviders(opts.Cloud)
	if err != nil {
		return nil, nil, err
	}

	var (
		matches []searchMatch
		warns   []error
	)
	for _, p := range providers {
		if reachedLimit(len(matches), opts.Limit) {
			break
		}
		rows, err := collectPIMMatches(ctx, p, query, opts.Limit-len(matches))
		if err != nil {
			if opts.Cloud == "" || opts.Cloud == "all" {
				warns = append(warns, fmt.Errorf("%s: %w", p.Name(), err))
				continue
			}
			return nil, nil, err
		}
		matches = append(matches, rows...)
	}
	sortMatches(matches)
	return matches, warns, nil
}

func pickFindProviders(cloud string) ([]provider.Provider, error) {
	switch cloud {
	case "", "all":
		return []provider.Provider{
			mustProvider(cloudAzure),
			mustProvider(cloudGCP),
			mustProvider(cloudAWS),
		}, nil
	default:
		p, err := pickProvider(cloud)
		if err != nil {
			return nil, err
		}
		return []provider.Provider{p}, nil
	}
}

func mustProvider(name string) provider.Provider {
	p, err := pickProvider(name)
	if err != nil {
		panic(err)
	}
	return p
}

func collectScopeMatches(ctx context.Context, p provider.Provider, query string, limit int) ([]searchMatch, error) {
	roots, err := p.Root(ctx)
	if err != nil {
		return nil, err
	}
	queue := append([]provider.Node(nil), roots...)
	matches := make([]searchMatch, 0, min(limit, len(queue)))

	for len(queue) > 0 && !reachedLimit(len(matches), limit) {
		n := queue[0]
		queue = queue[1:]
		if nodeMatchesQuery(n, query) {
			matches = append(matches, makeNodeMatch(p, n))
		}
		if !scopeNodeHasScopeChildren(n.Kind) {
			continue
		}
		children, err := p.Children(ctx, n)
		if err != nil {
			return nil, err
		}
		for _, child := range children {
			if child.Kind != provider.KindResource {
				queue = append(queue, child)
			}
		}
	}
	return matches, nil
}

func collectResourceMatches(ctx context.Context, p provider.Provider, query string, opts findOptions, limit int) ([]searchMatch, error) {
	roots, err := resourceSearchRoots(ctx, p, opts)
	if err != nil {
		return nil, err
	}
	matches := make([]searchMatch, 0, min(limit, len(roots)))
	for _, root := range roots {
		if reachedLimit(len(matches), limit) {
			break
		}
		children, err := p.Children(ctx, root)
		if err != nil {
			return nil, err
		}
		for _, child := range children {
			if child.Kind != provider.KindResource {
				continue
			}
			if nodeMatchesQuery(child, query) {
				matches = append(matches, makeNodeMatch(p, child))
				if reachedLimit(len(matches), limit) {
					break
				}
			}
		}
	}
	return matches, nil
}

func collectPIMMatches(ctx context.Context, p provider.Provider, query string, limit int) ([]searchMatch, error) {
	pimer, ok := p.(provider.PIMer)
	if !ok {
		return nil, fmt.Errorf("JIT/PIM not supported")
	}
	roles, err := pimer.ListEligibleRoles(ctx)
	if err != nil {
		return nil, err
	}
	matches := make([]searchMatch, 0, min(limit, len(roles)))
	for _, role := range roles {
		if roleMatchesQuery(role, query) {
			scope := strings.TrimSpace(role.ScopeName)
			if scope == "" {
				scope = strings.TrimSpace(role.Scope)
			}
			matches = append(matches, searchMatch{
				Cloud:   p.Name(),
				Kind:    "pim-role",
				Name:    role.RoleName,
				Scope:   scope,
				ID:      role.ID,
				Source:  defaultPIMSource(role.Source),
				Active:  role.Active,
				Expires: role.ActiveUntil,
			})
			if reachedLimit(len(matches), limit) {
				break
			}
		}
	}
	return matches, nil
}

func resourceSearchRoots(ctx context.Context, p provider.Provider, opts findOptions) ([]provider.Node, error) {
	switch p.Name() {
	case cloudAzure:
		return azureResourceRoots(ctx, p, opts)
	case cloudGCP:
		return gcpResourceRoots(ctx, p, opts)
	case cloudAWS:
		return awsResourceRoots(ctx, p, opts)
	default:
		return nil, fmt.Errorf("%s: resource search not implemented", p.Name())
	}
}

func azureResourceRoots(ctx context.Context, p provider.Provider, opts findOptions) ([]provider.Node, error) {
	if opts.ResourceGroup != "" && opts.Subscription == "" {
		return nil, fmt.Errorf("azure: --subscription is required with --resource-group")
	}
	if opts.ResourceGroup != "" {
		sub := provider.Node{ID: opts.Subscription, Name: opts.Subscription, Kind: provider.KindSubscription}
		return []provider.Node{{
			ID:     opts.ResourceGroup,
			Name:   opts.ResourceGroup,
			Kind:   provider.KindResourceGroup,
			Parent: &sub,
			Meta: map[string]string{
				"subscriptionId": opts.Subscription,
			},
		}}, nil
	}
	if opts.Subscription != "" {
		return p.Children(ctx, provider.Node{ID: opts.Subscription, Name: opts.Subscription, Kind: provider.KindSubscription})
	}

	subs, err := p.Root(ctx)
	if err != nil {
		return nil, err
	}
	var roots []provider.Node
	for _, sub := range subs {
		rgs, err := p.Children(ctx, sub)
		if err != nil {
			return nil, err
		}
		roots = append(roots, rgs...)
	}
	return roots, nil
}

func gcpResourceRoots(ctx context.Context, p provider.Provider, opts findOptions) ([]provider.Node, error) {
	if opts.Project != "" {
		return []provider.Node{{ID: opts.Project, Name: opts.Project, Kind: provider.KindProject}}, nil
	}
	roots, err := p.Root(ctx)
	if err != nil {
		return nil, err
	}
	var out []provider.Node
	queue := append([]provider.Node(nil), roots...)
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		switch n.Kind {
		case provider.KindProject:
			out = append(out, n)
		case provider.KindFolder:
			children, err := p.Children(ctx, n)
			if err != nil {
				return nil, err
			}
			for _, child := range children {
				if child.Kind == provider.KindFolder || child.Kind == provider.KindProject {
					queue = append(queue, child)
				}
			}
		}
	}
	return out, nil
}

func awsResourceRoots(ctx context.Context, p provider.Provider, opts findOptions) ([]provider.Node, error) {
	if opts.Region != "" {
		return []provider.Node{{ID: opts.Region, Name: opts.Region, Kind: provider.KindRegion}}, nil
	}
	accounts, err := p.Root(ctx)
	if err != nil {
		return nil, err
	}
	var roots []provider.Node
	for _, account := range accounts {
		if opts.Account != "" && !nodeMatchesAccount(account, opts.Account) {
			continue
		}
		regions, err := p.Children(ctx, account)
		if err != nil {
			return nil, err
		}
		roots = append(roots, regions...)
	}
	return roots, nil
}

func emitFindResults(ctx context.Context, matches []searchMatch, warns []error, opts findOptions, render func([]searchMatch) error) error {
	if opts.Details {
		if len(matches) != 1 {
			return fmt.Errorf("--details requires exactly 1 match, got %d", len(matches))
		}
		body, err := matches[0].p.Details(ctx, matches[0].node)
		if err != nil {
			return err
		}
		if _, err := os.Stdout.Write(body); err != nil {
			return err
		}
		if len(body) == 0 || body[len(body)-1] != '\n' {
			_, _ = fmt.Fprintln(os.Stdout)
		}
		printWarnings(warns)
		return nil
	}

	if opts.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(matches); err != nil {
			return err
		}
		printWarnings(warns)
		return nil
	}

	if len(matches) == 0 {
		fmt.Fprintln(os.Stderr, "no matches")
		printWarnings(warns)
		return nil
	}
	if err := render(matches); err != nil {
		return err
	}
	printWarnings(warns)
	return nil
}

func renderNodeMatches(matches []searchMatch) error {
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	println(tw, "CLOUD\tKIND\tNAME\tTYPE\tLOCATION\tSTATE\tSCOPE\tID")
	println(tw, "-----\t----\t----\t----\t--------\t-----\t-----\t--")
	for _, m := range matches {
		printf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			m.Cloud,
			m.Kind,
			trunc(m.Name, 32),
			trunc(findNonEmpty(m.Type, "—"), 24),
			trunc(findNonEmpty(m.Location, "—"), 20),
			trunc(findNonEmpty(m.State, "—"), 18),
			trunc(findNonEmpty(m.Scope, "—"), 30),
			trunc(m.ID, 48),
		)
	}
	return tw.Flush()
}

func renderPIMMatches(matches []searchMatch) error {
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	println(tw, "CLOUD\tSOURCE\tROLE\tSCOPE\tACTIVE\tEXPIRES")
	println(tw, "-----\t------\t----\t-----\t------\t-------")
	for _, m := range matches {
		active := "no"
		if m.Active {
			active = "yes"
		}
		printf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			m.Cloud,
			m.Source,
			trunc(m.Name, 36),
			trunc(findNonEmpty(m.Scope, "—"), 42),
			active,
			trunc(findNonEmpty(m.Expires, "—"), 24),
		)
	}
	return tw.Flush()
}

func makeNodeMatch(p provider.Provider, n provider.Node) searchMatch {
	scope := nodeScope(n)
	m := searchMatch{
		Cloud:     p.Name(),
		Kind:      kindLabel(n.Kind),
		Name:      n.Name,
		Type:      n.Meta["type"],
		Location:  n.Location,
		State:     n.State,
		Scope:     scope,
		ID:        n.ID,
		PortalURL: p.PortalURL(n),
		node:      n,
		p:         p,
	}
	if m.Type == "" {
		switch n.Kind {
		case provider.KindResource:
			m.Type = findNonEmpty(n.State, "")
		case provider.KindFolder:
			m.Type = "folder"
		}
	}
	return m
}

func nodeMatchesQuery(n provider.Node, query string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return true
	}
	fields := []string{
		n.Name,
		n.ID,
		string(n.Kind),
		n.Location,
		n.State,
	}
	for k, v := range n.Meta {
		fields = append(fields, k, v)
	}
	if n.Parent != nil {
		fields = append(fields, n.Parent.Name, n.Parent.ID, string(n.Parent.Kind))
	}
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), q) {
			return true
		}
	}
	return false
}

func roleMatchesQuery(role provider.PIMRole, query string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return true
	}
	fields := []string{
		role.RoleName,
		role.Scope,
		role.ScopeName,
		role.ID,
		role.RoleDefinitionID,
		role.GroupID,
		defaultPIMSource(role.Source),
	}
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), q) {
			return true
		}
	}
	return false
}

func nodeMatchesAccount(n provider.Node, account string) bool {
	account = strings.TrimSpace(account)
	return strings.EqualFold(n.ID, account) ||
		strings.EqualFold(n.Name, account) ||
		strings.EqualFold(n.Meta["accountId"], account)
}

func nodeScope(n provider.Node) string {
	if n.Parent != nil && n.Parent.Name != "" {
		return n.Parent.Name
	}
	switch {
	case n.Meta["subscriptionId"] != "":
		return n.Meta["subscriptionId"]
	case n.Meta["project"] != "":
		return n.Meta["project"]
	case n.Meta["region"] != "":
		return n.Meta["region"]
	case n.Meta["accountId"] != "":
		return n.Meta["accountId"]
	default:
		return ""
	}
}

func scopeNodeHasScopeChildren(kind provider.Kind) bool {
	switch kind {
	case provider.KindSubscription, provider.KindAccount, provider.KindFolder:
		return true
	default:
		return false
	}
}

func kindLabel(kind provider.Kind) string {
	switch kind {
	case provider.KindSubscription:
		return "subscription"
	case provider.KindResourceGroup:
		return "resource-group"
	case provider.KindResource:
		return "resource"
	case provider.KindProject:
		return "project"
	case provider.KindAccount:
		return "account"
	case provider.KindRegion:
		return "region"
	case provider.KindFolder:
		return "folder"
	case provider.KindTenant:
		return "tenant"
	case provider.KindCloud:
		return "cloud"
	case provider.KindCloudDisabled:
		return "cloud-disabled"
	default:
		return string(kind)
	}
}

func defaultPIMSource(source string) string {
	if strings.TrimSpace(source) == "" {
		return "azure"
	}
	return source
}

func sortMatches(matches []searchMatch) {
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Cloud != matches[j].Cloud {
			return matches[i].Cloud < matches[j].Cloud
		}
		if matches[i].Kind != matches[j].Kind {
			return matches[i].Kind < matches[j].Kind
		}
		if matches[i].Scope != matches[j].Scope {
			return matches[i].Scope < matches[j].Scope
		}
		return matches[i].Name < matches[j].Name
	})
}

func reachedLimit(count, limit int) bool {
	return limit > 0 && count >= limit
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func findNonEmpty(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func printWarnings(warns []error) {
	for _, warn := range warns {
		fmt.Fprintf(os.Stderr, "warning: %v\n", warn)
	}
}

func init() {
	findCmd.PersistentFlags().String("cloud", "all", "cloud to search: azure | gcp | aws | all")
	findCmd.PersistentFlags().Bool("json", false, "Emit JSON")
	findCmd.PersistentFlags().Bool("details", false, "If exactly one scope/resource matches, print its provider detail JSON")
	findCmd.PersistentFlags().Int("limit", 50, "Maximum number of matches to return")
	findCmd.PersistentFlags().String("subscription", "", "Azure subscription ID for narrowing resource searches")
	findCmd.PersistentFlags().String("resource-group", "", "Azure resource group name for narrowing resource searches")
	findCmd.PersistentFlags().String("project", "", "GCP project ID for narrowing resource searches")
	findCmd.PersistentFlags().String("account", "", "AWS account ID or name for narrowing resource searches")
	findCmd.PersistentFlags().String("region", "", "AWS region for narrowing resource searches")

	findCmd.AddCommand(findScopesCmd, findResourcesCmd, findPIMCmd)
	rootCmd.AddCommand(findCmd)
}
