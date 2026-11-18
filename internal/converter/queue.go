package converter

import (
	"context"
	"errors"
	"sync"
)

// BatchJob describes one item submitted to RunBatch.
type BatchJob struct {
	ID      string
	Request Request
}

// BatchUpdate notifies the UI or caller of a job's state transition.
type BatchUpdate struct {
	ID       string
	Stage    Stage
	Progress float64
	Detail   string
	Result   *Result
	Err      error
}

// BatchController lets callers cancel individual jobs or the entire batch.
type BatchController struct {
	mu      sync.Mutex
	cancels map[string]context.CancelFunc
	batchFn context.CancelFunc
	done    chan struct{}
}

// CancelJob cancels a single running or queued job by ID.
func (bc *BatchController) CancelJob(id string) {
	if bc == nil {
		return
	}
	bc.mu.Lock()
	fn := bc.cancels[id]
	bc.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// CancelAll cancels all running and queued jobs in the batch.
func (bc *BatchController) CancelAll() {
	if bc == nil {
		return
	}
	if bc.batchFn != nil {
		bc.batchFn()
	}
}

// Wait blocks until all workers finish.
func (bc *BatchController) Wait() {
	if bc != nil && bc.done != nil {
		<-bc.done
	}
}

// StartBatch launches a bounded worker pool to process jobs concurrently and streams updates via onUpdate.
func (p *Pipeline) StartBatch(parentCtx context.Context, jobs []BatchJob, concurrency int, onUpdate func(BatchUpdate)) *BatchController {
	if concurrency < 1 {
		concurrency = 1
	}
	if concurrency > 16 {
		concurrency = 16
	}
	batchCtx, batchCancel := context.WithCancel(parentCtx)
	ctrl := &BatchController{
		cancels: make(map[string]context.CancelFunc, len(jobs)),
		batchFn: batchCancel,
		done:    make(chan struct{}),
	}

	type workItem struct {
		job BatchJob
		ctx context.Context
	}
	ch := make(chan workItem, len(jobs))
	for _, j := range jobs {
		jobCtx, jobCancel := context.WithCancel(batchCtx)
		ctrl.cancels[j.ID] = jobCancel
		if onUpdate != nil {
			onUpdate(BatchUpdate{
				ID:       j.ID,
				Stage:    StageQueued,
				Progress: 0,
			})
		}
		ch <- workItem{job: j, ctx: jobCtx}
	}
	close(ch)

	workers := concurrency
	if len(jobs) < workers {
		workers = len(jobs)
	}
	if workers == 0 {
		close(ctrl.done)
		return ctrl
	}

	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func() {
			defer wg.Done()
			for item := range ch {
				if item.ctx.Err() != nil {
					if onUpdate != nil {
						onUpdate(BatchUpdate{
							ID:    item.job.ID,
							Stage: StageCanceled,
							Err:   ErrCanceled,
						})
					}
					continue
				}
				res, err := p.Convert(item.ctx, item.job.Request, func(ev ProgressEvent) {
					if onUpdate != nil {
						onUpdate(BatchUpdate{
							ID:       item.job.ID,
							Stage:    ev.Stage,
							Progress: ev.Progress,
							Detail:   ev.Detail,
						})
					}
				})
				if err != nil {
					stage := StageFailed
					if errors.Is(err, ErrCanceled) || errors.Is(err, context.Canceled) {
						stage = StageCanceled
						err = ErrCanceled
					}
					if onUpdate != nil {
						onUpdate(BatchUpdate{
							ID:    item.job.ID,
							Stage: stage,
							Err:   err,
						})
					}
					continue
				}
				resCopy := res
				if onUpdate != nil {
					onUpdate(BatchUpdate{
						ID:       item.job.ID,
						Stage:    StageDone,
						Progress: 1.0,
						Result:   &resCopy,
					})
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		batchCancel()
		close(ctrl.done)
	}()

	return ctrl
}
