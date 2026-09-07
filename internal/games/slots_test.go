package games

import (
	"testing"
)

func TestGetWeightedSymbolDistribution(t *testing.T) {
	counts := make(map[string]int)
	totalSpins := 10000

	for i := 0; i < totalSpins; i++ {
		sym := getWeightedSymbol()
		counts[sym.Name]++
	}

	// All 6 symbols should be observed
	for _, s := range slotSymbols {
		if counts[s.Name] == 0 {
			t.Fatalf("Symbol %s was never generated in %d spins", s.Name, totalSpins)
		}
	}

	// Cherries (weight 36) should be far more common than Sevens (weight 2)
	if counts["cherry"] <= counts["seven"] {
		t.Fatalf("Expected cherries (%d) to appear more than sevens (%d)", counts["cherry"], counts["seven"])
	}
}

func TestSpinSlotsMechanics(t *testing.T) {
	bet := 100

	// Run multiple spins and verify state invariants
	for i := 0; i < 1000; i++ {
		res := spinSlots(bet)

		if res.IsJackpot {
			if res.Multiplier < 3.0 {
				t.Fatalf("Jackpot multiplier should be at least 3.0x, got %.1fx", res.Multiplier)
			}
			if res.WinAmount != int(float64(bet)*res.Multiplier) {
				t.Fatalf("WinAmount mismatch on Jackpot: expected %d, got %d", int(float64(bet)*res.Multiplier), res.WinAmount)
			}
			if res.NetProfit != res.WinAmount-bet || res.NetProfit < 0 {
				t.Fatalf("Net profit invalid on Jackpot: %d", res.NetProfit)
			}
		} else if res.IsTwoMatch {
			if res.Multiplier < 1.0 {
				t.Fatalf("Two-match multiplier should never be less than 1.0x, got %.1fx", res.Multiplier)
			}
			if res.IsPush && res.Multiplier != 1.0 {
				t.Fatalf("Push should have exactly 1.0x multiplier, got %.1fx", res.Multiplier)
			}
			if res.WinAmount != int(float64(bet)*res.Multiplier) {
				t.Fatalf("WinAmount mismatch on TwoMatch: expected %d, got %d", int(float64(bet)*res.Multiplier), res.WinAmount)
			}
			if res.NetProfit != res.WinAmount-bet {
				t.Fatalf("Net profit mismatch: expected %d, got %d", res.WinAmount-bet, res.NetProfit)
			}
		} else {
			if res.Multiplier != 0 {
				t.Fatalf("Loss multiplier should be 0, got %.1fx", res.Multiplier)
			}
			if res.WinAmount != 0 {
				t.Fatalf("Loss win amount should be 0, got %d", res.WinAmount)
			}
			if res.NetProfit != -bet {
				t.Fatalf("Loss net profit should be -%d, got %d", bet, res.NetProfit)
			}
		}
	}
}

func TestSimulatedSlotsRTP(t *testing.T) {
	bet := 100
	totalBets := 100000
	totalWinnings := 0

	for i := 0; i < totalBets; i++ {
		res := spinSlots(bet)
		totalWinnings += res.WinAmount
	}

	simulatedRTP := float64(totalWinnings) / float64(totalBets*bet)

	// Theoretical RTP is ~96.7%. Allow statistical tolerance between 92% and 101% for 100k spins.
	if simulatedRTP < 0.92 || simulatedRTP > 1.02 {
		t.Fatalf("Simulated RTP %.4f is out of expected range (0.92 - 1.02)", simulatedRTP)
	}
}
