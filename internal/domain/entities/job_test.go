package entities_test

import (
	"testing"

	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/entities"
	"github.com/atvirokodosprendimai/crawlerdb/internal/domain/valueobj"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestConfig() valueobj.CrawlConfig {
	return valueobj.CrawlConfig{
		Scope:      valueobj.ScopeSameDomain,
		MaxDepth:   5,
		Extraction: valueobj.ExtractionStandard,
	}
}

func TestNewJob(t *testing.T) {
	job := entities.NewJob("https://example.com", newTestConfig())
	assert.NotEmpty(t, job.ID)
	assert.Equal(t, "https://example.com", job.SeedURL)
	assert.Equal(t, entities.JobStatusPending, job.Status)
	assert.False(t, job.CreatedAt.IsZero())
}

func TestJob_Start(t *testing.T) {
	job := entities.NewJob("https://example.com", newTestConfig())
	err := job.Start()
	require.NoError(t, err)
	assert.Equal(t, entities.JobStatusRunning, job.Status)
	assert.False(t, job.StartedAt.IsZero())
}

func TestJob_Start_InvalidState(t *testing.T) {
	job := entities.NewJob("https://example.com", newTestConfig())
	require.NoError(t, job.Start())
	require.NoError(t, job.Complete())

	err := job.Start()
	assert.ErrorIs(t, err, entities.ErrInvalidTransition)
}

func TestJob_Pause(t *testing.T) {
	job := entities.NewJob("https://example.com", newTestConfig())
	require.NoError(t, job.Start())

	err := job.Pause()
	require.NoError(t, err)
	assert.Equal(t, entities.JobStatusPaused, job.Status)
}

func TestJob_Pause_OnlyFromRunning(t *testing.T) {
	job := entities.NewJob("https://example.com", newTestConfig())
	err := job.Pause()
	assert.ErrorIs(t, err, entities.ErrInvalidTransition)
}

func TestJob_Resume(t *testing.T) {
	job := entities.NewJob("https://example.com", newTestConfig())
	require.NoError(t, job.Start())
	require.NoError(t, job.Pause())

	err := job.Resume()
	require.NoError(t, err)
	assert.Equal(t, entities.JobStatusRunning, job.Status)
}

func TestJob_Resume_OnlyFromPaused(t *testing.T) {
	job := entities.NewJob("https://example.com", newTestConfig())
	require.NoError(t, job.Start())

	err := job.Resume()
	assert.ErrorIs(t, err, entities.ErrInvalidTransition)
}

func TestJob_Complete(t *testing.T) {
	job := entities.NewJob("https://example.com", newTestConfig())
	require.NoError(t, job.Start())

	err := job.Complete()
	require.NoError(t, err)
	assert.Equal(t, entities.JobStatusCompleted, job.Status)
	assert.False(t, job.FinishedAt.IsZero())
}

func TestJob_Fail(t *testing.T) {
	job := entities.NewJob("https://example.com", newTestConfig())
	require.NoError(t, job.Start())

	err := job.Fail("connection timeout")
	require.NoError(t, err)
	assert.Equal(t, entities.JobStatusFailed, job.Status)
	assert.False(t, job.FinishedAt.IsZero())
}

func TestJob_Stop(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*entities.Job) error
	}{
		{"from running", func(j *entities.Job) error { return j.Start() }},
		{"from paused", func(j *entities.Job) error {
			if err := j.Start(); err != nil {
				return err
			}
			return j.Pause()
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := entities.NewJob("https://example.com", newTestConfig())
			require.NoError(t, tt.setup(job))

			err := job.Stop()
			require.NoError(t, err)
			assert.Equal(t, entities.JobStatusStopped, job.Status)
		})
	}
}

func TestJob_Stop_FromPending(t *testing.T) {
	job := entities.NewJob("https://example.com", newTestConfig())
	err := job.Stop()
	assert.ErrorIs(t, err, entities.ErrInvalidTransition)
}

func TestJob_TerminalStates(t *testing.T) {
	terminal := []entities.JobStatus{
		entities.JobStatusCompleted,
		entities.JobStatusFailed,
		entities.JobStatusStopped,
	}
	for _, status := range terminal {
		t.Run(string(status), func(t *testing.T) {
			job := entities.NewJob("https://example.com", newTestConfig())
			require.NoError(t, job.Start())
			job.Status = status // force terminal state

			assert.ErrorIs(t, job.Start(), entities.ErrInvalidTransition)
			assert.ErrorIs(t, job.Pause(), entities.ErrInvalidTransition)
			assert.ErrorIs(t, job.Stop(), entities.ErrInvalidTransition)
			assert.ErrorIs(t, job.Complete(), entities.ErrInvalidTransition)
		})
	}
}
