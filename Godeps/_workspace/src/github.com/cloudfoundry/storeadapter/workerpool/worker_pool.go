package workerpool

import (
	"sync"
	"time"
)

type WorkerPool struct {
	workQueue          chan func()
	workerCloseChannel chan bool

	workerCount float64
	workerMutex sync.WaitGroup

	metricsLock          sync.Mutex
	timeSpentWorking     time.Duration
	usageSampleStartTime time.Time
}

func NewWorkerPool(poolSize int) (pool *WorkerPool) {
	pool = &WorkerPool{
		workQueue:            make(chan func(), 0),
		workerCloseChannel:   make(chan bool),
		usageSampleStartTime: time.Now(),
		workerCount:          float64(poolSize),
	}

	for i := 0; i < poolSize; i++ {
		pool.workerMutex.Add(1)
		go pool.startWorker(pool.workQueue, pool.workerCloseChannel)
	}

	return
}

func (pool *WorkerPool) ScheduleWork(work func()) {
	select {
	case <-pool.workerCloseChannel:
		return
	default:
		go func() {
			pool.workQueue <- work
		}()
	}
}

func (pool *WorkerPool) StopWorkers() {
	select {
	case <-pool.workerCloseChannel:
		return
	default:
		close(pool.workerCloseChannel)
		pool.workerMutex.Wait()
	}
}

func (pool *WorkerPool) startWorker(workChannel chan func(), closeChannel chan bool) {
	defer pool.workerMutex.Done()
	for {
		select {
		case f := <-workChannel:
			tWork := time.Now()
			f()
			dtWork := time.Since(tWork)
			pool.addTimeSpentWorking(dtWork)
		case <-closeChannel:
			return
		}
	}
}

func (pool *WorkerPool) StartTrackingUsage() {
	pool.resetUsageMetrics()
}

func (pool *WorkerPool) MeasureUsage() (usage float64, timeSinceStartTime time.Duration) {
	timeSpentWorking, timeSinceStartTime := pool.resetUsageMetrics()

	usage = timeSpentWorking / (timeSinceStartTime.Seconds() * pool.workerCount)

	return
}

func (pool *WorkerPool) addTimeSpentWorking(moreTime time.Duration) {
	pool.metricsLock.Lock()
	defer pool.metricsLock.Unlock()

	pool.timeSpentWorking += moreTime
}

func (pool *WorkerPool) resetUsageMetrics() (timeSpentWorking float64, timeSinceStartTime time.Duration) {
	pool.metricsLock.Lock()
	defer pool.metricsLock.Unlock()

	timeSinceStartTime = time.Since(pool.usageSampleStartTime)
	timeSpentWorking = pool.timeSpentWorking.Seconds()

	pool.usageSampleStartTime = time.Now()
	pool.timeSpentWorking = 0
	return
}
