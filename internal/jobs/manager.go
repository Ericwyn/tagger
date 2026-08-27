package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/ericwyn/tagger/internal/domain"
)

type Repository interface {
	CreateJob(context.Context, domain.Job) (domain.Job, error)
	RecoverRunningJobs(context.Context) error
	ClaimJob(context.Context) (domain.Job, bool, error)
	UpdateJob(context.Context, domain.Job) error
	ListJobs(context.Context, int) ([]domain.Job, error)
	Job(context.Context, string) (domain.Job, error)
}

type Progress func(processed, total, succeeded, failed int, detail string) error
type Handler func(context.Context, domain.Job, Progress) error

type Manager struct {
	repo            Repository
	mu              sync.RWMutex
	handlers        map[domain.JobKind]Handler
	wake            chan struct{}
	cancel          context.CancelFunc
	wait            sync.WaitGroup
	now             func() time.Time
	runningMu       sync.Mutex
	running         map[string]context.CancelFunc
	cancelRequested map[string]bool
	eventMu         sync.RWMutex
	subscribers     map[string]map[chan Event]struct{}
	eventSeq        uint64
}

var (
	ErrNeedsReview       = errors.New("job requires review")
	ErrJobNotCancellable = errors.New("job is not cancellable")
	ErrJobNotRetryable   = errors.New("job is not retryable")
)

func New(repo Repository) *Manager {
	return &Manager{
		repo:            repo,
		handlers:        make(map[domain.JobKind]Handler),
		wake:            make(chan struct{}, 1),
		now:             time.Now,
		running:         make(map[string]context.CancelFunc),
		cancelRequested: make(map[string]bool),
		subscribers:     make(map[string]map[chan Event]struct{}),
	}
}

func (m *Manager) Register(kind domain.JobKind, handler Handler) {
	m.mu.Lock()
	m.handlers[kind] = handler
	m.mu.Unlock()
}

func (m *Manager) Start(ctx context.Context) error {
	if m.repo == nil {
		return errors.New("job repository is required")
	}
	if err := m.repo.RecoverRunningJobs(ctx); err != nil {
		return fmt.Errorf("recover jobs: %w", err)
	}
	workerCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.wait.Add(1)
	go m.loop(workerCtx)
	m.signal()
	return nil
}

func (m *Manager) Close() {
	if m.cancel != nil {
		m.cancel()
	}
	m.wait.Wait()
	m.closeSubscribers()
}

func (m *Manager) Enqueue(ctx context.Context, job domain.Job) (domain.Job, error) {
	created, err := m.repo.CreateJob(ctx, job)
	if err == nil {
		m.signal()
		m.publish(created)
	}
	return created, err
}

func (m *Manager) List(ctx context.Context, limit int) ([]domain.Job, error) {
	return m.repo.ListJobs(ctx, limit)
}
func (m *Manager) Get(ctx context.Context, id string) (domain.Job, error) { return m.repo.Job(ctx, id) }

// Update persists an externally completed job transition and publishes the
// same snapshot to subscribers. Workers use this when one durable job (for
// example, a write job) also advances the state of its parent workflow.
func (m *Manager) Update(ctx context.Context, job domain.Job) error {
	if err := m.repo.UpdateJob(ctx, job); err != nil {
		return err
	}
	m.publish(job)
	return nil
}

func (m *Manager) HasActive(ctx context.Context) (bool, error) {
	items, err := m.repo.ListJobs(ctx, 200)
	if err != nil {
		return false, err
	}
	for _, job := range items {
		switch job.State {
		case domain.JobWaiting, domain.JobRunning, domain.JobReview:
			return true, nil
		}
	}
	return false, nil
}

// HasBlockingFileWork is narrower than HasActive. A review job is durable UI
// state, not an executing file mutation, and must not prevent fsnotify changes
// from reaching the library index.
func (m *Manager) HasBlockingFileWork(ctx context.Context) (bool, error) {
	items, err := m.repo.ListJobs(ctx, 200)
	if err != nil {
		return false, err
	}
	for _, job := range items {
		if job.State != domain.JobWaiting && job.State != domain.JobRunning {
			continue
		}
		switch job.Kind {
		case domain.JobScan, domain.JobWrite, domain.JobBatchEdit:
			return true, nil
		}
	}
	return false, nil
}

// Cancel requests cooperative cancellation. Waiting/review jobs can be
// cancelled immediately; running handlers receive a child context cancellation
// and are finalized as cancelled once they return.
func (m *Manager) Cancel(ctx context.Context, id string) (domain.Job, error) {
	job, err := m.repo.Job(ctx, id)
	if err != nil {
		return domain.Job{}, err
	}
	if job.State != domain.JobWaiting && job.State != domain.JobRunning && job.State != domain.JobReview {
		return domain.Job{}, ErrJobNotCancellable
	}
	m.markCancelRequested(id)
	if job.State == domain.JobRunning {
		job.Detail = "已请求取消，正在等待当前步骤结束"
		if err := m.repo.UpdateJob(ctx, job); err != nil {
			return domain.Job{}, err
		}
		m.cancelRunning(id)
		m.publish(job)
		return job, nil
	}
	job.State = domain.JobCancelled
	job.Detail = "任务已取消"
	job.Error = ""
	job.CompletedAt = m.now().UTC()
	if err := m.repo.UpdateJob(ctx, job); err != nil {
		return domain.Job{}, err
	}
	m.clearCancelRequested(id)
	m.publish(job)
	return job, nil
}

// Retry resets a failed/partial/cancelled job to waiting. The caller may
// provide a filtered payload so match/write jobs only retry failed items.
func (m *Manager) Retry(ctx context.Context, id, payload string) (domain.Job, error) {
	job, err := m.repo.Job(ctx, id)
	if err != nil {
		return domain.Job{}, err
	}
	if job.State != domain.JobFailed && job.State != domain.JobPartial && job.State != domain.JobCancelled {
		return domain.Job{}, ErrJobNotRetryable
	}
	if payload != "" {
		job.Payload = payload
	}
	job.Total = retryPayloadTotal(job.Kind, job.Payload, job.Total)
	job.State = domain.JobWaiting
	job.Detail = "等待重试 worker"
	job.Error = ""
	job.Processed = 0
	job.Succeeded = 0
	job.Failed = 0
	job.StartedAt = time.Time{}
	job.CompletedAt = time.Time{}
	m.clearCancelRequested(id)
	if err := m.repo.UpdateJob(ctx, job); err != nil {
		return domain.Job{}, err
	}
	m.signal()
	m.publish(job)
	return job, nil
}

// Subscribe returns a buffered stream of job snapshots. An empty jobID
// subscribes to all jobs. Slow subscribers lose intermediate progress events;
// GET /jobs/:id remains the authoritative recovery snapshot.
func (m *Manager) Subscribe(jobID string) (<-chan Event, func()) {
	channel := make(chan Event, 16)
	m.eventMu.Lock()
	if m.subscribers[jobID] == nil {
		m.subscribers[jobID] = make(map[chan Event]struct{})
	}
	m.subscribers[jobID][channel] = struct{}{}
	m.eventMu.Unlock()
	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			m.eventMu.Lock()
			if subscribers := m.subscribers[jobID]; subscribers != nil {
				if _, found := subscribers[channel]; found {
					delete(subscribers, channel)
					close(channel)
				}
				if len(subscribers) == 0 {
					delete(m.subscribers, jobID)
				}
			}
			m.eventMu.Unlock()
		})
	}
	return channel, unsubscribe
}

func (m *Manager) signal() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *Manager) loop(ctx context.Context) {
	defer m.wait.Done()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		for {
			job, found, err := m.repo.ClaimJob(ctx)
			if err != nil || !found {
				break
			}
			m.publish(job)
			m.execute(ctx, job)
		}
		select {
		case <-ctx.Done():
			return
		case <-m.wake:
		case <-ticker.C:
		}
	}
}

func (m *Manager) execute(ctx context.Context, job domain.Job) {
	m.mu.RLock()
	handler := m.handlers[job.Kind]
	m.mu.RUnlock()
	jobCtx, cancel := context.WithCancel(ctx)
	m.registerRunning(job.ID, cancel)
	defer m.unregisterRunning(job.ID)
	preCancelled := m.isCancelRequested(job.ID) || m.isPersistentlyCancelled(job.ID)
	if preCancelled {
		cancel()
	}
	progress := func(processed, total, succeeded, failed int, detail string) error {
		if err := validateProgress(processed, total, succeeded, failed); err != nil {
			return err
		}
		job.Processed, job.Total, job.Succeeded, job.Failed, job.Detail = processed, total, succeeded, failed, detail
		if err := m.repo.UpdateJob(jobCtx, job); err != nil {
			return err
		}
		m.publish(job)
		return nil
	}
	var err error
	if handler == nil {
		err = fmt.Errorf("no handler registered for job kind %q", job.Kind)
	} else {
		err = handler(jobCtx, job, progress)
	}
	cancelled := preCancelled || m.consumeCancelRequested(job.ID) || m.isPersistentlyCancelled(job.ID)
	if errors.Is(err, context.Canceled) && ctx.Err() != nil && !cancelled {
		job.State, job.Error, job.StartedAt = domain.JobWaiting, "", time.Time{}
		job.Detail = "服务停止，等待下次启动恢复"
		_ = m.repo.UpdateJob(context.Background(), job)
		m.publish(job)
		return
	}
	if cancelled {
		job.State, job.Error = domain.JobCancelled, ""
		job.Detail = "任务已取消"
		job.CompletedAt = m.now().UTC()
		_ = m.repo.UpdateJob(context.Background(), job)
		m.publish(job)
		return
	}
	job.CompletedAt = m.now().UTC()
	if errors.Is(err, ErrNeedsReview) {
		if completionErr := validateCompletedProgress(job); completionErr != nil {
			finishFailedJob(&job, completionErr)
		} else if job.Succeeded == 0 {
			finishFailedJob(&job, errors.New("任务没有可审核的成功项"))
		} else {
			job.State, job.Error = domain.JobReview, ""
		}
		_ = m.repo.UpdateJob(context.Background(), job)
		m.publish(job)
		return
	}
	if err != nil {
		finishFailedJob(&job, err)
	} else if completionErr := validateCompletedProgress(job); completionErr != nil {
		finishFailedJob(&job, completionErr)
	} else if job.Failed > 0 && job.Succeeded > 0 {
		job.State, job.Error = domain.JobPartial, ""
	} else if job.Failed > 0 {
		job.State, job.Error = domain.JobFailed, ""
	} else {
		job.State, job.Error = domain.JobSucceeded, ""
	}
	_ = m.repo.UpdateJob(context.Background(), job)
	m.publish(job)
}

func validateProgress(processed, total, succeeded, failed int) error {
	if processed < 0 || total < 0 || succeeded < 0 || failed < 0 {
		return errors.New("job progress counters cannot be negative")
	}
	if processed > total {
		return fmt.Errorf("job processed count %d exceeds total %d", processed, total)
	}
	if succeeded+failed > processed {
		return fmt.Errorf("job outcome count %d exceeds processed %d", succeeded+failed, processed)
	}
	return nil
}

func validateCompletedProgress(job domain.Job) error {
	if err := validateProgress(job.Processed, job.Total, job.Succeeded, job.Failed); err != nil {
		return err
	}
	if job.Processed != job.Total {
		return fmt.Errorf("任务提前结束：仅处理 %d/%d 项", job.Processed, job.Total)
	}
	if job.Succeeded+job.Failed != job.Processed {
		return fmt.Errorf("任务结果计数不完整：已处理 %d 项，成功 %d 项，失败 %d 项", job.Processed, job.Succeeded, job.Failed)
	}
	return nil
}

func finishFailedJob(job *domain.Job, err error) {
	if job == nil || err == nil {
		return
	}
	job.Error = err.Error()
	if job.Succeeded > 0 {
		job.State = domain.JobPartial
		job.Detail = "任务部分完成：" + err.Error()
		return
	}
	job.State = domain.JobFailed
	job.Detail = "任务失败：" + err.Error()
}

func retryPayloadTotal(kind domain.JobKind, payload string, fallback int) int {
	if payload == "" {
		return fallback
	}
	var counts struct {
		TrackIDs []json.RawMessage `json:"trackIds"`
		Items    []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal([]byte(payload), &counts); err != nil {
		return fallback
	}
	switch kind {
	case domain.JobMatch:
		if len(counts.TrackIDs) > 0 {
			return len(counts.TrackIDs)
		}
	case domain.JobWrite, domain.JobBatchEdit:
		if len(counts.Items) > 0 {
			return len(counts.Items)
		}
	}
	return fallback
}

func (m *Manager) registerRunning(id string, cancel context.CancelFunc) {
	m.runningMu.Lock()
	m.running[id] = cancel
	m.runningMu.Unlock()
}

func (m *Manager) unregisterRunning(id string) {
	m.runningMu.Lock()
	delete(m.running, id)
	delete(m.cancelRequested, id)
	m.runningMu.Unlock()
}

func (m *Manager) cancelRunning(id string) {
	m.runningMu.Lock()
	cancel := m.running[id]
	m.runningMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (m *Manager) markCancelRequested(id string) {
	m.runningMu.Lock()
	m.cancelRequested[id] = true
	m.runningMu.Unlock()
}

func (m *Manager) clearCancelRequested(id string) {
	m.runningMu.Lock()
	delete(m.cancelRequested, id)
	m.runningMu.Unlock()
}

func (m *Manager) isCancelRequested(id string) bool {
	m.runningMu.Lock()
	requested := m.cancelRequested[id]
	m.runningMu.Unlock()
	return requested
}

func (m *Manager) isPersistentlyCancelled(id string) bool {
	job, err := m.repo.Job(context.Background(), id)
	return err == nil && job.State == domain.JobCancelled
}

func (m *Manager) consumeCancelRequested(id string) bool {
	m.runningMu.Lock()
	requested := m.cancelRequested[id]
	delete(m.cancelRequested, id)
	m.runningMu.Unlock()
	return requested
}

func (m *Manager) publish(job domain.Job) {
	m.eventMu.Lock()
	m.eventSeq++
	event := Event{ID: strconv.FormatUint(m.eventSeq, 10), Job: job}
	for _, key := range []string{job.ID, ""} {
		for channel := range m.subscribers[key] {
			select {
			case channel <- event:
			default:
			}
		}
	}
	m.eventMu.Unlock()
}

func (m *Manager) closeSubscribers() {
	m.eventMu.Lock()
	defer m.eventMu.Unlock()
	for jobID, subscribers := range m.subscribers {
		for channel := range subscribers {
			close(channel)
		}
		delete(m.subscribers, jobID)
	}
}
