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
	"stasher/pkg/docker"
	"stasher/pkg/retention"
)

// Manifest is written as the first entry ("stasher-manifest.json") of every archive.
type Manifest struct {
	ContainerID   string    `json:"container_id"`
	ContainerName string    `json:"container_name"`
	Timestamp     time.Time `json:"timestamp"`
	Path          string    `json:"path"`
}

// Backs up a single configured volume for the given container.
// It re-inspects the container at call time to pick up any label changes since registration.
func (m *Manager) backupContainerVolume(ctx context.Context, container docker.Container, volID string) error {
	info, err := container.Inspect(ctx)
	if err != nil {
		return fmt.Errorf("inspect: %w", err)
	}

	cb, err := label.Unmarshal(info.Config.Labels)
	if err != nil {
		return fmt.Errorf("unmarshal labels: %w", err)
	}

	vol, ok := cb.Volumes[volID]
	if !ok {
		return fmt.Errorf("volume %q no longer configured", volID)
	}

	containerName := strings.TrimPrefix(info.Name, "/")

	if cb.Options.StopDuringBackup && info.State.Running {
		slog.Info("stopping container for backup", "container", containerName)
		err := container.Stop(ctx, 30*time.Second)
		if err != nil {
			return fmt.Errorf("stop: %w", err)
		}
		defer func() {
			slog.Info("restarting container", "container", containerName)
			err := container.Start(ctx)
			if err != nil {
				slog.Error("restart failed", "container", containerName, "err", err)
			}
		}()
	}

	return m.backupVolume(ctx, container, containerName, volID, vol)
}

// Writes one archive for the given volume configuration.
func (m *Manager) backupVolume(ctx context.Context, container docker.Container, containerName, volID string, vol label.Volume) error {
	archiveName := containerName + "-" + volID

	timestamp := time.Now().UTC().Format("20060102-150405")
	filename := fmt.Sprintf("%s-%s.tar.gz", archiveName, timestamp)
	outPath := filepath.Join(m.config.BackupDest, filename)

	slog.Info("writing backup", "container", containerName, "file", filename)
	err := m.writeArchive(ctx, container, outPath, containerName, vol.Path)
	if err != nil {
		os.Remove(outPath)
		return fmt.Errorf("write archive: %w", err)
	}

	slog.Info("stasher complete", "container", containerName, "file", filename)
	m.applyRetention(archiveName, vol.Keep)
	return nil
}

// Creates the .tar.gz archive at outPath for the given container path.
func (m *Manager) writeArchive(ctx context.Context, container docker.Container, outPath, containerName, path string) (rerr error) {
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer func() {
		err := f.Close()
		if err != nil && rerr == nil {
			rerr = err
		}
	}()

	gz := gzip.NewWriter(f)
	defer func() {
		err := gz.Close()
		if err != nil && rerr == nil {
			rerr = err
		}
	}()

	tw := tar.NewWriter(gz)
	defer func() {
		err := tw.Close()
		if err != nil && rerr == nil {
			rerr = err
		}
	}()

	// Write manifest as the first entry so it can be inspected without extracting the whole archive
	manifest := Manifest{
		ContainerID:   container.Id[:12],
		ContainerName: containerName,
		Timestamp:     time.Now().UTC(),
		Path:          path,
	}
	err = writeManifest(tw, manifest)
	if err != nil {
		return fmt.Errorf("manifest: %w", err)
	}

	rc, err := container.CopyFrom(ctx, path)
	if err != nil {
		return fmt.Errorf("copy %s: %w", path, err)
	}
	defer rc.Close()

	err = repackTar(tw, rc, path)
	if err != nil {
		return fmt.Errorf("repack %s: %w", path, err)
	}

	return nil
}

// Reads a tar stream and rewrites each header name so files appear at their full container path.
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

		err = tw.WriteHeader(hdr)
		if err != nil {
			return err
		}
		// Copy file data, symlinks, hard links, and directories have no body
		if hdr.Typeflag != tar.TypeSymlink &&
			hdr.Typeflag != tar.TypeLink &&
			hdr.Typeflag != tar.TypeDir &&
			hdr.Size > 0 {
			_, err := io.Copy(tw, tr)
			if err != nil {
				return err
			}
		}
	}
}

// Renames a tar entry from the Docker-relative form ("data/file")
// to the full container path form ("var/lib/postgresql/data/file").
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

// Writes a file named "stasher-manifest.json" into a tar archive header.
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
	err = tw.WriteHeader(hdr)
	if err != nil {
		return err
	}
	_, err = tw.Write(data)
	return err
}

// Applies the keep policy for archiveName by deleting archives not covered by any active period.
// If the policy is empty or all values are 0, nothing is deleted.
func (m *Manager) applyRetention(archiveName string, policy label.KeepPolicy) {
	pattern := filepath.Join(m.config.BackupDest, archiveName+"-*.tar.gz")
	files, err := filepath.Glob(pattern)
	if err != nil || len(files) == 0 {
		return
	}

	var archives []retention.Archive
	for _, f := range files {
		t, ok := retention.ParseArchiveTime(filepath.Base(f), archiveName)
		if !ok {
			continue
		}
		archives = append(archives, retention.Archive{Name: f, Time: t})
	}

	for _, f := range retention.Apply(archives, policy) {
		slog.Info("removing old backup", "file", filepath.Base(f))
		err := os.Remove(f)
		if err != nil {
			slog.Warn("remove failed", "file", f, "err", err)
		}
	}
}
