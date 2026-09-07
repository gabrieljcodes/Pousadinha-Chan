package games

import (
	"testing"
)

func TestRouletteBetValidation(t *testing.T) {
	// Valid numbers
	if !isValidBet(BetNumber, "0") {
		t.Error("0 should be a valid number bet")
	}
	if !isValidBet(BetNumber, "36") {
		t.Error("36 should be a valid number bet")
	}
	if !isValidBet(BetNumber, "17") {
		t.Error("17 should be a valid number bet")
	}

	// Invalid numbers
	if isValidBet(BetNumber, "-1") {
		t.Error("-1 should be invalid")
	}
	if isValidBet(BetNumber, "37") {
		t.Error("37 should be invalid")
	}
	if isValidBet(BetNumber, "abc") {
		t.Error("abc should be invalid")
	}

	// Colors
	if !isValidBet(BetColor, "red") || !isValidBet(BetColor, "black") {
		t.Error("red and black should be valid colors")
	}
	if isValidBet(BetColor, "green") {
		t.Error("green color bet should be invalid (use number 0 instead)")
	}

	// Even / Odd
	if !isValidBet(BetEvenOdd, "even") || !isValidBet(BetEvenOdd, "odd") {
		t.Error("even and odd should be valid")
	}

	// Halves
	if !isValidBet(BetHalf, "1-18") || !isValidBet(BetHalf, "19-36") {
		t.Error("1-18 and 19-36 should be valid")
	}

	// Dozens
	if !isValidBet(BetDozen, "1st") || !isValidBet(BetDozen, "2nd") || !isValidBet(BetDozen, "3rd") {
		t.Error("1st, 2nd, 3rd should be valid dozens")
	}
}

func TestRoulettePayoutsAndNetProfit(t *testing.T) {
	// Scenario:
	// User1 bets 100 on "red" and 100 on "even" (Total wagered: 200)
	// Result is Red 3 (Odd)
	// User1 wins 100 * 2 = 200 on Red, and loses 100 on Even.
	// Net profit should be exactly 0 (winnings 200 - wagered 200).
	round := &RouletteRound{
		Result: 3,
		Color:  "red",
		Bets: []RouletteBet{
			{UserID: "user1", Username: "Player1", BetType: BetColor, Value: "red", Amount: 100},
			{UserID: "user1", Username: "Player1", BetType: BetEvenOdd, Value: "even", Amount: 100},
			// User2 bets 50 on straight number 3
			{UserID: "user2", Username: "Player2", BetType: BetNumber, Value: "3", Amount: 50},
			// User3 bets 100 on black (loses)
			{UserID: "user3", Username: "Player3", BetType: BetColor, Value: "black", Amount: 100},
		},
	}

	stats := processPayouts(round)

	// User1: Break-even (Net profit = 0)
	if stats["user1"].TotalWagered != 200 {
		t.Fatalf("Expected user1 wagered to be 200, got %d", stats["user1"].TotalWagered)
	}
	if stats["user1"].TotalPayout != 200 {
		t.Fatalf("Expected user1 payout to be 200, got %d", stats["user1"].TotalPayout)
	}
	if stats["user1"].NetProfit != 0 {
		t.Fatalf("Expected user1 net profit to be 0, got %d", stats["user1"].NetProfit)
	}

	// User2: Straight up hit (35:1) -> Payout: 50 + (50 * 35) = 1800. Net profit: +1750
	if stats["user2"].TotalWagered != 50 {
		t.Fatalf("Expected user2 wagered to be 50, got %d", stats["user2"].TotalWagered)
	}
	if stats["user2"].TotalPayout != 1800 {
		t.Fatalf("Expected user2 payout to be 1800, got %d", stats["user2"].TotalPayout)
	}
	if stats["user2"].NetProfit != 1750 {
		t.Fatalf("Expected user2 net profit to be 1750, got %d", stats["user2"].NetProfit)
	}

	// User3: Total loss -> Payout: 0. Net profit: -100
	if stats["user3"].TotalWagered != 100 {
		t.Fatalf("Expected user3 wagered to be 100, got %d", stats["user3"].TotalWagered)
	}
	if stats["user3"].TotalPayout != 0 {
		t.Fatalf("Expected user3 payout to be 0, got %d", stats["user3"].TotalPayout)
	}
	if stats["user3"].NetProfit != -100 {
		t.Fatalf("Expected user3 net profit to be -100, got %d", stats["user3"].NetProfit)
	}
}

func TestRouletteZeroHouseEdge(t *testing.T) {
	// When result is 0 (green), even/odd, red/black, halves, and dozens all lose
	round := &RouletteRound{
		Result: 0,
		Color:  "green",
		Bets: []RouletteBet{
			{UserID: "u1", BetType: BetColor, Value: "red", Amount: 100},
			{UserID: "u2", BetType: BetColor, Value: "black", Amount: 100},
			{UserID: "u3", BetType: BetEvenOdd, Value: "even", Amount: 100},
			{UserID: "u4", BetType: BetHalf, Value: "1-18", Amount: 100},
			{UserID: "u5", BetType: BetDozen, Value: "1st", Amount: 100},
			{UserID: "u6", BetType: BetNumber, Value: "0", Amount: 100}, // Only this one wins!
		},
	}

	stats := processPayouts(round)

	for _, id := range []string{"u1", "u2", "u3", "u4", "u5"} {
		if stats[id].TotalPayout != 0 {
			t.Fatalf("Expected user %s to lose on 0, got payout %d", id, stats[id].TotalPayout)
		}
	}

	if stats["u6"].TotalPayout != 3600 {
		t.Fatalf("Expected number 0 straight up to pay 3600, got %d", stats["u6"].TotalPayout)
	}
}

func TestSpinWheelCryptoDistribution(t *testing.T) {
	seen := make(map[int]bool)
	spins := 10000

	for i := 0; i < spins; i++ {
		res := spinWheelCrypto()
		if res < 0 || res > 36 {
			t.Fatalf("Invalid roulette outcome: %d (expected 0-36)", res)
		}
		seen[res] = true
	}

	for n := 0; n <= 36; n++ {
		if !seen[n] {
			t.Fatalf("Number %d was never drawn in %d spins", n, spins)
		}
	}
}
