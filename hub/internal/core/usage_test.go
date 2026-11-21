package core

import (
	"testing"

	"github.com/google/uuid"

	"canopy/hub/internal/store/db"
)

var testPrices = NewPrices([]db.ModelPrice{
	{Pattern: "opus", Input: 4, Output: 20, CacheRead: 0.2, CacheWrite: 5},
	{Pattern: "opus-4", Input: 5, Output: 25, CacheRead: 0.5, CacheWrite: 6.25},
	{Pattern: "sonnet", Input: 2, Output: 10, CacheRead: 0.2, CacheWrite: 2.5},
})

func TestCost(t *testing.T) {
	// 1M fresh + 1M out on sonnet: $2 + $10.
	if got := testPrices.Cost("claude-sonnet-5-5", 1_000_000, 1_000_000, 0, 0); got != 12_000_000 {
		t.Errorf("sonnet in+out = %d micros", got)
	}
	// Cache writes are part of tokensIn but priced at the write rate, not the input rate.
	// 500k fresh ($1) + 500k written ($1.25) + 1M read ($0.20) + 100k out ($1) = $3.45.
	if got := testPrices.Cost("sonnet", 1_000_000, 100_000, 1_000_000, 500_000); got != 3_450_000 {
		t.Errorf("sonnet with cache = %d micros", got)
	}
	// The longest matching pattern wins: opus-4-6 is not priced as plain opus.
	if got := testPrices.Cost("claude-opus-4-6", 1_000_000, 0, 0, 0); got != 5_000_000 {
		t.Errorf("opus-4-6 = %d micros", got)
	}
	if got := testPrices.Cost("opus", 1_000_000, 0, 0, 0); got != 4_000_000 {
		t.Errorf("opus alias = %d micros", got)
	}
	// Unknown models cost nothing; a write larger than the input never goes negative.
	if got := testPrices.Cost("mystery", 1000, 1000, 1000, 0); got != 0 {
		t.Errorf("unknown model = %d micros", got)
	}
	if got := testPrices.Cost("sonnet", 10, 0, 0, 50); got != 125 {
		t.Errorf("write over input = %d micros", got)
	}
}

func TestSumUsage(t *testing.T) {
	g, t1, t2 := uuid.New(), uuid.New(), uuid.New()
	rows := []db.UsageWork{
		{GoalID: g, TaskID: t1, Model: "sonnet", TokensIn: 100, TokensOut: 10, Calls: 2, CostMicros: 50},
		{GoalID: g, TaskID: t1, Model: "opus", TokensIn: 10, TokensOut: 1, Calls: 1, CostMicros: 40},
		{GoalID: g, TaskID: t2, Model: "sonnet", TokensIn: 5, TokensOut: 5, Calls: 1, CostMicros: 7},
		{TaskID: uuid.New(), Model: "sonnet", TokensIn: 1}, // no goal: counts for its task only
	}
	goals, tasks := SumUsage(rows)
	if u := goals[g]; u.TokensIn != 115 || u.CostMicros != 97 || u.Calls != 4 || len(u.ByModel) != 2 {
		t.Errorf("goal totals: %+v", u)
	}
	if u := tasks[t1]; u.TokensIn != 110 || u.CostMicros != 90 || len(u.ByModel) != 2 {
		t.Errorf("task totals: %+v", u)
	}
	if len(goals) != 1 || len(tasks) != 3 {
		t.Errorf("groups: %d goals, %d tasks", len(goals), len(tasks))
	}
}
