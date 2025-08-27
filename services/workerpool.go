package services

import (
	"crawler/models"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

type Job struct {
	ID       string
	Type     string
	URL      string
	Index    int
	ResultCh chan<- JobResult
}

type JobResult struct {
	Index    int
	Response models.ContentResponse
	Error    error
}

type WorkerPool struct {
	maxWorkers  int
	activeJobs  int32
	jobs        chan Job
	semaphore   chan struct{}
	browserPool *BrowserPool
	shutdown    chan struct{}
	wg          sync.WaitGroup
}

var GlobalPool *WorkerPool

func InitializeGlobalPool() {
	maxWorkers := calculateOptimalWorkers()
	GlobalPool = &WorkerPool{
		maxWorkers:  maxWorkers,
		jobs:        make(chan Job, maxWorkers*2),
		semaphore:   make(chan struct{}, maxWorkers),
		browserPool: NewBrowserPool(maxWorkers / 2),
		shutdown:    make(chan struct{}),
	}

	for i := 0; i < maxWorkers; i++ {
		GlobalPool.wg.Add(1)
		go GlobalPool.worker()
	}
}

func calculateOptimalWorkers() int {
	cpuCount := runtime.NumCPU()
	memStats := &runtime.MemStats{}
	runtime.ReadMemStats(memStats)

	cpuBasedLimit := cpuCount * 2
	memoryBasedLimit := int(memStats.Sys / (100 * 1024 * 1024))

	if memoryBasedLimit < 1 {
		memoryBasedLimit = 10
	}

	maxWorkers := cpuBasedLimit
	if memoryBasedLimit < maxWorkers {
		maxWorkers = memoryBasedLimit
	}

	if maxWorkers > 50 {
		maxWorkers = 50
	}
	if maxWorkers < 5 {
		maxWorkers = 5
	}

	return maxWorkers
}

func (wp *WorkerPool) worker() {
	defer wp.wg.Done()
	for {
		select {
		case job := <-wp.jobs:
			wp.processJob(job)
		case <-wp.shutdown:
			return
		}
	}
}

func (wp *WorkerPool) processJob(job Job) {
	atomic.AddInt32(&wp.activeJobs, 1)
	defer atomic.AddInt32(&wp.activeJobs, -1)

	result := JobResult{Index: job.Index}

	switch job.Type {
	case "content":
		result.Response = processURL(job.URL)
	default:
		result.Error = errors.New("unknown job type")
	}

	select {
	case job.ResultCh <- result:
	case <-time.After(5 * time.Second):
	}
}

func (wp *WorkerPool) CanAcceptWork(urlCount int) bool {
	active := atomic.LoadInt32(&wp.activeJobs)
	available := wp.maxWorkers - int(active)
	return available >= urlCount/10 || available >= 2
}

func (wp *WorkerPool) ProcessContentURLs(urls []string) ([]models.ContentResponse, error) {
	if !wp.CanAcceptWork(len(urls)) {
		reduced := wp.maxWorkers / 4
		if reduced < 1 {
			return nil, errors.New("system overloaded")
		}
		return wp.processWithReducedConcurrency(urls, reduced)
	}

	results := make([]models.ContentResponse, len(urls))
	resultCh := make(chan JobResult, len(urls))

	for i, url := range urls {
		job := Job{
			ID:       generateJobID(),
			Type:     "content",
			URL:      url,
			Index:    i,
			ResultCh: resultCh,
		}

		select {
		case wp.jobs <- job:
		case <-time.After(5 * time.Second):
			return nil, errors.New("job submission timeout")
		}
	}

	collected := 0
	timeout := time.After(time.Duration(len(urls)) * 30 * time.Second)

	for collected < len(urls) {
		select {
		case result := <-resultCh:
			results[result.Index] = result.Response
			collected++
		case <-timeout:
			return results, errors.New("processing timeout")
		}
	}

	return results, nil
}

func (wp *WorkerPool) processWithReducedConcurrency(urls []string, limit int) ([]models.ContentResponse, error) {
	results := make([]models.ContentResponse, len(urls))
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup

	for i, url := range urls {
		wg.Add(1)
		go func(index int, url string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			results[index] = processURL(url)
		}(i, url)
	}

	wg.Wait()
	return results, nil
}

func (wp *WorkerPool) Shutdown() {
	close(wp.shutdown)
	wp.wg.Wait()
	wp.browserPool.Shutdown()
}

func generateJobID() string {
	return time.Now().Format("20060102150405") + "-" + generateRandomString(6)
}

func generateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[time.Now().UnixNano()%int64(len(charset))]
	}
	return string(b)
}
