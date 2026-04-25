package azure

import (
	"context"
	"fmt"

	"github.com/tesserix/cloudnav/internal/provider"
)

// Delete satisfies provider.Deleter. Routes the generic Node-based
// call to the existing scope-specific Azure delete methods.
//
// The TUI only ever calls this with KindResource or KindResourceGroup
// — both wired below. Other kinds return ErrNotSupported so the
// caller surfaces a portal hand-off hint instead of a stack trace.
func (a *Azure) Delete(ctx context.Context, n provider.Node) error {
	subID := nodeSubID(n)
	if subID == "" {
		return fmt.Errorf("azure delete: %s has no subscription context", n.Name)
	}
	switch n.Kind {
	case provider.KindResource:
		return a.DeleteResource(ctx, subID, n.ID, n.Meta["type"])
	case provider.KindResourceGroup:
		return a.DeleteResourceGroup(ctx, subID, n.Name)
	default:
		return fmt.Errorf("%w: azure cannot delete kind %q", provider.ErrNotSupported, n.Kind)
	}
}

// Locks satisfies provider.Locker. Returns the locks bound to the
// passed Node. The TUI only ever asks at the RG scope today; other
// scopes return an empty slice (not an error) so the L overlay
// renders cleanly when the user opens it on a non-RG row.
func (a *Azure) Locks(ctx context.Context, n provider.Node) ([]provider.Lock, error) {
	subID := nodeSubID(n)
	if subID == "" {
		return nil, fmt.Errorf("azure locks: %s has no subscription context", n.Name)
	}
	if n.Kind != provider.KindResourceGroup {
		return nil, nil
	}
	all, err := a.ResourceGroupLocks(ctx, subID)
	if err != nil {
		return nil, err
	}
	out := make([]provider.Lock, 0, len(all[n.Name]))
	for _, l := range all[n.Name] {
		out = append(out, provider.Lock{
			Name:  l.Name,
			Level: l.Level,
		})
	}
	return out, nil
}

// CreateLock satisfies provider.Locker.
func (a *Azure) CreateLock(ctx context.Context, n provider.Node, reason string) error {
	subID := nodeSubID(n)
	if subID == "" {
		return fmt.Errorf("azure lock: %s has no subscription context", n.Name)
	}
	if n.Kind != provider.KindResourceGroup {
		return fmt.Errorf("%w: azure locks are RG-scoped today (got %q)", provider.ErrNotSupported, n.Kind)
	}
	name := "cloudnav-cannotdelete"
	if reason != "" {
		// reason is informational only — Azure locks don't carry a
		// free-text body; we encode the reason into the lock name
		// suffix so the L overlay can show it back.
		name = "cloudnav-" + sanitiseLockName(reason)
	}
	return a.CreateRGLock(ctx, subID, n.Name, name, "CanNotDelete")
}

// RemoveLock satisfies provider.Locker.
func (a *Azure) RemoveLock(ctx context.Context, n provider.Node, name string) error {
	subID := nodeSubID(n)
	if subID == "" {
		return fmt.Errorf("azure lock: %s has no subscription context", n.Name)
	}
	if n.Kind != provider.KindResourceGroup {
		return nil
	}
	return a.DeleteRGLock(ctx, subID, n.Name, name)
}

// nodeSubID extracts a subscription id from a node — Resource
// Graph rows carry it in Meta, RG drills inherit it from the
// active subscription frame which the TUI passes via Parent.
func nodeSubID(n provider.Node) string {
	if id := n.Meta["subscriptionId"]; id != "" {
		return id
	}
	if n.Parent != nil && n.Parent.Kind == provider.KindSubscription {
		return n.Parent.ID
	}
	return ""
}

// Compile-time assertions Azure satisfies the new interfaces.
var (
	_ provider.Deleter = (*Azure)(nil)
	_ provider.Locker  = (*Azure)(nil)
)

// sanitiseLockName drops characters that aren't valid in an Azure
// lock name, capped at 60 chars. Lock names accept a-z, 0-9, -.
func sanitiseLockName(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s) && len(out) < 50; i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-':
			out = append(out, c)
		case c >= 'A' && c <= 'Z':
			out = append(out, c-'A'+'a')
		case c == ' ':
			out = append(out, '-')
		}
	}
	if len(out) == 0 {
		return "default"
	}
	return string(out)
}
