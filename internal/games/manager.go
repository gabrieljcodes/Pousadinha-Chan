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
	jobQueue = make(chan GameJob, 100) // Buffer up to 100 games
	queueLen = 0
	queueMu  sync.Mutex
	
	// Map to track users in active games
	activePlayers = make(map[string]bool)
	playersMu     sync.RWMutex
)

func init() {
	go processQueue()
}

func Enqueue(job GameJob) {
	queueMu.Lock()
	currentLen := len(jobQueue)
	queueMu.Unlock()

	// Notify user of their position
	if currentLen > 0 && job.OnQueue != nil {
		job.OnQueue(currentLen)
	}

	jobQueue <- job
}

func processQueue() {
	for job := range jobQueue {
		// Mark user as in-game
		playersMu.Lock()
		activePlayers[job.UserID] = true
		playersMu.Unlock()
		
		// Create a channel to wait for this specific game to finish
		finishChan := make(chan struct{})
		
		// Run the game logic
		go job.Run(finishChan)

		// Wait here until the game signals it is done
		<-finishChan
		
		// Remove user from active games
		playersMu.Lock()
		delete(activePlayers, job.UserID)
		playersMu.Unlock()
	}
}

// IsUserInGame checks if a user is currently in an active game
func IsUserInGame(userID string) bool {
	playersMu.RLock()
	defer playersMu.RUnlock()
	return activePlayers[userID]
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
