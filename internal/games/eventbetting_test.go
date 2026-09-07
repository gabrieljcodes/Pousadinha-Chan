package games

import (
	"sync"
	"testing"
	"time"
)

func TestOddsCalculationAndDeadlock(t *testing.T) {
	evt := &BettingEvent{
		ID:        "evt_test",
		Question:  "Who wins?",
		Options: []*EventOption{
			{ID: "opt_0", Name: "Alpha", TotalBets: 10, TotalAmount: 1000},
			{ID: "opt_1", Name: "Beta", TotalBets: 2, TotalAmount: 100},
		},
		TotalPool: 1100,
		Status:    "open",
		EndTime:   time.Now().Add(10 * time.Minute),
	}

	odds := evt.GetOdds()
	if odds["opt_0"] < 1.0 {
		t.Fatalf("Expected odds for opt_0 >= 1.0, got %f", odds["opt_0"])
	}
	if odds["opt_1"] <= odds["opt_0"] {
		t.Fatalf("Expected underdog opt_1 to have higher odds than opt_0")
	}

	// Concurrency & Deadlock test: simulate concurrent reads and writes to ToEmbed
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = evt.ToEmbed()
		}()
		go func() {
			defer wg.Done()
			evt.mu.Lock()
			evt.TotalPool += 10
			evt.mu.Unlock()
		}()
	}
	wg.Wait()
}

func TestFairPayoutFormula(t *testing.T) {
	// Simulate: Total pool 10,000. Winning option has 9,800. Losing option has 200.
	totalPool := 10000
	winnerTotal := 9800
	userBet := 9800 // User placed all bets on the winner

	losingPool := totalPool - winnerTotal
	houseProfit := int(float64(losingPool) * HouseEdge)
	distributableBonusPool := losingPool - houseProfit

	userShare := float64(userBet) / float64(winnerTotal)
	bonus := int(userShare * float64(distributableBonusPool))
	winnings := userBet + bonus

	if winnings < userBet {
		t.Fatalf("Winner lost coins! Bet %d, received %d", userBet, winnings)
	}

	if bonus < 0 {
		t.Fatalf("Bonus must be non-negative, got %d", bonus)
	}
}
