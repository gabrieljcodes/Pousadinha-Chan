package games

import (
	"sync"
	"time"
)

type GameJob struct {
	UserID    string
	Run       func(finishChan chan struct{})
	OnQueue   func(position int)
}

var (
	// Map to track users in active games
	activePlayers = make(map[string]bool)
	playersMu     sync.RWMutex
)

// Enqueue launches a game job concurrently in its own goroutine for the user.
// Unlike the legacy single-worker bottleneck, multiple players can now play simultaneously,
// while strictly ensuring a single user cannot run multiple games at the same time.
func Enqueue(job GameJob) {
	playersMu.Lock()
	if activePlayers[job.UserID] {
		playersMu.Unlock()
		if job.OnQueue != nil {
			job.OnQueue(-1) // Signal that the player already has an active game
		}
		return
	}
	activePlayers[job.UserID] = true
	playersMu.Unlock()

	go func() {
		finishChan := make(chan struct{})
		defer func() {
			playersMu.Lock()
			delete(activePlayers, job.UserID)
			playersMu.Unlock()
		}()

		// Run the game logic in dedicated goroutine
		job.Run(finishChan)

		// Wait until the game signals it is done
		<-finishChan
	}()
}

// IsUserInGame checks if a user is currently in an active game
func IsUserInGame(userID string) bool {
	playersMu.RLock()
	defer playersMu.RUnlock()
	return activePlayers[userID]
}

// RegisterActivePlayer attempts to register a user as being in an active game.
// Returns false if the user is already in an active game.
func RegisterActivePlayer(userID string) bool {
	playersMu.Lock()
	defer playersMu.Unlock()
	if activePlayers[userID] {
		return false
	}
	activePlayers[userID] = true
	return true
}

// UnregisterActivePlayer removes a user from active game tracking.
func UnregisterActivePlayer(userID string) {
	playersMu.Lock()
	defer playersMu.Unlock()
	delete(activePlayers, userID)
}

// WaitForGameFinish waits for the user to finish their current game
func WaitForGameFinish(userID string) {
	for {
		playersMu.RLock()
		inGame := activePlayers[userID]
		playersMu.RUnlock()
		
		if !inGame {
			return
		}
		
		// Wait a bit before checking again
		time.Sleep(500 * time.Millisecond)
	}
}
