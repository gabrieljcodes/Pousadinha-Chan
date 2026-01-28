package games

import "sync"

type GameJob struct {
	UserID    string
	Run       func(finishChan chan struct{})
	OnQueue   func(position int)
}

var (
	jobQueue = make(chan GameJob, 100) // Buffer up to 100 games
	queueLen = 0
	queueMu  sync.Mutex
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
		// Create a channel to wait for this specific game to finish
		finishChan := make(chan struct{})
		
		// Run the game logic
		go job.Run(finishChan)

		// Wait here until the game signals it is done
		<-finishChan
	}
}
