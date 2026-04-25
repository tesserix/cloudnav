package currency

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/tesserix/cloudnav/internal/cache"
)

// Converter is the cloudnav-wide FX rate resolver. The user picks a
// display currency (config.DisplayCurrency or the runtime hotkey);
// every formatter passes its raw amount + native currency through
// Convert and renders the result in the display currency.
//
// Rates are sourced from frankfurter.app, cached in SQLite via the
// shared cache backend with a 24-hour TTL (rates change at most
// once a day, so anything tighter just thrashes the network).
//
// Thread-safety: Converter is process-shared. The HTTP fetch is
// guarded by a sync.Once-per-base so two concurrent cost overlays
// don't race-fetch the same rate table.
type Converter struct {
	mu          sync.Mutex
	pending     map[string]chan struct{}
	cacheStore  *cache.Store[ratePayload]
	displayCode string
}

// ratePayload is what we persist in SQLite. Keeping it on its own
// type rather than inlining map[string]float64 lets us evolve the
// shape later (e.g. adding the source / refresh time) without
// breaking older cached rows.
type ratePayload struct {
	Base  string             `json:"base"`
	Rates map[string]float64 `json:"rates"`
}

// rateCacheTTL bounds how long a cached rate table is served
// without re-hitting frankfurter. ECB updates daily; 24 hours is
// the natural ceiling. Override via CLOUDNAV_FX_TTL.
const rateCacheTTL = 24 * time.Hour

// defaultConverter is the process-wide converter formatters consult.
// nil = "no display currency configured" — every Convert call
// passes the amount through unchanged.
//
// Using a package-level singleton avoids threading a *Converter
// through every formatCost / formatAmount in every provider; the
// TUI bootstrap calls SetDefault once from config.
var (
	defaultMu        sync.RWMutex
	defaultConverter *Converter
)

// SetDefault installs the process-wide converter. Pass nil to
// disable conversion. Called from the TUI bootstrap with the user's
// `display_currency` config value.
func SetDefault(c *Converter) {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultConverter = c
}

// Default returns the current process-wide converter or nil. Safe
// to call from formatters before SetDefault has fired — Convert
// becomes a passthrough.
func Default() *Converter {
	defaultMu.RLock()
	defer defaultMu.RUnlock()
	return defaultConverter
}

// ConvertDefault is the package-level convenience formatters call
// to optionally re-denominate an amount. Returns (amount, native,
// false) when no converter is installed or when conversion fails,
// so the caller can render the source amount unchanged.
//
// The 5-second budget is inherited from LatestRates; in practice
// the cache hit makes Convert sub-millisecond.
func ConvertDefault(ctx context.Context, amount float64, native string) (float64, string, bool) {
	c := Default()
	if c == nil {
		return amount, native, false
	}
	return c.Convert(ctx, amount, native)
}

// New returns a process-wide converter backed by cache.Shared().
// Display currency is upper-cased; pass "" to disable conversion
// (every Convert call returns the original amount unchanged).
func New(displayCode string) *Converter {
	return NewWithBackend(cache.Shared(), displayCode)
}

// NewWithBackend builds a converter wired to the supplied cache
// backend. Used by tests to isolate the rate cache from the
// process-wide singleton.
func NewWithBackend(backend cache.Backend, displayCode string) *Converter {
	return &Converter{
		pending:     map[string]chan struct{}{},
		cacheStore:  cache.NewStoreWithBackend[ratePayload](backend, "fx-rates", rateCacheTTL),
		displayCode: strings.ToUpper(strings.TrimSpace(displayCode)),
	}
}

// Display returns the converter's target currency or "" when no
// conversion is configured.
func (c *Converter) Display() string {
	if c == nil {
		return ""
	}
	return c.displayCode
}

// SetDisplay updates the target currency at runtime (used by the TUI
// hotkey). Safe to call concurrently with Convert.
func (c *Converter) SetDisplay(code string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.displayCode = strings.ToUpper(strings.TrimSpace(code))
}

// Convert returns (converted-amount, target-currency, true) when
// the conversion succeeds. Returns (amount, native, false) on any
// failure mode (no display currency set, source/target unknown to
// frankfurter, FX network error, etc.) so the caller can render
// the original amount unchanged. Conversion never blocks the
// caller longer than the 5-second frankfurter timeout.
func (c *Converter) Convert(ctx context.Context, amount float64, native string) (float64, string, bool) {
	if c == nil {
		return amount, native, false
	}
	target := strings.ToUpper(strings.TrimSpace(c.displayCode))
	src := strings.ToUpper(strings.TrimSpace(native))
	if target == "" || src == "" || target == src {
		return amount, native, target == src && target != ""
	}
	rates, ok := c.ratesFor(ctx, src)
	if !ok {
		return amount, native, false
	}
	rate, ok := rates[target]
	if !ok {
		return amount, native, false
	}
	return amount * rate, target, true
}

// ratesFor returns the rate table with `base` as the source
// currency, hitting the cache first and the network on miss. The
// per-base sync.Once-style channel coalesces concurrent callers
// asking for the same base.
func (c *Converter) ratesFor(ctx context.Context, base string) (map[string]float64, bool) {
	if cached, ok := c.cacheStore.Get(base); ok && len(cached.Rates) > 0 {
		return cached.Rates, true
	}
	// One inflight fetch per base — extra callers wait on the
	// channel rather than firing parallel HTTP requests.
	c.mu.Lock()
	if ch, inflight := c.pending[base]; inflight {
		c.mu.Unlock()
		<-ch
		// Now there's either a cached row or the fetch failed
		// — both cases handled by re-reading the cache.
		if cached, ok := c.cacheStore.Get(base); ok && len(cached.Rates) > 0 {
			return cached.Rates, true
		}
		return nil, false
	}
	ch := make(chan struct{})
	c.pending[base] = ch
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, base)
		close(ch)
		c.mu.Unlock()
	}()

	rates, err := LatestRates(ctx, base)
	if err != nil || len(rates) == 0 {
		return nil, false
	}
	_ = c.cacheStore.Set(base, ratePayload{Base: base, Rates: rates})
	return rates, true
}
