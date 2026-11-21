package core

import (
	"math"
	"sort"
	"strings"

	"canopy/hub/internal/store/db"
)

// Price is a model's list price in dollars per million tokens.
type Price struct {
	Pattern                              string
	Input, Output, CacheRead, CacheWrite float64
}

// Prices picks a price by the longest pattern found in the model name.
type Prices []Price

func NewPrices(rows []db.ModelPrice) Prices {
	p := make(Prices, 0, len(rows))
	for _, r := range rows {
		p = append(p, Price{Pattern: r.Pattern, Input: r.Input, Output: r.Output, CacheRead: r.CacheRead, CacheWrite: r.CacheWrite})
	}
	sort.Slice(p, func(i, j int) bool { return len(p[i].Pattern) > len(p[j].Pattern) })
	return p
}

// For returns the price for a model, or false when none matches.
func (p Prices) For(model string) (Price, bool) {
	m := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(model, ".", "-"), "_", "-"))
	for _, x := range p {
		if strings.Contains(m, x.Pattern) {
			return x, true
		}
	}
	return Price{}, false
}

// Cost is the price in millionths of a dollar. tokensIn counts fresh input plus cache writes
// (what the runner reports), so cache writes are priced at their own rate and taken out of it.
// An unknown model costs 0.
func (p Prices) Cost(model string, tokensIn, tokensOut, cacheRead, cacheWrite int64) int64 {
	pr, ok := p.For(model)
	if !ok {
		return 0
	}
	fresh := max(tokensIn-cacheWrite, 0)
	// dollars per million tokens equals millionths of a dollar per token
	c := float64(fresh)*pr.Input + float64(tokensOut)*pr.Output + float64(cacheRead)*pr.CacheRead + float64(cacheWrite)*pr.CacheWrite
	return int64(math.Round(c))
}

// ModelUsage is one model's share of a total.
type ModelUsage struct {
	Model      string `json:"model"`
	TokensIn   int64  `json:"tokens_in"`
	TokensOut  int64  `json:"tokens_out"`
	CacheRead  int64  `json:"cache_read"`
	CacheWrite int64  `json:"cache_write"`
	Calls      int64  `json:"calls"`
	CostMicros int64  `json:"cost_micros"`
}

// UsageTotals is what a task or goal has used. Cost is in millionths of a dollar.
type UsageTotals struct {
	TokensIn   int64        `json:"tokens_in"`
	TokensOut  int64        `json:"tokens_out"`
	CacheRead  int64        `json:"cache_read"`
	CacheWrite int64        `json:"cache_write"`
	Calls      int64        `json:"calls"`
	CostMicros int64        `json:"cost_micros"`
	ByModel    []ModelUsage `json:"by_model"`
}

func (u *UsageTotals) add(r db.UsageWork) {
	u.TokensIn += r.TokensIn
	u.TokensOut += r.TokensOut
	u.CacheRead += r.CacheRead
	u.CacheWrite += r.CacheWrite
	u.Calls += r.Calls
	u.CostMicros += r.CostMicros
	for i := range u.ByModel {
		if m := &u.ByModel[i]; m.Model == r.Model {
			m.TokensIn += r.TokensIn
			m.TokensOut += r.TokensOut
			m.CacheRead += r.CacheRead
			m.CacheWrite += r.CacheWrite
			m.Calls += r.Calls
			m.CostMicros += r.CostMicros
			return
		}
	}
	u.ByModel = append(u.ByModel, ModelUsage{Model: r.Model, TokensIn: r.TokensIn, TokensOut: r.TokensOut, CacheRead: r.CacheRead,
		CacheWrite: r.CacheWrite, Calls: r.Calls, CostMicros: r.CostMicros})
}

func newTotals() *UsageTotals { return &UsageTotals{ByModel: []ModelUsage{}} }

// SumUsage totals rows by goal and by task. Work with no goal or task lands under neither.
func SumUsage(rows []db.UsageWork) (goals, tasks map[[16]byte]*UsageTotals) {
	goals, tasks = map[[16]byte]*UsageTotals{}, map[[16]byte]*UsageTotals{}
	for _, r := range rows {
		if r.GoalID != ([16]byte{}) {
			if goals[r.GoalID] == nil {
				goals[r.GoalID] = newTotals()
			}
			goals[r.GoalID].add(r)
		}
		if r.TaskID != ([16]byte{}) {
			if tasks[r.TaskID] == nil {
				tasks[r.TaskID] = newTotals()
			}
			tasks[r.TaskID].add(r)
		}
	}
	return goals, tasks
}
