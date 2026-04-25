package aws

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/budgets"
	budgetstypes "github.com/aws/aws-sdk-go-v2/service/budgets/types"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	cetypes "github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
)

// Cost Explorer + Budgets SDK clients. CE is a global service (no
// region pinning needed); Budgets too. Same lazy + cached-error
// pattern as the other SDK clients.
var (
	ceOnce    sync.Once
	ceClient  *costexplorer.Client
	ceInitErr error

	budgetsOnce    sync.Once
	budgetsClient  *budgets.Client
	budgetsInitErr error
)

func (a *AWS) ceClient(ctx context.Context) (*costexplorer.Client, error) {
	ceOnce.Do(func() {
		cfg, err := sdkConfig(ctx)
		if err != nil {
			ceInitErr = err
			return
		}
		ceClient = costexplorer.NewFromConfig(cfg)
	})
	return ceClient, ceInitErr
}

func (a *AWS) budgetsClient(ctx context.Context) (*budgets.Client, error) {
	budgetsOnce.Do(func() {
		cfg, err := sdkConfig(ctx)
		if err != nil {
			budgetsInitErr = err
			return
		}
		budgetsClient = budgets.NewFromConfig(cfg)
	})
	return budgetsClient, budgetsInitErr
}

// fetchCostSDK runs a GetCostAndUsage query for a window grouped
// by REGION. Returns (nil, false, err) on SDK auth failure so the
// caller falls back to the CLI.
func (a *AWS) fetchCostSDK(ctx context.Context, from, to time.Time, groupBy string) (map[string]costSample, bool, error) {
	client, err := a.ceClient(ctx)
	if err != nil || client == nil {
		return nil, false, err
	}
	out, err := client.GetCostAndUsage(ctx, &costexplorer.GetCostAndUsageInput{
		TimePeriod: &cetypes.DateInterval{
			Start: aws.String(from.Format("2006-01-02")),
			End:   aws.String(to.Format("2006-01-02")),
		},
		Granularity: cetypes.GranularityMonthly,
		Metrics:     []string{"UnblendedCost"},
		GroupBy: []cetypes.GroupDefinition{{
			Type: cetypes.GroupDefinitionTypeDimension,
			Key:  aws.String(groupBy),
		}},
	})
	if err != nil {
		return nil, true, err
	}
	res := make(map[string]costSample, 16)
	for _, period := range out.ResultsByTime {
		for _, g := range period.Groups {
			if len(g.Keys) == 0 {
				continue
			}
			m, ok := g.Metrics["UnblendedCost"]
			if !ok {
				continue
			}
			amount, perr := strconv.ParseFloat(aws.ToString(m.Amount), 64)
			if perr != nil {
				continue
			}
			res[g.Keys[0]] = costSample{
				amount:   amount,
				currency: aws.ToString(m.Unit),
			}
		}
	}
	return res, true, nil
}

// fetchForecastSDK returns the projected month-end cost via
// GetCostForecast. Used by BillingSummary().
func (a *AWS) fetchForecastSDK(ctx context.Context, from, to time.Time) (float64, string, bool, error) {
	client, err := a.ceClient(ctx)
	if err != nil || client == nil {
		return 0, "", false, err
	}
	out, err := client.GetCostForecast(ctx, &costexplorer.GetCostForecastInput{
		TimePeriod: &cetypes.DateInterval{
			Start: aws.String(from.Format("2006-01-02")),
			End:   aws.String(to.Format("2006-01-02")),
		},
		Granularity: cetypes.GranularityMonthly,
		Metric:      cetypes.MetricUnblendedCost,
	})
	if err != nil {
		return 0, "", true, err
	}
	if out.Total == nil {
		return 0, "", true, nil
	}
	v, perr := strconv.ParseFloat(aws.ToString(out.Total.Amount), 64)
	if perr != nil {
		return 0, "", true, nil
	}
	return v, aws.ToString(out.Total.Unit), true, nil
}

// fetchBudgetSDK returns the largest monthly ceiling configured on
// the account (or 0 if none). Mirrors the CLI parser shape.
func (a *AWS) fetchBudgetSDK(ctx context.Context, accountID string) (float64, string, bool, error) {
	client, err := a.budgetsClient(ctx)
	if err != nil || client == nil {
		return 0, "", false, err
	}
	pager := budgets.NewDescribeBudgetsPaginator(client, &budgets.DescribeBudgetsInput{
		AccountId: aws.String(accountID),
	})
	var (
		maxAmount float64
		currency  string
	)
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return 0, "", true, err
		}
		for _, b := range page.Budgets {
			if b.BudgetLimit == nil {
				continue
			}
			if b.TimeUnit != budgetstypes.TimeUnitMonthly {
				continue
			}
			amt, perr := strconv.ParseFloat(aws.ToString(b.BudgetLimit.Amount), 64)
			if perr != nil {
				continue
			}
			if amt > maxAmount {
				maxAmount = amt
				currency = aws.ToString(b.BudgetLimit.Unit)
			}
		}
	}
	return maxAmount, currency, true, nil
}

// fetchAnomaliesSDK pulls Cost Anomaly Detection results. Used by
// the AWS advisor for the cost-anomaly recommendation rows.
func (a *AWS) fetchAnomaliesSDK(ctx context.Context, from, to time.Time) ([]cetypes.Anomaly, bool, error) {
	client, err := a.ceClient(ctx)
	if err != nil || client == nil {
		return nil, false, err
	}
	pager := costexplorer.NewGetAnomaliesPaginator(client, &costexplorer.GetAnomaliesInput{
		DateInterval: &cetypes.AnomalyDateInterval{
			StartDate: aws.String(from.Format("2006-01-02")),
			EndDate:   aws.String(to.Format("2006-01-02")),
		},
	})
	out := make([]cetypes.Anomaly, 0, 16)
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, true, err
		}
		out = append(out, page.Anomalies...)
	}
	return out, true, nil
}
