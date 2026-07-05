package scheduler

import (
	"context"
	"runtime/debug"
	"sync"
	"time"

	"go.uber.org/zap"
	"rent_game_accs/internal/pkg/monitoring"
)

type Task func(ctx context.Context) error

type Job struct {
	Name     string
	Interval time.Duration
	Handler  Task
}

type Scheduler struct {
	logger *zap.Logger
	jobs   []Job
	wg     sync.WaitGroup
}

func New(log *zap.Logger) *Scheduler {
	return &Scheduler{
		logger: log,
		jobs:   make([]Job, 0),
	}
}

func (s *Scheduler) Register(name string, interval time.Duration, handler Task) {
	s.jobs = append(s.jobs, Job{
		Name:     name,
		Interval: interval,
		Handler:  handler,
	})
}

func (s *Scheduler) Start(ctx context.Context) {
	s.logger.Info("starting background scheduler", zap.Int("jobs_count", len(s.jobs)))
	for _, job := range s.jobs {
		s.wg.Add(1)
		go func(j Job) {
			defer s.wg.Done()
			s.runJob(ctx, j)
		}(job)
	}
}

func (s *Scheduler) Stop() {
	s.logger.Info("waiting for background scheduler jobs to stop...")
	s.wg.Wait()
	s.logger.Info("background scheduler stopped")
}

func (s *Scheduler) runJob(ctx context.Context, job Job) {
	s.logger.Info("scheduler: starting job", zap.String("job", job.Name), zap.Duration("interval", job.Interval))

	ticker := time.NewTicker(job.Interval)
	defer ticker.Stop()

	s.execute(ctx, job)

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("scheduler: stopping job due to context termination", zap.String("job", job.Name))
			return
		case <-ticker.C:
			s.execute(ctx, job)
		}
	}
}

func (s *Scheduler) execute(ctx context.Context, job Job) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("scheduler: job panicked",
				zap.String("job", job.Name),
				zap.Any("panic", r),
				zap.String("stack", string(debug.Stack())),
			)
		}
	}()

	s.logger.Debug("scheduler: executing job", zap.String("job", job.Name))
	start := time.Now()
	err := job.Handler(ctx)
	duration := time.Since(start)

	monitoring.SchedulerJobDuration.WithLabelValues(job.Name).Observe(duration.Seconds())

	if err != nil {
		s.logger.Error("scheduler: job failed",
			zap.String("job", job.Name),
			zap.Error(err),
			zap.Duration("duration", duration),
		)
	} else {
		s.logger.Info("scheduler: job completed successfully",
			zap.String("job", job.Name),
			zap.Duration("duration", duration),
		)
	}
}
