// Generic worker pool with limited concurrency, backpressure, and dynamically resizable number of workers.
package workerpool

import (
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"go.polysender.org/internal/errorbehavior"
)

type jobActionFunc = func(workerID int, attempt int, connection interface{}) error

type job struct {
	action  jobActionFunc
	id      int
	attempt int
}

type Pool struct {
	maxActiveWorkers int
	retries          int
	retryDelay       time.Duration
	idleTimeout      time.Duration
	loggerInfo       *log.Logger
	loggerDebug      *log.Logger
	workerInit       func(workerID int) (interface{}, error)
	workerDeinit     func(workerID int, connection interface{}) error
	concurrency      int32
	jobsNew          chan jobActionFunc
	jobsQueue        chan job
	wgJobs           sync.WaitGroup
	wgWorkers        sync.WaitGroup
	nJobsProcessing  int32
	jobsDone         chan struct{}
	enableWorker     chan struct{}
	disableWorker    chan struct{}
}

type poolConfig struct {
	retries     int
	retryDelay  time.Duration
	idleTimeout time.Duration
	loggerInfo  *log.Logger
	loggerDebug *log.Logger
}

func Retries(n int) func(c *poolConfig) error {
	return func(c *poolConfig) error {
		c.retries = n
		return nil
	}
}

func RetryDelay(d time.Duration) func(c *poolConfig) error {
	return func(c *poolConfig) error {
		c.retryDelay = d
		return nil
	}
}

func IdleTimeout(d time.Duration) func(c *poolConfig) error {
	return func(c *poolConfig) error {
		c.idleTimeout = d
		return nil
	}
}

func LoggerInfo(l *log.Logger) func(c *poolConfig) error {
	return func(c *poolConfig) error {
		c.loggerInfo = l
		return nil
	}
}

func LoggerDebug(l *log.Logger) func(c *poolConfig) error {
	return func(c *poolConfig) error {
		c.loggerDebug = l
		return nil
	}
}

func NewPoolSimple(maxActiveWorkers int, options ...func(*poolConfig) error) (*Pool, error) {
	return NewPoolWithInit(maxActiveWorkers, nil, nil, options...)
}

func NewPoolWithInit(maxActiveWorkers int, workerInit func(workerID int) (interface{}, error), workerDeinit func(workerID int, connection interface{}) error, options ...func(*poolConfig) error) (*Pool, error) {
	// default configuration
	config := poolConfig{
		retryDelay:  time.Second,
		idleTimeout: 20 * time.Second,
	}
	for _, option := range options {
		err := option(&config)
		if err != nil {
			return nil, fmt.Errorf("config error: %s", err)
		}
	}
	p := Pool{
		retries:          config.retries,
		retryDelay:       config.retryDelay,
		idleTimeout:      config.idleTimeout,
		loggerInfo:       config.loggerInfo,
		loggerDebug:      config.loggerDebug,
		maxActiveWorkers: maxActiveWorkers,
		workerInit:       workerInit,
		workerDeinit:     workerDeinit,
	}
	if p.maxActiveWorkers == 0 {
		return nil, fmt.Errorf("maxActiveWorkers = 0")
	}
	p.jobsNew = make(chan jobActionFunc, 2)
	p.jobsQueue = make(chan job, p.maxActiveWorkers) // size p.maxActiveWorkers in order to avoid deadlock
	p.jobsDone = make(chan struct{}, p.maxActiveWorkers)
	p.enableWorker = make(chan struct{}, 1)
	p.disableWorker = make(chan struct{})
	go p.loop()
	for i := 0; i < p.maxActiveWorkers; i++ {
		w := newWorker(&p, i)
		p.wgWorkers.Add(1)
		go w.loop()
	}
	return &p, nil
}

func (p *Pool) loop() {
	var loadAvg float64 = 1
	var jobID int
	var doneCounter int
	var doneCounterWhenLastDisabledWorker int
	var nJobsInSystem int
	var jobDone bool
	for p.jobsNew != nil || p.jobsDone != nil {
		concurrency := atomic.LoadInt32(&p.concurrency)
		if concurrency > 0 {
			// concurrency > 0 so we can divide
			loadNow := float64(nJobsInSystem) / float64(concurrency)
			const a = 0.1
			loadAvg = a*float64(loadNow) + (1-a)*float64(loadAvg)
			if p.loggerDebug != nil {
				nJobsProcessing := atomic.LoadInt32(&p.nJobsProcessing)
				if jobDone {
					p.loggerDebug.Printf("[workerpool/loop] len(jobsNew)=%d len(jobsQueue)=%d nJobsProcessing=%d nJobsInSystem=%d concurrency=%d loadAvg=%.2f doneCounter=%d\n", len(p.jobsNew), len(p.jobsQueue), nJobsProcessing, nJobsInSystem, concurrency, loadAvg, doneCounter)
				} else {
					p.loggerDebug.Printf("[workerpool/loop] len(jobsNew)=%d len(jobsQueue)=%d nJobsProcessing=%d nJobsInSystem=%d concurrency=%d loadAvg=%.2f jobID=%d\n", len(p.jobsNew), len(p.jobsQueue), nJobsProcessing, nJobsInSystem, concurrency, loadAvg, jobID)
				}
			}
		}
		if concurrency > 0 && jobDone {
			if loadAvg < 0.9*float64(concurrency-1)/float64(concurrency) && doneCounter-doneCounterWhenLastDisabledWorker > 20 {
				// if load is low and we didn't disable a worker recently, disable n workers
				// n = number of workers we should disable
				// find n such that:
				// loadAvg > 0.9*(concurrency-n)/concurrency
				// loadAvg*concurrency/0.9 > concurrency-n
				// n + loadAvg*concurrency/0.9 > concurrency
				// n > concurrency - loadAvg*concurrency/0.9
				n := int(float64(concurrency) - loadAvg*float64(concurrency)/0.9)
				if int(concurrency)-n <= 0 {
					n = int(concurrency) - 1
				}
				if n > 0 {
					if p.loggerDebug != nil {
						p.loggerDebug.Printf("[workerpool/loop] [doneCounter=%d] low load - disabling %v workers", doneCounter, n)
					}
					// try to disable n workers.
					for i := 0; i < n; i++ {
						select {
						case p.disableWorker <- struct{}{}:
						default:
							// no worker is listening to the disabledWorker channel so write will be lost but this is not a problem
							// it means all workers are busy so maybe we shouldn't disable any worker
						}
					}
					doneCounterWhenLastDisabledWorker = doneCounter
				}
			}
		} else if concurrency == 0 {
			loadAvg = 1
		}
		jobDone = false
		// make sure not all workers are disabled while there are jobs
		if concurrency == 0 && nJobsInSystem > 0 {
			if p.loggerDebug != nil {
				p.loggerDebug.Printf("[workerpool/loop] [doneCounter=%d] no active worker. try to enable new worker", doneCounter)
			}
		drainLoop:
			for {
				select {
				case <-p.disableWorker:
				default:
					break drainLoop
				}
			}
			select {
			case p.enableWorker <- struct{}{}:
			default:
			}
		}
		if nJobsInSystem >= p.maxActiveWorkers {
			// if there are p.maxActiveWorkers jobs don't accept new jobs
			// len(p.jobsQueue) = p.maxActiveWorkers
			// that way we make sure nJobsInSystem < len(p.jobsQueue)
			// so writes to p.jobsQueue don't block
			// blocking writes to p.jobsQueue would cause deadlock
			_, ok := <-p.jobsDone
			if !ok {
				p.jobsDone = nil
				continue
			}
			nJobsInSystem--
			doneCounter++
			jobDone = true
		} else {
			select {
			case action, ok := <-p.jobsNew:
				if !ok {
					p.jobsNew = nil
					continue
				}
				nJobsInSystem++
				p.jobsQueue <- job{action: action, attempt: 0}
				jobID++
			case _, ok := <-p.jobsDone:
				if !ok {
					p.jobsDone = nil
					continue
				}
				nJobsInSystem--
				doneCounter++
				jobDone = true
			}
		}
	}
	close(p.jobsQueue)
	close(p.enableWorker)
	close(p.disableWorker)
	if p.loggerDebug != nil {
		p.loggerDebug.Println("[workerpool/loop] finished")
	}
}

func (p *Pool) Submit(j jobActionFunc) {
	p.wgJobs.Add(1)
	select {
	case p.jobsNew <- j:
	default:
		if p.loggerDebug != nil {
			p.loggerDebug.Println("[workerpool/Submit] blocked. try to enable new worker")
		}
		select {
		case p.enableWorker <- struct{}{}:
		default:
		}
		p.jobsNew <- j
	}
}

func (p *Pool) StopAndWait() {
	close(p.jobsNew)
	if p.loggerDebug != nil {
		p.loggerDebug.Println("[workerpool/StopAndWait] waiting for all jobs to finish")
	}
	p.wgJobs.Wait()
	close(p.jobsDone)
	if p.loggerDebug != nil {
		p.loggerDebug.Println("[workerpool/StopAndWait] waiting for all workers to finish")
	}
	p.wgWorkers.Wait()
	if p.loggerDebug != nil {
		p.loggerDebug.Println("[workerpool/StopAndWait] finished")
	}
}

type worker struct {
	id         int
	pool       *Pool
	connection interface{}
	idleTicker *time.Ticker
}

func newWorker(p *Pool, id int) *worker {
	return &worker{
		id:   id,
		pool: p,
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
			if w.pool.workerDeinit != nil {
				err := w.pool.workerDeinit(w.id, w.connection)
				if err != nil {
					w.pool.loggerInfo.Printf("[workerpool/worker%d] workerDeinit failed: %s\n", w.id, err)
				}
				w.connection = nil
			}
			if w.pool.loggerDebug != nil {
				concurrency := atomic.LoadInt32(&w.pool.concurrency)
				w.pool.loggerDebug.Printf("[workerpool/worker%d] worker disabled - concurrency %d\n", w.id, concurrency)
			}
		}
	}
	defer w.pool.wgWorkers.Done()
	defer deinit()
loop:
	for {
		if !enabled {
			if w.idleTicker != nil {
				w.idleTicker.Stop()
			}
			_, ok := <-w.pool.enableWorker
			if !ok {
				break
			}
			enabled = true
			atomic.AddInt32(&w.pool.concurrency, 1)
			if w.pool.loggerDebug != nil {
				w.pool.loggerDebug.Printf("[workerpool/worker%d] worker enabled\n", w.id)
			}
			if w.pool.workerInit != nil {
				connection, err := w.pool.workerInit(w.id)
				if err != nil {
					w.pool.loggerInfo.Printf("[workerpool/worker%d] workerInit failed: %s\n", w.id, err)
					time.Sleep(w.pool.retryDelay)
					continue
				}
				w.connection = connection
			} else {
				w.connection = nil
			}
			w.idleTicker = time.NewTicker(w.pool.idleTimeout)
		}
		select {
		case <-w.idleTicker.C:
			deinit()
		case _, ok := <-w.pool.disableWorker:
			if !ok {
				break loop
			}
			deinit()
		case j, ok := <-w.pool.jobsQueue:
			if !ok {
				break loop
			}
			atomic.AddInt32(&w.pool.nJobsProcessing, 1)
			err := j.action(w.id, j.attempt, w.connection)
			atomic.AddInt32(&w.pool.nJobsProcessing, -1)
			w.idleTicker.Stop()
			w.idleTicker = time.NewTicker(w.pool.idleTimeout)
			if err != nil && errorbehavior.IsRetryable(err) && j.attempt < w.pool.retries {
				j.attempt++
				w.pool.jobsQueue <- j
			} else {
				w.pool.jobsDone <- struct{}{}
				w.pool.wgJobs.Done()
			}
		}
	}
	if w.pool.loggerDebug != nil {
		w.pool.loggerDebug.Printf("[workerpool/worker%d] finished\n", w.id)
	}
}
