package scheduler

import (
	"stasher/pkg/cron"
	"sync"
	"time"
)

// Job is a single repeating job backed by time.AfterFunc.
// Rescheduling happens after fn returns, so runs never overlap.
type Job struct {
	mu      sync.Mutex
	timer   *time.Timer
	sched   *cron.Cron
	next    time.Time
	stopped bool
	fn      func()
}

// NewJob validates schedule, schedules fn at the next occurrence, and reschedules
// after each run. Returns an error if the schedule expression is invalid.
func NewJob(schedule *cron.Cron, fn func()) (*Job, error) {
	next, err := schedule.NextOccurrence(time.Now())
	if err != nil {
		return nil, err
	}
	j := &Job{sched: schedule, next: next, fn: fn}
	j.timer = time.AfterFunc(time.Until(next), j.run)
	return j, nil
}

// run is called by the timer. It executes fn, then reschedules.
func (j *Job) run() {
	j.mu.Lock()
	if j.stopped {
		j.mu.Unlock()
		return
	}
	j.mu.Unlock()

	j.fn()

	next, err := j.sched.NextOccurrence(time.Now())
	if err != nil {
		return // schedule became invalid; stop silently
	}

	j.mu.Lock()
	defer j.mu.Unlock()
	if !j.stopped {
		j.next = next
		j.timer = time.AfterFunc(time.Until(next), j.run)
	}
}

// Stop cancels any pending timer. Safe to call concurrently.
func (j *Job) Stop() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.stopped = true
	j.timer.Stop()
}

// Next returns the scheduled next-fire time.
func (j *Job) Next() time.Time {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.next
}

// Schedule returns the cron expression the job was created with.
func (j *Job) Schedule() *cron.Cron {
	return j.sched // immutable after construction; no lock needed
}
