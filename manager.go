package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"stasher/internal/label"
	"stasher/internal/scheduler"
	"stasher/pkg/cron"
	"stasher/pkg/labels"
)

// labelEnabled is the Docker label filter used to discover opt-in containers.
const labelEnabled = "stasher.enabled"

// Manager watches Docker for containers with backup labels and schedules backups
type Manager struct {
	docker *dockerClient
	cfg    Config
	jobs   map[string]map[string]*scheduler.Job // containerID → volID → job
	mu     sync.Mutex
}

func newManager(cfg Config) (*Manager, error) {
	cli, err := newDockerClient()
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	return &Manager{
		docker: cli,
		cfg:    cfg,
		jobs:   make(map[string]map[string]*scheduler.Job),
	}, nil
}

// Run starts the sync loop. It blocks until ctx is cancelled.
func (m *Manager) Run(ctx context.Context) error {

	// Initial sync so backups are registered before the first tick
	m.sync(ctx)

	ticker := time.NewTicker(m.cfg.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			m.sync(ctx)
		}
	}
}

// sync reconciles the set of running jobs with the current labeled containers.
func (m *Manager) sync(ctx context.Context) {
	containers, err := m.docker.ContainerList(ctx, labelEnabled+"=true")
	if err != nil {
		slog.Error("container list", "err", err)
		return
	}

	names := make([]string, len(containers))
	for i, c := range containers {
		names[i] = displayName(c.Names)
	}
	slog.Debug("containers found", "count", len(containers), "containers", names)

	m.mu.Lock()
	defer m.mu.Unlock()

	active := make(map[string]struct{}, len(containers))
	for _, c := range containers {
		active[c.ID] = struct{}{}

		var cb label.Container
		err := labels.Parse(c.Labels, &cb, label.ROOT)
		if err != nil {
			slog.Info("parse labels", "container", displayName(c.Names), "err", err)
			continue
		}

		m.reconcileVolumes(c.ID, displayName(c.Names), cb)
	}

	// Unregister all jobs for containers that are gone
	for id := range m.jobs {
		_, ok := active[id]
		if !ok {
			for _, j := range m.jobs[id] {
				j.Stop()
			}
			delete(m.jobs, id)
			slog.Info("container gone", "container", id[:12])
		}
	}
}

// reconcileVolumes registers, updates, or removes jobs so they match
// the volume configuration declared in cb.
func (m *Manager) reconcileVolumes(containerID, name string, cb label.Container) {
	if m.jobs[containerID] == nil {
		m.jobs[containerID] = make(map[string]*scheduler.Job)
	}

	active := make(map[string]struct{}, len(cb.Volumes))
	for volID, vol := range cb.Volumes {
		active[volID] = struct{}{}

		schedule, err := cron.NewCron(vol.Schedule)
		if err != nil {
			slog.Error("invalid schedule", "container", name, "volume", volID, "schedule", vol.Schedule, "err", err)
			continue
		}

		// Skip if already registered with the same schedule
		j, ok := m.jobs[containerID][volID]
		if ok {
			if j.Schedule().Equal(schedule) {
				continue
			}
			j.Stop()
		}

		// Capture loop variables before the closure
		cID, vID, cName := containerID, volID, name
		j, err = scheduler.NewJob(schedule, func() {
			err := m.backupContainerVolume(context.Background(), cID, vID)
			if err != nil {
				slog.Error("stasher error", "container", cName, "volume", vID, "err", err)
			}
		})
		if err != nil {
			slog.Error("invalid schedule", "container", name, "volume", volID, "schedule", vol.Schedule, "err", err)
			continue
		}

		m.jobs[containerID][volID] = j

		archiveName := name + "-" + volID
		ret := "forever"
		if vol.Retention > 0 {
			ret = vol.Retention.String()
		}
		slog.Info("volume registered",
			"container", name,
			"volume", volID,
			"path", vol.Path,
			"archive", archiveName,
			"schedule", vol.Schedule,
			"retention", ret,
			"next", j.Next().Format(time.RFC3339),
		)
	}

	// Unregister jobs for volumes that are no longer configured
	for volID, j := range m.jobs[containerID] {
		if _, ok := active[volID]; !ok {
			j.Stop()
			delete(m.jobs[containerID], volID)
			slog.Info("volume unregistered", "container", name, "volume", volID)
		}
	}
}

func displayName(names []string) string {
	if len(names) == 0 {
		return "unknown"
	}
	return strings.TrimPrefix(names[0], "/")
}
