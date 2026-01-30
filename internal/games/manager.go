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
	
	// Mapa para rastrear usuários em jogos ativos
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
		// Marcar usuário como em jogo
		playersMu.Lock()
		activePlayers[job.UserID] = true
		playersMu.Unlock()
		
		// Create a channel to wait for this specific game to finish
		finishChan := make(chan struct{})
		
		// Run the game logic
		go job.Run(finishChan)

		// Wait here until the game signals it is done
		<-finishChan
		
		// Remover usuário dos jogos ativos
		playersMu.Lock()
		delete(activePlayers, job.UserID)
		playersMu.Unlock()
	}
}

// IsUserInGame verifica se um usuário está em um jogo ativo
func IsUserInGame(userID string) bool {
	playersMu.RLock()
	defer playersMu.RUnlock()
	return activePlayers[userID]
}

// WaitForGameFinish espera o usuário terminar o jogo atual
func WaitForGameFinish(userID string) {
	for {
		playersMu.RLock()
		inGame := activePlayers[userID]
		playersMu.RUnlock()
		
		if !inGame {
			return
		}
		
		// Esperar um pouco antes de verificar novamente
		time.Sleep(500 * time.Millisecond)
	}
}
