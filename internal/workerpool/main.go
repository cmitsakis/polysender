// Copyright (C) 2022 Charalampos Mitsakis (go.mitsakis.org/workerpool)
// Licensed under the Apache License, Version 2.0
// This file has been copied from go.mitsakis.org/workerpool and edited to remove generics and adapt it to this software.

// Worker pool (work queue) library with auto-scaling, and backpressure.
package workerpool

import (
	"context"
	"fmt"
	"log"
	"math"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

type payloadFunc = func(workerID int, attempt int, connection interface{}) error

type Job struct {
	Payload payloadFunc
	ID      int
	Attempt int
}

type Result struct {
	Job   Job
	Error error
}

type Pool struct {
	maxActiveWorkers int
	fixedWorkers     bool
	retries          int
	reinitDelay      time.Duration
	idleTimeout      time.Duration
	targetLoad       float64
	name             string
	loggerInfo       *log.Logger
	loggerDebug      *log.Logger
	workerInit       func(workerID int) (interface{}, error)
	workerDeinit     func(workerID int, connection interface{}) error
	concurrency      int32
	concurrencyIs0   chan struct{}
	jobsNew          chan payloadFunc
	jobsQueue        chan Job
	wgJobs           sync.WaitGroup
	wgWorkers        sync.WaitGroup
	nJobsProcessing  int32
	jobsDone         chan Result
	// If the pool is created by the constructors
	// NewPoolWithResults() or NewPoolWithResultsAndInit(),
	// results are written to this channel,
	// and you must consume from this channel in a loop until it is closed.
	// If the pool is created by the constructors
	// NewPoolSimple() or NewPoolWithInit(),
	// this channel is nil.
	Results          chan Result
	disableWorker    chan struct{}
	monitor          func(s stats)
	cancelWorkers    context.CancelFunc
	loopDone         chan struct{}
	stoppedWorkers   map[int]*worker
	stoppedWorkersMu sync.Mutex
}

// NewPoolSimple creates a new worker pool.
func NewPoolSimple(maxActiveWorkers int, options ...func(*poolConfig) error) (*Pool, error) {
	return NewPoolWithInit(maxActiveWorkers, nil, nil, options...)
}

// NewPoolWithInit creates a new worker pool with workerInit() and workerDeinit() functions.
func NewPoolWithInit(maxActiveWorkers int, workerInit func(workerID int) (interface{}, error), workerDeinit func(workerID int, connection interface{}) error, options ...func(*poolConfig) error) (*Pool, error) {
	return newPool(maxActiveWorkers, workerInit, workerDeinit, false, options...)
}

func newPool(maxActiveWorkers int, workerInit func(workerID int) (interface{}, error), workerDeinit func(workerID int, connection interface{}) error, createResultsChannel bool, options ...func(*poolConfig) error) (*Pool, error) {
	// default configuration
	config := poolConfig{
		setOptions:  make(map[int]struct{}),
		retries:     1,
		reinitDelay: time.Second,
		idleTimeout: 20 * time.Second,
		targetLoad:  0.9,
	}
	for _, option := range options {
		err := option(&config)
		if err != nil {
			return nil, fmt.Errorf("config error: %s", err)
		}
	}
	_, setFixedWorkers := config.setOptions[optionFixedWorkers]
	_, setTargetLoad := config.setOptions[optionTargetLoad]
	if setFixedWorkers && setTargetLoad {
		return nil, fmt.Errorf("options FixedWorkers() and TargetLoad() are incompatible")
	}

	var loggerInfo *log.Logger
	if config.loggerInfo != nil {
		if config.name == "" {
			loggerInfo = config.loggerInfo
		} else {
			loggerInfo = log.New(config.loggerInfo.Writer(), config.loggerInfo.Prefix()+"[pool="+config.name+"] ", config.loggerInfo.Flags()|log.Lmsgprefix)
		}
	}
	var loggerDebug *log.Logger
	if config.loggerDebug != nil {
		if config.name == "" {
			loggerDebug = config.loggerDebug
		} else {
			loggerDebug = log.New(config.loggerDebug.Writer(), config.loggerDebug.Prefix()+"[pool="+config.name+"] ", config.loggerDebug.Flags()|log.Lmsgprefix)
		}
	}

	ctxWorkers, cancelWorkers := context.WithCancel(context.Background())

	p := Pool{
		retries:          config.retries,
		reinitDelay:      config.reinitDelay,
		idleTimeout:      config.idleTimeout,
		targetLoad:       config.targetLoad,
		name:             config.name,
		loggerInfo:       loggerInfo,
		loggerDebug:      loggerDebug,
		monitor:          config.monitor,
		maxActiveWorkers: maxActiveWorkers,
		fixedWorkers:     config.fixedWorkers,
		workerInit:       workerInit,
		workerDeinit:     workerDeinit,
		cancelWorkers:    cancelWorkers,
	}
	if p.maxActiveWorkers <= 0 {
		return nil, fmt.Errorf("maxActiveWorkers <= 0")
	}
	p.jobsNew = make(chan payloadFunc, 2)
	p.jobsQueue = make(chan Job, p.maxActiveWorkers) // size p.maxActiveWorkers in order to avoid deadlock
	p.jobsDone = make(chan Result, p.maxActiveWorkers)
	if createResultsChannel {
		p.Results = make(chan Result, p.maxActiveWorkers)
	}
	p.concurrencyIs0 = make(chan struct{}, 1)
	p.disableWorker = make(chan struct{}, p.maxActiveWorkers)
	p.stoppedWorkers = make(map[int]*worker, p.maxActiveWorkers)
	p.loopDone = make(chan struct{})
	for i := 0; i < p.maxActiveWorkers; i++ {
		w := newWorker(&p, i, ctxWorkers)
		p.stoppedWorkers[i] = w
	}
	go p.loop()
	return &p, nil
}

type stats struct {
	Time          time.Time
	NJobsInSystem int
	Concurrency   int32
	JobID         int
	DoneCounter   int
}

// loop at each iteration:
// - updates stats
// - handles auto-scaling
// - receives a submitted job payload from p.jobsNew OR receive a done job from p.jobsDone
func (p *Pool) loop() {
	var loadAvg float64 = 1
	var lenResultsAVG float64
	var jobID int
	var jobIDWhenLastEnabledWorker int
	var doneCounter int
	var doneCounterWhenLastDisabledWorker int
	var doneCounterWhenDisabledWorkerResultsBlocked int
	var nJobsInSystem int
	var jobDone bool
	var resultsBlocked bool

	// concurrencyThreshold stores the value of p.concurrency at the last time write to the p.Results channel blocked
	concurrencyThreshold := int32(p.maxActiveWorkers)

	// calculate decay factor "a"
	// of the exponentially weighted moving average.
	// first we calculate the "window" (formula found experimentaly),
	// and then "a" using the formula a = 2/(window+1) (commonly used formula for a)
	window := p.maxActiveWorkers / 2
	if window < 5 {
		window = 5
	}
	a := 2 / (float64(window) + 1)

	// window2 is used as the number of jobs that have to pass
	// before we enable or disable workers again
	window2 := window

	// initialize with negative value so we don't wait 'window2' submissions for auto-scaling to run for the 1st time
	jobIDWhenLastEnabledWorker = -window2 / 2
	doneCounterWhenLastDisabledWorker = -window2 / 2

	for p.jobsNew != nil || p.jobsDone != nil {
		// update stats
		lenResultsAVG = a*float64(len(p.Results)) + (1-a)*lenResultsAVG
		concurrency := atomic.LoadInt32(&p.concurrency)
		if concurrency > 0 {
			// concurrency > 0 so we can divide
			loadNow := float64(nJobsInSystem) / float64(concurrency)
			loadAvg = a*loadNow + (1-a)*loadAvg
			if p.loggerDebug != nil {
				nJobsProcessing := atomic.LoadInt32(&p.nJobsProcessing)
				if jobDone {
					p.loggerDebug.Printf("[workerpool/loop] len(jobsNew)=%d len(jobsQueue)=%d lenResultsAVG=%.2f nJobsProcessing=%d nJobsInSystem=%d concurrency=%d loadAvg=%.2f doneCounter=%d\n", len(p.jobsNew), len(p.jobsQueue), lenResultsAVG, nJobsProcessing, nJobsInSystem, concurrency, loadAvg, doneCounter)
				} else {
					p.loggerDebug.Printf("[workerpool/loop] len(jobsNew)=%d len(jobsQueue)=%d lenResultsAVG=%.2f nJobsProcessing=%d nJobsInSystem=%d concurrency=%d loadAvg=%.2f jobID=%d\n", len(p.jobsNew), len(p.jobsQueue), lenResultsAVG, nJobsProcessing, nJobsInSystem, concurrency, loadAvg, jobID)
				}
			}
		} else {
			loadAvg = 1
		}
		if p.monitor != nil {
			p.monitor(stats{
				Time:          time.Now(),
				NJobsInSystem: nJobsInSystem,
				Concurrency:   concurrency,
				JobID:         jobID,
				DoneCounter:   doneCounter,
			})
		}

		if p.fixedWorkers {
			concurrencyDesired := p.maxActiveWorkers
			concurrencyDiff := int32(concurrencyDesired) - concurrency
			if concurrencyDiff > 0 {
				if p.loggerDebug != nil {
					p.loggerDebug.Printf("[workerpool/loop] [jobID=%d] enabling %d new workers", jobID, concurrencyDiff)
				}
				p.enableWorkers(concurrencyDiff)
			}
		} else {
			// if this is not a pool with fixed number of workers, run auto-scaling
			if !jobDone && // if we received a new job in the previous iteration
				loadAvg/p.targetLoad > float64(concurrency+1)/float64(concurrency) && // and load is high
				jobID-jobIDWhenLastEnabledWorker > window2 && // and we haven't enabled a worker recently
				len(p.Results) == 0 { // and there is no backpressure
				// calculate desired concurrency
				// concurrencyDesired/concurrency = loadAvg/p.targetLoad
				concurrencyDesired := float64(concurrency) * loadAvg / p.targetLoad
				// reduce desired concurrency if it exceeds threshold
				concurrencyExcess := concurrencyDesired - float64(concurrencyThreshold)
				if concurrencyExcess > 0 {
					concurrencyDesired = float64(concurrencyThreshold) + 0.3*concurrencyExcess
				}
				concurrencyDiffFloat := concurrencyDesired - float64(concurrency)
				// then we multiply by 1-sqrt(lenResultsAVG/p.maxActiveWorkers) (found experimentally. needs improvement)
				// in order to reduce concurrencyDiff if there is backpressure and len(p.Results) == 0 was temporary
				concurrencyDiffFloat *= 1 - math.Sqrt(lenResultsAVG/float64(p.maxActiveWorkers))
				concurrencyDiff := int32(math.Round(concurrencyDiffFloat))
				if concurrencyDiff > 0 {
					if p.loggerDebug != nil {
						p.loggerDebug.Printf("[workerpool/loop] [jobID=%d] high load - enabling %d new workers", jobID, concurrencyDiff)
					}
					p.enableWorkers(concurrencyDiff)
					jobIDWhenLastEnabledWorker = jobID
				}
			}
			if jobDone && // if a job was done in the previous iteration
				concurrency > 0 &&
				loadAvg/p.targetLoad < float64(concurrency-1)/float64(concurrency) && // and load is low
				doneCounter-doneCounterWhenLastDisabledWorker > window2 { // and we haven't disabled a worker recently
				// calculate desired concurrency
				// concurrencyDesired/concurrency = loadAvg/p.targetLoad
				concurrencyDesired := float64(concurrency) * loadAvg / p.targetLoad
				if int(math.Round(concurrencyDesired)) <= 0 {
					concurrencyDesired = 1
				}
				concurrencyDiff := int32(math.Round(concurrencyDesired)) - concurrency
				if concurrencyDiff < 0 {
					if p.loggerDebug != nil {
						p.loggerDebug.Printf("[workerpool/loop] [doneCounter=%d] low load - disabling %v workers", doneCounter, -concurrencyDiff)
					}
					p.disableWorkers(-concurrencyDiff)
					doneCounterWhenLastDisabledWorker = doneCounter
				}
			}
			if resultsBlocked && // if write to p.Results channel blocked in the previous iteration
				doneCounter-doneCounterWhenDisabledWorkerResultsBlocked > window2 { // and we haven't recently disabled a worker due to p.Results blocking
				concurrencyThreshold = concurrency
				concurrencyDesired := 0.9 * float64(concurrency)
				if int(math.Round(concurrencyDesired)) <= 0 {
					concurrencyDesired = 1
				}
				concurrencyDiff := int32(math.Round(concurrencyDesired)) - concurrency
				if concurrencyDiff < 0 {
					if p.loggerDebug != nil {
						p.loggerDebug.Printf("[workerpool/loop] [doneCounter=%d] write to p.Results blocked. try to disable %d workers\n", doneCounter, -concurrencyDiff)
					}
					p.disableWorkers(-concurrencyDiff)
					doneCounterWhenDisabledWorkerResultsBlocked = doneCounter
				}
			}
			// make sure not all workers are disabled while there are jobs
			if concurrency == 0 && nJobsInSystem > 0 {
				if p.loggerDebug != nil {
					p.loggerDebug.Printf("[workerpool/loop] [doneCounter=%d] no active worker. try to enable new worker", doneCounter)
				}
				p.enableWorkers(1)
			}
		}

		jobDone = false
		resultsBlocked = false
		if nJobsInSystem >= p.maxActiveWorkers {
			// If there are p.maxActiveWorkers jobs in the system, receive a done job from p.jobsDone, but don't accept new jobs.
			// That way we make sure nJobsInSystem < p.maxActiveWorkers
			// thus nJobsInSystem < cap(p.jobsQueue) (because cap(p.jobsQueue) = p.maxActiveWorkers)
			// thus p.jobsQueue cannot exceed it's capacity,
			// so writes to p.jobsQueue don't block.
			// Blocking writes to p.jobsQueue would cause deadlock.
			select {
			case result, ok := <-p.jobsDone:
				if !ok {
					p.jobsDone = nil
					continue
				}
				if p.Results != nil {
					select {
					case p.Results <- result:
					default:
						p.Results <- result
						resultsBlocked = true
					}
				}
				nJobsInSystem--
				doneCounter++
				jobDone = true
			case _, ok := <-p.concurrencyIs0:
				// if a worker signals that concurrency is 0, enable a worker to avoid deadlock
				if !ok {
					p.jobsNew = nil
					continue
				}
				p.enableWorkers(1)
			}
		} else {
			// receive a submitted job payload from p.jobsNew OR receive a done job from p.jobsDone
			select {
			case payload, ok := <-p.jobsNew:
				if !ok {
					p.jobsNew = nil
					continue
				}
				nJobsInSystem++
				p.jobsQueue <- Job{Payload: payload, ID: jobID, Attempt: 0}
				jobID++
			case result, ok := <-p.jobsDone:
				if !ok {
					p.jobsDone = nil
					continue
				}
				if p.Results != nil {
					select {
					case p.Results <- result:
					default:
						p.Results <- result
						resultsBlocked = true
					}
				}
				nJobsInSystem--
				doneCounter++
				jobDone = true
			case _, ok := <-p.concurrencyIs0:
				// if a worker signals that concurrency is 0, enable a worker to avoid deadlock
				if !ok {
					p.jobsNew = nil
					continue
				}
				p.enableWorkers(1)
			}
		}
	}
	close(p.jobsQueue)
	if p.loggerDebug != nil {
		p.loggerDebug.Println("[workerpool/loop] finished")
	}
	p.loopDone <- struct{}{}
}

func (p *Pool) disableWorkers(n int32) {
	// try to disable n workers
	for i := int32(0); i < n; i++ {
		select {
		case p.disableWorker <- struct{}{}:
		default:
		}
	}
}

func (p *Pool) enableWorkers(n int32) {
	// drain p.disableWorker channel
loop:
	for {
		select {
		case <-p.disableWorker:
		default:
			break loop
		}
	}

	p.stoppedWorkersMu.Lock()
	defer p.stoppedWorkersMu.Unlock()

	if len(p.stoppedWorkers) == 0 {
		return
	}

	// copy keys (worker IDs) of map p.stoppedWorkers to slice stoppedWorkerIDs,
	// so we can then choose random worker IDs
	var stoppedWorkerIDs []int
	for workerID := range p.stoppedWorkers {
		stoppedWorkerIDs = append(stoppedWorkerIDs, workerID)
	}

	// choose n random worker IDs to start
	workerIDsToStart := make([]int, 0, n)
	if n == 1 {
		randomWorkerID := stoppedWorkerIDs[rand.Intn(len(stoppedWorkerIDs))]
		workerIDsToStart = append(workerIDsToStart, randomWorkerID)
	} else {
		for i, randomI := range rand.Perm(len(stoppedWorkerIDs)) {
			if int32(i) >= n {
				break
			}
			randomWorkerID := stoppedWorkerIDs[randomI]
			workerIDsToStart = append(workerIDsToStart, randomWorkerID)
		}
	}

	// start workers
	p.wgWorkers.Add(len(workerIDsToStart))
	for _, workerIDToStart := range workerIDsToStart {
		workerToStart, exists := p.stoppedWorkers[workerIDToStart]
		if !exists {
			// unreachable
			panic(fmt.Sprintf("invalid workerIDToStart: %d", workerIDToStart))
		}
		delete(p.stoppedWorkers, workerIDToStart)
		go workerToStart.loop()
	}
}

// Submit adds a new job to the queue.
func (p *Pool) Submit(jobPayload payloadFunc) {
	p.wgJobs.Add(1)
	p.jobsNew <- jobPayload
}

// StopAndWait shuts down the pool.
// Once called no more jobs can be submitted,
// and waits for all enqueued jobs to finish and workers to stop.
func (p *Pool) StopAndWait() {
	close(p.jobsNew)
	if p.loggerDebug != nil {
		p.loggerDebug.Println("[workerpool/StopAndWait] waiting for all jobs to finish")
	}
	p.wgJobs.Wait()
	p.cancelWorkers()
	close(p.jobsDone)
	if p.loggerDebug != nil {
		p.loggerDebug.Println("[workerpool/StopAndWait] waiting for all workers to finish")
	}
	p.wgWorkers.Wait()
	<-p.loopDone
	close(p.disableWorker)
	close(p.concurrencyIs0)
	if p.loggerDebug != nil {
		p.loggerDebug.Println("[workerpool/StopAndWait] finished")
	}
	if p.Results != nil {
		close(p.Results)
	}
	p.stoppedWorkersMu.Lock()
	defer p.stoppedWorkersMu.Unlock()
	p.stoppedWorkers = nil
}

type worker struct {
	id         int
	pool       *Pool
	ctx        context.Context
	connection *interface{}
	idleTicker *time.Ticker
}

func newWorker(p *Pool, id int, ctx context.Context) *worker {
	return &worker{
		id:   id,
		pool: p,
		ctx:  ctx,
	}
}

func (w *worker) loop() {
	enabled := false
	deinit := func() {
		if w.idleTicker != nil {
			w.idleTicker.Stop()
		}
		if enabled {
			enabled = false
			atomic.AddInt32(&w.pool.concurrency, -1)
			if w.pool.workerDeinit != nil && w.connection != nil {
				err := w.pool.workerDeinit(w.id, *w.connection)
				if err != nil {
					if w.pool.loggerInfo != nil {
						w.pool.loggerInfo.Printf("[workerpool/worker%d] workerDeinit failed: %s\n", w.id, err)
					}
				}
				w.connection = nil
			}
			concurrency := atomic.LoadInt32(&w.pool.concurrency)
			if w.pool.loggerDebug != nil {
				w.pool.loggerDebug.Printf("[workerpool/worker%d] worker disabled - concurrency %d\n", w.id, concurrency)
			}
			if concurrency == 0 {
				// if all workers are disabled, the pool loop might get stuck resulting in a deadlock.
				// we send a signal to p.concurrencyIs0 so the pool loop can continue and enable one worker.
				if w.pool.loggerDebug != nil {
					w.pool.loggerDebug.Printf("[workerpool/worker%d] sending to p.concurrencyIs0\n", w.id)
				}
				select {
				case w.pool.concurrencyIs0 <- struct{}{}:
				default:
				}
			}
		}
	}
	defer func() {
		if w.pool.loggerDebug != nil {
			w.pool.loggerDebug.Printf("[workerpool/worker%d] stopped\n", w.id)
		}

		deinit()

		// save this worker to w.pool.stoppedWorkers
		w.pool.stoppedWorkersMu.Lock()
		defer w.pool.stoppedWorkersMu.Unlock()
		if w.pool.stoppedWorkers != nil { // might have been set to nil by pool.StopAndWait()
			w.pool.stoppedWorkers[w.id] = w
		}

		w.pool.wgWorkers.Done()
	}()

	select {
	case <-w.ctx.Done():
		if w.pool.loggerDebug != nil {
			w.pool.loggerDebug.Printf("ctx has been cancelled. worker cannot start")
		}
		return
	default:
	}
	enabled = true
	atomic.AddInt32(&w.pool.concurrency, 1)
	if w.pool.loggerDebug != nil {
		w.pool.loggerDebug.Printf("[workerpool/worker%d] worker enabled\n", w.id)
	}
	if w.pool.workerInit != nil {
		connection, err := w.pool.workerInit(w.id)
		if err != nil {
			if w.pool.loggerInfo != nil {
				w.pool.loggerInfo.Printf("[workerpool/worker%d] workerInit failed: %s\n", w.id, err)
			}
			enabled = false
			atomic.AddInt32(&w.pool.concurrency, -1)
			time.Sleep(w.pool.reinitDelay)
			// this worker failed to start, so enable another worker
			w.pool.enableWorkers(1)
			return
		}
		w.connection = &connection
	} else {
		w.connection = nil
	}
	if !w.pool.fixedWorkers {
		w.idleTicker = time.NewTicker(w.pool.idleTimeout)
	} else {
		neverTickingTicker := time.Ticker{C: make(chan time.Time)}
		w.idleTicker = &neverTickingTicker
	}

	for {
		select {
		case <-w.idleTicker.C:
			return
		case <-w.ctx.Done():
			return
		case _, ok := <-w.pool.disableWorker:
			if !ok {
				return
			}
			return
		case j, ok := <-w.pool.jobsQueue:
			if !ok {
				return
			}
			if !w.pool.fixedWorkers {
				w.idleTicker.Stop()
			}
			// run job
			atomic.AddInt32(&w.pool.nJobsProcessing, 1)
			var err error
			if w.connection != nil {
				err = j.Payload(w.id, j.Attempt, *w.connection)
			} else {
				err = j.Payload(w.id, j.Attempt, nil)
			}
			atomic.AddInt32(&w.pool.nJobsProcessing, -1)
			if err != nil && ErrorIsRetryable(err) && (j.Attempt < w.pool.retries || errorIsUnaccounted(err)) {
				// if error is retryable, put the job back in queue
				if !errorIsUnaccounted(err) {
					j.Attempt++
				}
				w.pool.jobsQueue <- j
			} else {
				// else job is done
				w.pool.jobsDone <- Result{Job: j, Error: err}
				w.pool.wgJobs.Done()
			}
			// check if worker has to pause due to the error
			if err != nil {
				pauseDuration := errorPausesWorker(err)
				if pauseDuration > 0 {
					deinit()
					// enable another worker so concurrency does not decrease
					w.pool.enableWorkers(1)
					sleepCtx(w.ctx, pauseDuration)
					return
				}
			}
			if !w.pool.fixedWorkers {
				w.idleTicker = time.NewTicker(w.pool.idleTimeout)
			}
		}
	}
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
