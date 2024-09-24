package taskmanager

import (
	"fmt"
	"log/slog"

	"github.com/go-co-op/gocron/v2"
	"github.com/google/uuid"
)

type TaskManager struct {
	scheduler gocron.Scheduler
}

func New(logger *slog.Logger) (*TaskManager, error) {
	scheduler, err := gocron.NewScheduler(
		gocron.WithLogger(logger),
		gocron.WithGlobalJobOptions(
			gocron.WithSingletonMode(gocron.LimitModeReschedule), // same jobs can not overlap
		),
	)
	if err != nil {
		return nil, err
	}

	return &TaskManager{
		scheduler: scheduler,
	}, nil
}

func (tm *TaskManager) Start() {
	tm.scheduler.Start()
}

func (tm *TaskManager) Stop() error {
	return tm.scheduler.Shutdown()
}

func (tm *TaskManager) AddTask(name, schedule string, task func()) (uuid.UUID, error) {
	j, err := tm.scheduler.NewJob(
		gocron.CronJob(schedule, false),
		gocron.NewTask(task),
		gocron.WithName(name),
	)
	if err != nil {
		return uuid.Nil, err
	}
	return j.ID(), nil
}

func (tm *TaskManager) RemoveTask(id uuid.UUID) error {
	return tm.scheduler.RemoveJob(id)
}

func (tm *TaskManager) RunJob(id uuid.UUID) error {
	jobs := tm.scheduler.Jobs()
	for _, job := range jobs {
		if job.ID() == id {
			return job.RunNow()
		}
	}
	return fmt.Errorf("could not find task %d", id)
}

func (tm *TaskManager) ListTasks() []gocron.Job {
	return tm.scheduler.Jobs()
}
