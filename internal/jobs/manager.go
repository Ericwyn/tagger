package jobs

import (
	"context"
	"errors"
	"fmt"
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
	repo     Repository
	mu       sync.RWMutex
	handlers map[domain.JobKind]Handler
	wake     chan struct{}
	cancel   context.CancelFunc
	wait     sync.WaitGroup
	now      func() time.Time
}

func New(repo Repository) *Manager {
	return &Manager{repo: repo, handlers: make(map[domain.JobKind]Handler), wake: make(chan struct{}, 1), now: time.Now}
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
	workerCtx, cancel := context.WithCancel(context.Background())
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
}

func (m *Manager) Enqueue(ctx context.Context, job domain.Job) (domain.Job, error) {
	created, err := m.repo.CreateJob(ctx, job)
	if err == nil {
		m.signal()
	}
	return created, err
}

func (m *Manager) List(ctx context.Context, limit int) ([]domain.Job, error) {
	return m.repo.ListJobs(ctx, limit)
}
func (m *Manager) Get(ctx context.Context, id string) (domain.Job, error) { return m.repo.Job(ctx, id) }

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
	progress := func(processed, total, succeeded, failed int, detail string) error {
		job.Processed, job.Total, job.Succeeded, job.Failed, job.Detail = processed, total, succeeded, failed, detail
		return m.repo.UpdateJob(ctx, job)
	}
	var err error
	if handler == nil {
		err = fmt.Errorf("no handler registered for job kind %q", job.Kind)
	} else {
		err = handler(ctx, job, progress)
	}
	if errors.Is(err, context.Canceled) && ctx.Err() != nil {
		job.State, job.Error, job.StartedAt = domain.JobWaiting, "", time.Time{}
		job.Detail = "服务停止，等待下次启动恢复"
		_ = m.repo.UpdateJob(context.Background(), job)
		return
	}
	job.CompletedAt = m.now().UTC()
	if err != nil {
		job.State, job.Error, job.Failed = domain.JobFailed, err.Error(), max(1, job.Failed)
		job.Detail = "任务失败：" + err.Error()
	} else {
		job.State, job.Error = domain.JobSucceeded, ""
		if job.Total > 0 {
			job.Processed, job.Succeeded = job.Total, job.Total
		}
	}
	_ = m.repo.UpdateJob(context.Background(), job)
}
