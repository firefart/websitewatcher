package taskmanager

import (
	"github.com/firefart/websitewatcher/internal/logger"
	"github.com/robfig/cron/v3"
)

type TaskManager struct {
	scheduler *cron.Cron
}

func New(logger logger.Logger) *TaskManager {
	return &TaskManager{
		scheduler: cron.New(cron.WithSeconds()),
	}
}

func (tm *TaskManager) Start() {
	tm.scheduler.Start()
}

func (tm *TaskManager) Stop() {
	tm.scheduler.Stop()
}

func (tm *TaskManager) AddTask(schedule string, task func()) (cron.EntryID, error) {
	return tm.scheduler.AddFunc(schedule, task)
}

func (tm *TaskManager) RemoveTask(id cron.EntryID) {
	tm.scheduler.Remove(id)
}

func (tm *TaskManager) ListTasks() []cron.Entry {
	return tm.scheduler.Entries()
}
