package azure

import (
	"context"

	"github.com/tesserix/cloudnav/internal/currency"
)

// fxConvert is the package-local thin wrapper around the
// process-wide currency.ConvertDefault. Kept inside this package
// (rather than importing currency directly in every formatCost
// callsite) so the dependency surface is clear when reading the
// cost code: the formatters know about an FX layer; the wire-format
// parsers don't.
//
// 100ms context budget — every Convert call is either a memory /
// SQLite cache hit (sub-ms) or one HTTP fetch already bounded to
// 5s by the LatestRates timeout. We don't want a slow network to
// stretch the cost column render.
func fxConvert(amount float64, native string) (float64, string, bool) {
	return currency.ConvertDefault(context.Background(), amount, native)
}
