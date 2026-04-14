package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"stasher/internal/label"
	"stasher/pkg/labels"
)

// Manifest is written as the first entry ("stasher-manifest.json") of every archive.
type Manifest struct {
	ContainerID   string    `json:"container_id"`
	ContainerName string    `json:"container_name"`
	Timestamp     time.Time `json:"timestamp"`
	Path          string    `json:"path"`
}

// backupContainerVolume backs up a single configured volume for the given container.
// It re-inspects the container at call time to pick up any label changes since registration.
func (m *Manager) backupContainerVolume(ctx context.Context, containerID, volID string) error {
	info, err := m.docker.ContainerInspect(ctx, containerID)
	if err != nil {
		return fmt.Errorf("inspect: %w", err)
	}

	var cb label.Container
	if err := labels.Parse(info.Config.Labels, &cb, label.ROOT); err != nil {
		return fmt.Errorf("parse labels: %w", err)
	}

	vol, ok := cb.Volumes[volID]
	if !ok {
		return fmt.Errorf("volume %q no longer configured", volID)
	}

	containerName := strings.TrimPrefix(info.Name, "/")

	if cb.Options.StopDuringBackup && info.State.Running {
		slog.Info("stopping container for backup", "container", containerName)
		if err := m.docker.ContainerStop(ctx, containerID, 30); err != nil {
			return fmt.Errorf("stop: %w", err)
		}
		defer func() {
			slog.Info("restarting container", "container", containerName)
			if err := m.docker.ContainerStart(ctx, containerID); err != nil {
				slog.Error("restart failed", "container", containerName, "err", err)
			}
		}()
	}

	return m.backupVolume(ctx, containerID, containerName, volID, vol)
}

// backupVolume writes one archive for the given volume configuration.
func (m *Manager) backupVolume(ctx context.Context, containerID, containerName, volID string, vol label.Volume) error {
	archiveName := containerName + "-" + volID

	timestamp := time.Now().UTC().Format("20060102-150405")
	filename := fmt.Sprintf("%s-%s.tar.gz", archiveName, timestamp)
	outPath := filepath.Join(m.cfg.BackupDest, filename)

	slog.Info("writing backup", "container", containerName, "file", filename)
	if err := m.writeArchive(ctx, containerID, outPath, containerName, vol.Path); err != nil {
		os.Remove(outPath)
		return fmt.Errorf("write archive: %w", err)
	}

	slog.Info("stasher complete", "container", containerName, "file", filename)
	m.applyRetention(archiveName, vol.Retention)
	return nil
}

// writeArchive creates the .tar.gz archive at outPath for the given container path.
func (m *Manager) writeArchive(ctx context.Context, containerID, outPath, containerName, path string) (rerr error) {
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer func() {
		if err := f.Close(); err != nil && rerr == nil {
			rerr = err
		}
	}()

	gz := gzip.NewWriter(f)
	defer func() {
		if err := gz.Close(); err != nil && rerr == nil {
			rerr = err
		}
	}()

	tw := tar.NewWriter(gz)
	defer func() {
		if err := tw.Close(); err != nil && rerr == nil {
			rerr = err
		}
	}()

	// Write manifest as the first entry so it can be inspected without
	// extracting the whole archive
	manifest := Manifest{
		ContainerID:   containerID[:12],
		ContainerName: containerName,
		Timestamp:     time.Now().UTC(),
		Path:          path,
	}
	if err := writeManifest(tw, manifest); err != nil {
		return fmt.Errorf("manifest: %w", err)
	}

	rc, err := m.docker.CopyFromContainer(ctx, containerID, path)
	if err != nil {
		return fmt.Errorf("copy %s: %w", path, err)
	}
	defer rc.Close()

	if err := repackTar(tw, rc, path); err != nil {
		return fmt.Errorf("repack %s: %w", path, err)
	}

	return nil
}

// repackTar reads a tar stream from CopyFromContainer and rewrites each
// header name so files appear at their full container path
//
// Docker's CopyFromContainer returns a tar where the root is named after the
// last path component of srcPath:
//
//	srcPath = /var/lib/postgresql/data
//	tar entries: "data/", "data/PG_VERSION", ...
//	→ rewritten: "var/lib/postgresql/data/", "var/lib/postgresql/data/PG_VERSION", ...
func repackTar(tw *tar.Writer, r io.Reader, srcPath string) error {
	prefix := strings.TrimPrefix(srcPath, "/") // e.g. "var/lib/postgresql/data"
	base := filepath.Base(srcPath)             // e.g. "data"

	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		hdr.Name = rewritePath(hdr.Name, base, prefix)
		// Hard link targets are stored as paths inside the archive and must be
		// renamed with the same transform, otherwise tar cannot resolve them
		// on restore
		if hdr.Typeflag == tar.TypeLink {
			hdr.Linkname = rewritePath(hdr.Linkname, base, prefix)
		}

		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		// Copy file data, symlinks, hard links, and directories have no body
		if hdr.Typeflag != tar.TypeSymlink &&
			hdr.Typeflag != tar.TypeLink &&
			hdr.Typeflag != tar.TypeDir &&
			hdr.Size > 0 {
			if _, err := io.Copy(tw, tr); err != nil {
				return err
			}
		}
	}
}

// rewritePath renames a tar entry from the Docker-relative form ("data/file")
// to the full container path form ("var/lib/postgresql/data/file")
func rewritePath(name, base, prefix string) string {
	switch {
	case name == base:
		return prefix
	case strings.HasPrefix(name, base+"/"):
		return prefix + name[len(base):]
	default:
		return prefix + "/" + name
	}
}

func writeManifest(tw *tar.Writer, m Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	hdr := &tar.Header{
		Name:    "stasher-manifest.json",
		Size:    int64(len(data)),
		Mode:    0644,
		ModTime: time.Now(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err = tw.Write(data)
	return err
}

// applyRetention removes archives for archiveName that are older than retention.
// If retention is zero, archives are kept forever.
func (m *Manager) applyRetention(archiveName string, retention time.Duration) {
	if retention == 0 {
		return
	}
	pattern := filepath.Join(m.cfg.BackupDest, archiveName+"-*.tar.gz")
	files, err := filepath.Glob(pattern)
	if err != nil || len(files) == 0 {
		return
	}
	cutoff := time.Now().Add(-retention)
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			slog.Info("removing old backup", "file", filepath.Base(f))
			if err := os.Remove(f); err != nil {
				slog.Warn("remove failed", "file", f, "err", err)
			}
		}
	}
}
