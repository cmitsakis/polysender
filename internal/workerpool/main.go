package workerpool

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"go.polysender.org/internal/errorbehavior"
)

type (
	jobActionFunc    = func(ctx context.Context, connection interface{}, workerID int, attempt int) error
	connectorFunc    = func(ctx context.Context, workerID int) (interface{}, error)
	disconnectorFunc = func(ctx context.Context, connection interface{}, workerID int) error
)

type job struct {
	action  jobActionFunc
	attempt int
}

type Queue struct {
	concurrencyMax  int
	concurrency     uint32
	retries         int
	jobsNew         chan jobActionFunc
	jobsRetry       chan job
	jobsMerged      chan job
	wgJobs          sync.WaitGroup
	wgWorkers       sync.WaitGroup
	nJobsInSystem   uint32
	nJobsProcessing uint32
	jobsDone        chan struct{}
	enableWorker    chan struct{}
	disableWorker   chan struct{}
	connector       connectorFunc
	disconnector    disconnectorFunc
	loggerInfo      *log.Logger
	loggerDebug     *log.Logger
}

func NewQueue(ctx context.Context, concurrencyMax int, retries int, connector connectorFunc, disconnector disconnectorFunc, loggerInfo, loggerDebug *log.Logger) *Queue {
	q := Queue{
		concurrencyMax: concurrencyMax,
		retries:        retries,
		connector:      connector,
		disconnector:   disconnector,
		loggerInfo:     loggerInfo,
		loggerDebug:    loggerDebug,
	}
	q.jobsNew = make(chan jobActionFunc)
	q.jobsRetry = make(chan job)
	q.jobsMerged = make(chan job, concurrencyMax)
	q.jobsDone = make(chan struct{}, concurrencyMax)
	q.enableWorker = make(chan struct{})
	q.disableWorker = make(chan struct{})
	go q.merger()
	for i := 0; i < q.concurrencyMax; i++ {
		w := newWorker(&q, i)
		q.wgWorkers.Add(1)
		go w.loop(ctx)
	}
	return &q
}

func (q *Queue) merger() {
	var loadAvg float64 = 1
	for q.jobsNew != nil || q.jobsRetry != nil {
		nJobsInSystem := atomic.LoadUint32(&q.nJobsInSystem)
		concurrency := atomic.LoadUint32(&q.concurrency)
		if concurrency > 0 {
			// concurrency > 0 so we can divide
			loadNow := float64(nJobsInSystem) / float64(concurrency)
			const a = 0.1
			loadAvg = a*float64(loadNow) + (1-a)*float64(loadAvg)
			if q.loggerDebug != nil {
				nJobsProcessing := atomic.LoadUint32(&q.nJobsProcessing)
				q.loggerDebug.Printf("[workqueue/merger] len(jobsNew)=%d len(jobsRetry)=%d len(jobsMerged)=%d nJobsInSystem=%d nJobsProcessing=%d concurrency=%d loadNow=%.2f loadAvg=%.2f\n", len(q.jobsNew), len(q.jobsRetry), len(q.jobsMerged), nJobsInSystem, int(nJobsProcessing), concurrency, loadNow, loadAvg)
			}
			if loadAvg < 0.8*float64(concurrency-1)/float64(concurrency) {
				if q.loggerDebug != nil {
					q.loggerDebug.Println("[workqueue/merger] low load - disabling a worker")
				}
				select {
				case q.disableWorker <- struct{}{}:
				default:
				}
			}
		} else if concurrency == 0 {
			loadAvg = 1
		}
		// make sure not all workers are disabled while there are jobs
		if concurrency == 0 && nJobsInSystem > 0 {
			if q.loggerDebug != nil {
				q.loggerDebug.Println("[workqueue/merger] no active worker. try to enable new worker")
			}
			select {
			case q.enableWorker <- struct{}{}:
			default:
			}
		}
		if nJobsInSystem >= uint32(q.concurrencyMax) {
			select {
			case j, ok := <-q.jobsRetry:
				if !ok {
					q.jobsRetry = nil
					continue
				}
				q.jobsMerged <- j
			case <-q.jobsDone:
			}
		} else {
			select {
			case action, ok := <-q.jobsNew:
				if !ok {
					q.jobsNew = nil
					continue
				}
				atomic.AddUint32(&q.nJobsInSystem, 1)
				q.jobsMerged <- job{action: action, attempt: 0}
			case j, ok := <-q.jobsRetry:
				if !ok {
					q.jobsRetry = nil
					continue
				}
				q.jobsMerged <- j
			case <-q.jobsDone:
			}
		}
	}
	close(q.jobsMerged)
	close(q.enableWorker)
	close(q.disableWorker)
	if q.loggerDebug != nil {
		q.loggerDebug.Println("[workqueue/merger] finished")
	}
}

func (q *Queue) Enqueue(f jobActionFunc) {
	q.wgJobs.Add(1)
	concurrency := atomic.LoadUint32(&q.concurrency)
	if concurrency == 0 {
		if q.loggerDebug != nil {
			q.loggerDebug.Println("[workqueue/Enqueue] no active worker. try to enable new worker")
		}
		select {
		case q.enableWorker <- struct{}{}:
		default:
		}
	}
	select {
	case q.jobsNew <- f:
	default:
		if q.loggerDebug != nil {
			q.loggerDebug.Println("[workqueue/Enqueue] blocked. try to enable new worker")
		}
		select {
		case q.enableWorker <- struct{}{}:
		default:
		}
		q.jobsNew <- f
	}
}

func (q *Queue) StopAndWait() {
	close(q.jobsNew)
	if q.loggerDebug != nil {
		q.loggerDebug.Println("[workqueue/StopAndWait] waiting for all jobs to finish")
	}
	q.wgJobs.Wait()
	close(q.jobsRetry)
	close(q.jobsDone)
	if q.loggerDebug != nil {
		q.loggerDebug.Println("[workqueue/StopAndWait] waiting for all workers to finish")
	}
	q.wgWorkers.Wait()
	if q.loggerDebug != nil {
		q.loggerDebug.Println("[workqueue/StopAndWait] finished")
	}
}

const idleTimeout = 20 * time.Second

type worker struct {
	id         int
	queue      *Queue
	idleTicker *time.Ticker
	connection interface{}
}

func newWorker(q *Queue, id int) *worker {
	return &worker{
		id:    id,
		queue: q,
	}
}

func (w *worker) loop(ctx context.Context) {
	enabled := false
	disconnect := func() {
		if w.idleTicker != nil {
			w.idleTicker.Stop()
		}
		if enabled {
			enabled = false
			atomic.AddUint32(&w.queue.concurrency, ^uint32(0)) // w.queue.concurrency--
			if w.queue.disconnector != nil {
				err := w.queue.disconnector(ctx, w.connection, w.id)
				if err != nil {
					w.queue.loggerInfo.Printf("[workqueue/worker%d] disconnector failed: %s\n", w.id, err)
				}
				w.connection = nil
			}
			if w.queue.loggerDebug != nil {
				concurrency := atomic.LoadUint32(&w.queue.concurrency)
				w.queue.loggerDebug.Printf("[workqueue/worker%d] worker disabled - concurrency %d\n", w.id, concurrency)
			}
		}
	}
	defer w.queue.wgWorkers.Done()
	defer disconnect()
loop:
	for {
		if !enabled {
			if w.idleTicker != nil {
				w.idleTicker.Stop()
			}
			_, ok := <-w.queue.enableWorker
			if !ok {
				break
			}
			enabled = true
			atomic.AddUint32(&w.queue.concurrency, 1)
			if w.queue.loggerDebug != nil {
				w.queue.loggerDebug.Printf("[workqueue/worker%d] worker enabled\n", w.id)
			}
			if w.queue.connector != nil {
				connection, err := w.queue.connector(ctx, w.id)
				if err != nil {
					w.queue.loggerInfo.Printf("[workqueue/worker%d] connector failed: %s\n", w.id, err)
					time.Sleep(time.Second)
					continue
				}
				w.connection = connection
			}
			w.idleTicker = time.NewTicker(idleTimeout)
		}
		select {
		case <-w.idleTicker.C:
			disconnect()
		case _, ok := <-w.queue.disableWorker:
			if !ok {
				break loop
			}
			disconnect()
		case j, ok := <-w.queue.jobsMerged:
			if !ok {
				break loop
			}
			atomic.AddUint32(&w.queue.nJobsProcessing, 1)
			err := j.action(ctx, w.connection, w.id, j.attempt)
			atomic.AddUint32(&w.queue.nJobsProcessing, ^uint32(0)) // w.queue.nJobsProcessing--
			w.idleTicker.Stop()
			w.idleTicker = time.NewTicker(idleTimeout)
			if err != nil && errorbehavior.IsRetryable(err) && j.attempt < w.queue.retries {
				j.attempt++
				w.queue.jobsRetry <- j
			} else {
				w.queue.jobsDone <- struct{}{}
				w.queue.wgJobs.Done()
				atomic.AddUint32(&w.queue.nJobsInSystem, ^uint32(0)) // w.queue.nJobsInSystem--
			}
		}
	}
	if w.queue.loggerDebug != nil {
		w.queue.loggerDebug.Printf("[workqueue/worker%d] finished\n", w.id)
	}
}
