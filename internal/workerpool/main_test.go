package workerpool

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"testing"
	"time"

	"go.polysender.org/internal/errorbehavior"
)

func TestQueue(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()
	period := 10 * time.Millisecond
	jobDur := 500 * time.Millisecond
	q := NewQueue(ctx, 100, 4, func(ctx context.Context, workerID int) (interface{}, error) {
		time.Sleep(3 * jobDur)
		log.Printf("[test/worker%d] connecting\n", workerID)
		return nil, nil
	}, func(ctx context.Context, connection interface{}, workerID int) error {
		time.Sleep(3 * jobDur)
		log.Printf("[test/worker%d] disconnecting\n", workerID)
		return nil
	}, log.Default(), log.Default())
	results := make(chan struct{})
	go func() {
		const a = 0.1
		var outputPeriodAvg time.Duration
		lastReceived := time.Now()
		for range results {
			outputPeriod := time.Since(lastReceived)
			lastReceived = time.Now()
			outputPeriodAvg = time.Duration(a*float64(outputPeriod) + (1-a)*float64(outputPeriodAvg))
			log.Println("[test] outputPeriodAvg:", outputPeriodAvg)
		}
	}()
	for i := 0; i < 100000; i++ {
		i := i
		if sleepCtx(ctx, period) {
			break
		}
		log.Printf("[test] enqueueing job%d\n", i)
		q.Enqueue(func(ctx context.Context, connection interface{}, workerID int, attempt int) error {
			log.Printf("[test/worker%d] job%d started\n", workerID, i)
			time.Sleep(jobDur)
			if rand.Float32() > 0.95 {
				log.Printf("[test/worker%d] job %d finished with error\n", workerID, i)
				return errorbehavior.WrapRetryable(fmt.Errorf("job failure"))
			}
			log.Printf("[test/worker%d] job%d finished without error\n", workerID, i)
			results <- struct{}{}
			return nil
		})
	}
	log.Println("[test] enqueued jobs - calling q.StopAndWait()")
	q.StopAndWait()
	log.Println("[test] q.StopAndWait() returned")
	time.Sleep(time.Second)
}

func sleepCtx(ctx context.Context, dur time.Duration) bool {
	ticker := time.NewTicker(dur)
	defer ticker.Stop()
	select {
	case <-ctx.Done():
		return true
	case <-ticker.C:
		return false
	}
}
