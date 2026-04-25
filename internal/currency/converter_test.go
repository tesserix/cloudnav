package currency

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tesserix/cloudnav/internal/cache"
)

// stubFrankfurter returns a test server that emulates the
// frankfurter.app /latest endpoint with a fixed rate table.
func stubFrankfurter(t *testing.T, base string, rates map[string]float64) *httptest.Server {
	t.Helper()
	body := `{"base":"` + base + `","date":"2026-04-25","rates":{`
	first := true
	for k, v := range rates {
		if !first {
			body += ","
		}
		first = false
		body += `"` + k + `":` + floatJSON(v)
	}
	body += `}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	prev := frankfurterURL
	frankfurterURL = srv.URL + "/latest"
	t.Cleanup(func() { frankfurterURL = prev })
	return srv
}

func floatJSON(v float64) string {
	switch v {
	case 1.0:
		return "1.0"
	case 1.05:
		return "1.05"
	case 0.79:
		return "0.79"
	case 0.85:
		return "0.85"
	default:
		return "1.0"
	}
}

// newTestConverter builds a converter with a per-test in-memory
// JSON backend so the cache state never leaks across tests
// (cache.Shared() is process-wide and cached behind sync.Once).
func newTestConverter(t *testing.T, code string) *Converter {
	t.Helper()
	be := cache.NewJSONBackend(t.TempDir())
	return NewWithBackend(be, code)
}

func TestConverterSameCurrencyIsNoop(t *testing.T) {
	c := newTestConverter(t, "USD")
	got, target, ok := c.Convert(context.Background(), 100, "USD")
	if got != 100 || target != "USD" || !ok {
		t.Errorf("same-currency conversion: got (%v, %q, %v), want (100, USD, true)", got, target, ok)
	}
}

func TestConverterEmptyDisplayPassesThrough(t *testing.T) {
	c := newTestConverter(t, "")
	got, target, ok := c.Convert(context.Background(), 42, "USD")
	if got != 42 || target != "USD" || ok {
		t.Errorf("empty display: got (%v, %q, %v), want (42, USD, false)", got, target, ok)
	}
}

func TestConverterNilSafe(t *testing.T) {
	var c *Converter
	got, target, ok := c.Convert(context.Background(), 7, "GBP")
	if got != 7 || target != "GBP" || ok {
		t.Errorf("nil converter: got (%v, %q, %v), want (7, GBP, false)", got, target, ok)
	}
}

func TestConverterRoundTrip(t *testing.T) {
	stubFrankfurter(t, "USD", map[string]float64{
		"GBP": 0.79,
		"EUR": 0.85,
	})
	c := newTestConverter(t, "GBP")
	got, target, ok := c.Convert(context.Background(), 100, "USD")
	if !ok {
		t.Fatal("conversion should succeed when stub server returns rates")
	}
	if target != "GBP" {
		t.Errorf("target = %q, want GBP", target)
	}
	if got < 78 || got > 80 {
		t.Errorf("converted = %v, want ~79", got)
	}
}

func TestConverterUnknownTargetPassesThrough(t *testing.T) {
	stubFrankfurter(t, "USD", map[string]float64{
		"GBP": 0.79,
	})
	c := newTestConverter(t, "ZZZ") // not a real currency
	got, target, ok := c.Convert(context.Background(), 100, "USD")
	if ok {
		t.Error("conversion should fail for unsupported target")
	}
	if got != 100 || target != "USD" {
		t.Errorf("unsupported target: got (%v, %q), want (100, USD)", got, target)
	}
}

func TestConverterSetDisplay(t *testing.T) {
	c := newTestConverter(t, "")
	if c.Display() != "" {
		t.Errorf("initial Display = %q, want empty", c.Display())
	}
	c.SetDisplay("eur")
	if c.Display() != "EUR" {
		t.Errorf("after SetDisplay: Display = %q, want EUR", c.Display())
	}
}

func TestConverterCachesAcrossCalls(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = w.Write([]byte(`{"base":"USD","date":"2026-04-25","rates":{"GBP":0.79}}`))
	}))
	t.Cleanup(srv.Close)
	prev := frankfurterURL
	frankfurterURL = srv.URL + "/latest"
	t.Cleanup(func() { frankfurterURL = prev })

	c := newTestConverter(t, "GBP")
	for i := 0; i < 5; i++ {
		_, _, ok := c.Convert(context.Background(), 10, "USD")
		if !ok {
			t.Fatal("conversion should succeed")
		}
	}
	if hits != 1 {
		t.Errorf("frankfurter called %d times for 5 conversions, want 1 (rest cached)", hits)
	}
}

func TestLatestRatesAddsBaseAsOne(t *testing.T) {
	stubFrankfurter(t, "USD", map[string]float64{"GBP": 0.79})
	rates, err := LatestRates(context.Background(), "USD")
	if err != nil {
		t.Fatal(err)
	}
	if rates["USD"] != 1.0 {
		t.Errorf("base currency missing or != 1.0: %v", rates["USD"])
	}
	if rates["GBP"] != 0.79 {
		t.Errorf("USD→GBP rate: got %v, want 0.79", rates["GBP"])
	}
}
