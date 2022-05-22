// Copyright (C) 2022 Charalampos Mitsakis (go.mitsakis.org/workerpool)
// Licensed under the Apache License, Version 2.0

package workerpool

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"os"
	"testing"
	"time"
)

var (
	flagDebugLogs           = flag.Bool("debug", false, "Enable debug logs")
	flagSaveTimeseriesToDir = flag.String("save-timeseries-dir", "", "Save concurrency timeseries data to files in the given directory")
)

func TestExample(t *testing.T) {
	p, _ := NewPoolSimple(4, LoggerInfo(loggerIfDebugEnabled()), LoggerDebug(loggerIfDebugEnabled()))
	for i := 0; i < 100; i++ {
		p.Submit(func(workerID int, attempt int, connection interface{}) error {
			result := math.Sqrt(float64(i))
			t.Logf("result: %v", result)
			return nil
		})
	}
	p.StopAndWait()
}

func TestPoolCorrectness(t *testing.T) {
	results := make(chan float64)
	p, err := NewPoolSimple(5, Retries(1), Name("p"), LoggerInfo(loggerIfDebugEnabled()), LoggerDebug(loggerIfDebugEnabled()))
	if err != nil {
		t.Errorf("[ERROR] failed to create pool p: %s", err)
		return
	}

	const submittedCount = 100
	go func() {
		for i := 0; i < submittedCount; i++ {
			i := i
			p.Submit(func(workerID int, attempt int, connection interface{}) error {
				// fail the first attempt only
				if attempt == 0 {
					return ErrorWrapRetryable(fmt.Errorf("failed"))
				}
				results <- float64(i)
				return nil
			})
		}
		p.StopAndWait()
		close(results)
	}()

	var resultsCount int
	seenPayloads := make(map[float64]struct{}, submittedCount)
	for result := range results {
		resultsCount++
		payload := result
		if _, exists := seenPayloads[payload]; exists {
			t.Errorf("[ERROR] duplicate job.Payload=%v", payload)
		}
		seenPayloads[payload] = struct{}{}
	}
	if resultsCount != submittedCount {
		t.Error("[ERROR] submittedCount != resultsCount")
	}
}

// Failure does not mean there is an error, but that the auto-scaling behavior of the pool is not ideal and can be improved.
func TestPoolAutoscalingBehavior(t *testing.T) {
	var logger *log.Logger
	if *flagDebugLogs {
		logger = log.New(os.Stdout, "[DEBUG] [test] ", log.LstdFlags|log.Lmsgprefix)
	} else {
		logger = log.New(io.Discard, "", 0)
	}
	rand.Seed(time.Now().UnixNano())
	workerProfiles := make([]string, 0)
	for i := 0; i < 100; i++ {
		workerProfiles = append(workerProfiles, fmt.Sprintf("w%d", i))
	}
	const inputPeriod = 10 * time.Millisecond
	const jobDur = 500 * time.Millisecond
	const successRate = 0.75
	var pStats []stats
	results := make(chan struct{})
	p, err := NewPoolWithInit(len(workerProfiles), func(workerID int) (interface{}, error) {
		time.Sleep(3 * jobDur)
		if rand.Float32() > 0.9 {
			return struct{}{}, fmt.Errorf("worker init failure")
		}
		logger.Printf("[worker%v] connecting\n", workerID)
		return struct{}{}, nil
	}, func(workerID int, connection interface{}) error {
		time.Sleep(3 * jobDur)
		if rand.Float32() > 0.9 {
			return fmt.Errorf("worker deinit failure")
		}
		logger.Printf("[worker%v] disconnecting\n", workerID)
		return nil
	}, LoggerInfo(loggerIfDebugEnabled()), LoggerDebug(loggerIfDebugEnabled()), monitor(func(s stats) {
		pStats = append(pStats, s)
	}))
	if err != nil {
		t.Errorf("[ERROR] failed to create pool: %s", err)
		return
	}
	started := time.Now()
	var stopped time.Time
	var submittedCount int
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
		defer cancel()
		i := 0
		for ; i < 100000; i++ {
			ctxCanceled := sleepCtx(ctx, inputPeriod)
			if ctxCanceled {
				break
			}
			logger.Printf("submitting job%d\n", i)
			i := i
			p.Submit(func(workerID int, attempt int, connection interface{}) error {
				worker := workerProfiles[workerID]
				logger.Printf("[worker%v] job%d started - attempt %d - worker %v\n", workerID, i, attempt, worker)
				time.Sleep(jobDur)
				if rand.Float32() > successRate {
					return ErrorWrapRetryableUnaccounted(fmt.Errorf("job failure"))
				}
				results <- struct{}{}
				return nil
			})
		}
		logger.Printf("submitted %d jobs - calling p.StopAndWait()\n", i)
		t.Logf("[INFO] submitted %d jobs\n", i)
		submittedCount = i
		stopped = time.Now()
		p.StopAndWait()
		logger.Println("p.StopAndWait() returned")
		close(results)
	}()
	const a = 0.1
	var outputPeriodAVG time.Duration
	lastReceived := time.Now()
	var resultsCount int
	for range results {
		outputPeriod := time.Since(lastReceived)
		lastReceived = time.Now()
		outputPeriodAVG = time.Duration(a*float64(outputPeriod) + (1-a)*float64(outputPeriodAVG))
		logger.Println("outputPeriodAVG:", outputPeriodAVG)
		resultsCount++
	}
	t.Logf("[INFO] got %d results\n", resultsCount)
	if submittedCount != resultsCount {
		t.Error("[ERROR] submittedCount != resultsCount")
	}

	if *flagSaveTimeseriesToDir != "" {
		err := saveConcurrencyStatsToFile(pStats, *flagSaveTimeseriesToDir+"/"+t.Name()+".txt")
		if err != nil {
			t.Logf("saveConcurrencyStatsToFile failed: %v", err)
		}
	}

	pWorkersAVG, pWorkersSD, throughput := processStats(pStats, started.Add(30*time.Second), stopped)
	t.Logf("[INFO] pool workers: AVG=%v SD=%v\n", pWorkersAVG, pWorkersSD)
	// expectedNumOfWorkers = effectiveJobDur/inputPeriod
	// where effectiveJobDur = jobDur / successRate
	// because each job is tried 1/successRate on average
	expectedNumOfWorkers := float64(jobDur/inputPeriod) / successRate
	if pWorkersAVG < 0.95*expectedNumOfWorkers {
		t.Errorf("[WARNING] pWorkersAVG < 0.95*%v", expectedNumOfWorkers)
	}
	if pWorkersAVG > 1.1*expectedNumOfWorkers {
		t.Errorf("[WARNING] pWorkersAVG > 1.1*%v", expectedNumOfWorkers)
	}
	// fail if standard deviation is too high
	if pWorkersSD/pWorkersAVG > 0.1 {
		t.Error("[WARNING] pWorkersSD/pWorkersAVG > 0.1")
	}

	t.Logf("[INFO] throughput: %v\n", throughput)
	// expectedThroughput calculation assumes the inputPeriod is long enough that there is no backpressure
	inputPeriodInSeconds := float64(inputPeriod) / float64(time.Second)
	expectedThroughput := 1 / inputPeriodInSeconds
	if throughput < 0.85*expectedThroughput {
		t.Errorf("[WARNING] throughput < %v", 0.85*expectedThroughput)
	}
	if throughput > 1.1*expectedThroughput {
		t.Errorf("[WARNING] throughput > %v", 1.1*expectedThroughput)
	}

	throughputPerWorker := throughput / pWorkersAVG
	t.Logf("[INFO] throughputPerWorker: %v\n", throughputPerWorker)
	// expectedThroughputPerWorker calculation assumes the inputPeriod is long enough that there is no backpressure
	expectedThroughputPerWorker := expectedThroughput / expectedNumOfWorkers
	if throughputPerWorker < 0.85*expectedThroughputPerWorker {
		t.Errorf("[WARNING] throughputPerWorker < %v", 0.85*expectedThroughputPerWorker)
	}
	if throughputPerWorker > 1.1*expectedThroughputPerWorker {
		t.Errorf("[WARNING] throughputPerWorker > %v", 1.1*expectedThroughputPerWorker)
	}
}

// calculates the average and standard deviation of concurrency in the specified time period
func processStats(statsArray []stats, from time.Time, to time.Time) (float64, float64, float64) {
	var workersSum int
	var workersSumSq int

	// number of elements of statsArray within the specified time period
	var n int

	// time at the first element of statsArray within the specified time period
	var t0 time.Time
	// time at the last element of statsArray within the specified time period
	var t1 time.Time

	// doneCounter at the first element of statsArray within the specified time period
	var doneCounter0 int
	// doneCounter at the last element of statsArray within the specified time period
	var doneCounter1 int

	first := true
	for _, s := range statsArray {
		if s.Time.Before(from) {
			continue
		} else if s.Time.After(to) {
			break
		}
		n++
		if first {
			first = false
			t0 = s.Time
			doneCounter0 = s.DoneCounter
		}
		t1 = s.Time
		doneCounter1 = s.DoneCounter
		workersSum += int(s.Concurrency)
		workersSumSq += int(s.Concurrency * s.Concurrency)
	}
	nFloat := float64(n)
	workersAVG := float64(workersSum) / nFloat
	workersSD := math.Sqrt(float64(workersSumSq)/nFloat - math.Pow(float64(workersSum)/nFloat, 2))
	numOfJobsDone := doneCounter1 - doneCounter0
	dtInSeconds := float64(t1.Sub(t0)) / float64(time.Second)
	throughput := float64(numOfJobsDone) / dtInSeconds
	return workersAVG, workersSD, throughput
}

func saveConcurrencyStatsToFile(statsArray []stats, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("os.Create() failed: %v", err)
	}
	defer f.Close()

	w := bufio.NewWriter(f)

	var t0 time.Time
	first := true
	for _, s := range statsArray {
		if first {
			first = false
			t0 = s.Time
		}
		dt := s.Time.Sub(t0)
		_, err = w.WriteString(fmt.Sprintf("%v %v\n", dt.Microseconds(), s.Concurrency))
		if err != nil {
			return fmt.Errorf("w.WriteString() failed: %v", err)
		}
	}

	err = w.Flush()
	if err != nil {
		return fmt.Errorf("w.Flush() failed: %v", err)
	}
	return nil
}

func loggerIfDebugEnabled() *log.Logger {
	if *flagDebugLogs {
		return log.New(os.Stdout, "[DEBUG] ", log.LstdFlags|log.Lmsgprefix)
	}
	return nil
}
