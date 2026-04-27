package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"stasher/internal/config"
	"stasher/internal/label"
	"stasher/internal/scheduler"
	"stasher/pkg/cron"
	"stasher/pkg/docker"
)

// labelEnabled is the Docker label filter used to discover opt-in containers.
const labelEnabled = "stasher.enabled"

type containerMeta struct {
	name   string
	image  string
	config *label.Container
}

// Manager watches Docker for containers with backup labels and schedules backups
type Manager struct {
	docker *docker.Client
	config *config.Config
	jobs   map[string]map[string]*scheduler.Job // containerID → volID → job
	meta   map[string]containerMeta             // containerID → decoded label state
	mu     sync.Mutex
}

func newManager(config *config.Config) (*Manager, error) {
	cli, err := docker.NewClient()
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	return &Manager{
		docker: cli,
		config: config,
		jobs:   make(map[string]map[string]*scheduler.Job),
		meta:   make(map[string]containerMeta),
	}, nil
}

// Run starts the sync loop. It blocks until ctx is cancelled.
func (m *Manager) Run(ctx context.Context) error {

	// Initial sync so backups are registered before the first tick
	m.sync(ctx)

	ticker := time.NewTicker(m.config.CheckInterval)
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
	containers, err := m.docker.ListContainers(ctx, labelEnabled+"=true")
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
		active[c.Id] = struct{}{}

		cb, err := label.Unmarshal(c.Labels)
		if err != nil {
			slog.Info("unmarshal labels", "container", displayName(c.Names), "err", err)
			continue
		}

		m.meta[c.Id] = containerMeta{name: displayName(c.Names), image: c.Image, config: cb}
		m.reconcileVolumes(c, displayName(c.Names), cb)
	}

	// Unregister all jobs for containers that are gone
	for id := range m.jobs {
		_, ok := active[id]
		if !ok {
			for _, j := range m.jobs[id] {
				j.Stop()
			}
			delete(m.jobs, id)
			delete(m.meta, id)
			slog.Info("container gone", "container", id[:12])
		}
	}
}

// reconcileVolumes registers, updates, or removes jobs so they match
// the volume configuration declared in cb.
func (m *Manager) reconcileVolumes(container docker.Container, name string, cb *label.Container) {
	if m.jobs[container.Id] == nil {
		m.jobs[container.Id] = make(map[string]*scheduler.Job)
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
		j, ok := m.jobs[container.Id][volID]
		if ok {
			if j.Schedule().Equal(schedule) {
				continue
			}
			j.Stop()
		}

		// Capture loop variables before the closure
		vID, cName := volID, name
		j, err = scheduler.NewJob(schedule, func() {
			err := m.backupContainerVolume(context.Background(), container, vID)
			if err != nil {
				slog.Error("stasher error", "container", cName, "volume", vID, "err", err)
			}
		})
		if err != nil {
			slog.Error("invalid schedule", "container", name, "volume", volID, "schedule", vol.Schedule, "err", err)
			continue
		}

		m.jobs[container.Id][volID] = j

		archiveName := name + "-" + volID
		slog.Info("volume registered",
			"container", name,
			"volume", volID,
			"path", vol.Path,
			"archive", archiveName,
			"schedule", vol.Schedule,
			"keep", fmt.Sprintf("days=%d weeks=%d months=%d years=%d", vol.Keep[label.Days], vol.Keep[label.Weeks], vol.Keep[label.Months], vol.Keep[label.Years]),
			"next", j.Next().Format(time.RFC3339),
		)
	}

	// Unregister jobs for volumes that are no longer configured
	for volID, j := range m.jobs[container.Id] {
		_, ok := active[volID]
		if !ok {
			j.Stop()
			delete(m.jobs[container.Id], volID)
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
