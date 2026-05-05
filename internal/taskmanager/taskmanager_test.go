package taskmanager

import (
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func newTestTaskManager(t *testing.T) *TaskManager {
	t.Helper()
	logger := slog.New(slog.DiscardHandler)
	tm, err := New(logger)
	require.NoError(t, err)
	require.NotNil(t, tm)
	return tm
}

func TestNew(t *testing.T) {
	t.Parallel()

	tm := newTestTaskManager(t)
	err := tm.Stop()
	require.NoError(t, err)
}

func TestAddTask(t *testing.T) {
	t.Parallel()

	tm := newTestTaskManager(t)
	defer tm.Stop() //nolint:errcheck

	id, err := tm.AddTask("test-task", "@hourly", func() {})
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, id)
}

func TestAddTask_InvalidCron(t *testing.T) {
	t.Parallel()

	tm := newTestTaskManager(t)
	defer tm.Stop() //nolint:errcheck

	_, err := tm.AddTask("bad-task", "not-a-cron", func() {})
	require.Error(t, err)
}

func TestRemoveTask(t *testing.T) {
	t.Parallel()

	tm := newTestTaskManager(t)
	defer tm.Stop() //nolint:errcheck

	id, err := tm.AddTask("test-task", "@hourly", func() {})
	require.NoError(t, err)

	err = tm.RemoveTask(id)
	require.NoError(t, err)

	require.Empty(t, tm.ListTasks())
}

func TestRemoveNonExistentTask(t *testing.T) {
	t.Parallel()

	tm := newTestTaskManager(t)
	defer tm.Stop() //nolint:errcheck

	err := tm.RemoveTask(uuid.New())
	require.Error(t, err)
}

func TestListTasks(t *testing.T) {
	t.Parallel()

	tm := newTestTaskManager(t)
	defer tm.Stop() //nolint:errcheck

	require.Empty(t, tm.ListTasks())

	_, err := tm.AddTask("task1", "@hourly", func() {})
	require.NoError(t, err)

	_, err = tm.AddTask("task2", "@daily", func() {})
	require.NoError(t, err)

	require.Len(t, tm.ListTasks(), 2)
}

func TestRunJob(t *testing.T) {
	t.Parallel()

	tm := newTestTaskManager(t)
	tm.Start()
	defer tm.Stop() //nolint:errcheck

	var ran atomic.Bool
	id, err := tm.AddTask("test-task", "@hourly", func() {
		ran.Store(true)
	})
	require.NoError(t, err)

	err = tm.RunJob(id)
	require.NoError(t, err)

	require.Eventually(t, ran.Load, time.Second, 10*time.Millisecond)
}

func TestRunNonExistentJob(t *testing.T) {
	t.Parallel()

	tm := newTestTaskManager(t)
	defer tm.Stop() //nolint:errcheck

	err := tm.RunJob(uuid.New())
	require.Error(t, err)
	require.ErrorContains(t, err, "could not find task")
}

func TestStartStop(t *testing.T) {
	t.Parallel()

	tm := newTestTaskManager(t)
	tm.Start()

	_, err := tm.AddTask("task1", "@hourly", func() {})
	require.NoError(t, err)

	err = tm.Stop()
	require.NoError(t, err)
}
