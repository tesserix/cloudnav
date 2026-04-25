package gcp

import (
	"context"

	"github.com/tesserix/cloudnav/internal/currency"
)

// fxConvert wraps currency.ConvertDefault for the GCP cost
// formatters. Same reasoning as the azure-side wrapper — keeps
// the dependency contained at the formatter boundary.
func fxConvert(amount float64, native string) (float64, string, bool) {
	return currency.ConvertDefault(context.Background(), amount, native)
}
