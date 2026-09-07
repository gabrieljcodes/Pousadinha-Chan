package games

import (
	"testing"
	"time"
)

func TestCrashPointGeneration(t *testing.T) {
	for i := 0; i < 500; i++ {
		crash := GenerateCrashPoint()
		if crash < 1.00 {
			t.Fatalf("Crash point below 1.00: %f", crash)
		}
		if crash > 100.00 {
			t.Fatalf("Crash point above 100.00: %f", crash)
		}
	}
}

func TestMultiplierCurve(t *testing.T) {
	m0 := CalculateMultiplier(0)
	if m0 != 1.00 {
		t.Fatalf("Expected multiplier at 0s to be 1.00, got %f", m0)
	}

	m10 := CalculateMultiplier(10)
	if m10 < 1.70 || m10 > 1.95 {
		t.Fatalf("Expected multiplier at 10s around 1.82, got %f", m10)
	}

	m30 := CalculateMultiplier(30)
	if m30 < 5.50 || m30 > 6.50 {
		t.Fatalf("Expected multiplier at 30s around 6.05, got %f", m30)
	}
}

func TestProfitCalculation(t *testing.T) {
	bet := 200
	multiplier := 1.75
	totalPayout := int(float64(bet) * multiplier)
	netProfit := totalPayout - bet

	if totalPayout != 350 {
		t.Fatalf("Expected total payout 350, got %d", totalPayout)
	}
	if netProfit != 150 {
		t.Fatalf("Expected net profit 150, got %d", netProfit)
	}
}

func TestConcurrentGameManager(t *testing.T) {
	// Test that two DIFFERENT users can run concurrently
	user1Started := make(chan struct{})
	user2Started := make(chan struct{})

	job1 := GameJob{
		UserID: "user_concurrent_1",
		Run: func(finishChan chan struct{}) {
			close(user1Started)
			time.Sleep(50 * time.Millisecond)
			close(finishChan)
		},
	}

	job2 := GameJob{
		UserID: "user_concurrent_2",
		Run: func(finishChan chan struct{}) {
			close(user2Started)
			time.Sleep(50 * time.Millisecond)
			close(finishChan)
		},
	}

	Enqueue(job1)
	Enqueue(job2)

	// Both should start without waiting for the other
	select {
	case <-user1Started:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("User 1 failed to start concurrently")
	}

	select {
	case <-user2Started:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("User 2 failed to start concurrently")
	}

	// Test that the SAME user cannot start two games at once
	duplicateStarted := false
	duplicateJob := GameJob{
		UserID: "user_concurrent_1",
		OnQueue: func(pos int) {
			if pos == -1 {
				duplicateStarted = false
			}
		},
		Run: func(finishChan chan struct{}) {
			duplicateStarted = true
			close(finishChan)
		},
	}

	Enqueue(duplicateJob)
	if duplicateStarted {
		t.Fatal("Expected duplicate job for user 1 to be rejected while game is running")
	}
}
