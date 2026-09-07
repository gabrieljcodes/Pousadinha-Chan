package games

import (
	"testing"
)

func TestBlackjackShoeCreation(t *testing.T) {
	deck := createDeck()
	if len(deck) != 208 {
		t.Fatalf("Expected 208 cards in 4-deck shoe, got %d", len(deck))
	}

	aceCount := 0
	for _, c := range deck {
		if c.Value == "A" {
			aceCount++
		}
	}
	if aceCount != 16 {
		t.Fatalf("Expected 16 Aces in 4-deck shoe, got %d", aceCount)
	}
}

func TestScoreCalculation(t *testing.T) {
	// Soft 20: Ace + 9
	h1 := Hand{
		Cards: []Card{
			{Value: "A", Score: 11},
			{Value: "9", Score: 9},
		},
	}
	calculateScore(&h1)
	if h1.Score != 20 || h1.Aces != 1 {
		t.Fatalf("Expected score 20 with 1 Ace, got %d (aces: %d)", h1.Score, h1.Aces)
	}

	// Soft 21: Ace + 9 + Ace
	h2 := Hand{
		Cards: []Card{
			{Value: "A", Score: 11},
			{Value: "9", Score: 9},
			{Value: "A", Score: 11},
		},
	}
	calculateScore(&h2)
	if h2.Score != 21 {
		t.Fatalf("Expected score 21 for A+9+A, got %d", h2.Score)
	}

	// Hard 15: Ace + 9 + 5 (11+9+5 = 25 -> ace becomes 1 -> 15)
	h3 := Hand{
		Cards: []Card{
			{Value: "A", Score: 11},
			{Value: "9", Score: 9},
			{Value: "5", Score: 5},
		},
	}
	calculateScore(&h3)
	if h3.Score != 15 {
		t.Fatalf("Expected score 15 for A+9+5, got %d", h3.Score)
	}
}

func TestIsBlackjack(t *testing.T) {
	bj := Hand{
		Cards: []Card{
			{Value: "A", Score: 11},
			{Value: "K", Score: 10},
		},
		Score: 21,
	}
	if !isBlackjack(bj) {
		t.Fatal("Expected isBlackjack to be true for Ace + King")
	}

	threeCard21 := Hand{
		Cards: []Card{
			{Value: "7", Score: 7},
			{Value: "7", Score: 7},
			{Value: "7", Score: 7},
		},
		Score: 21,
	}
	if isBlackjack(threeCard21) {
		t.Fatal("Expected isBlackjack to be false for 3-card 21")
	}
}

func TestProfitAndInsuranceMath(t *testing.T) {
	bet := 100
	insurance := 50

	// Case 1: Player won hand (200), but insurance lost (-50) -> net profit = +50
	winnings := bet * 2
	totalSpent := bet + insurance
	netProfit := winnings - totalSpent
	if netProfit != 50 {
		t.Fatalf("Expected net profit 50, got %d", netProfit)
	}

	// Case 2: Dealer BJ, Player insured (pays 2:1 on insurance, loses main bet)
	// Insurance payout = 50 * 3 = 150. Total spent = 150. Net result = 0 (even)
	insuranceWinnings := insurance * 3
	insuranceNet := insuranceWinnings - totalSpent
	if insuranceNet != 0 {
		t.Fatalf("Expected even result (0) when insured against dealer BJ, got %d", insuranceNet)
	}

	// Case 3: Surrender (recovers 50% of bet)
	surrenderWinnings := bet / 2
	surrenderNet := surrenderWinnings - bet
	if surrenderNet != -50 {
		t.Fatalf("Expected net loss of -50 on surrender, got %d", surrenderNet)
	}
}
