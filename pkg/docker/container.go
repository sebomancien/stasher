package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type MountPoint struct {
	// Enum: "bind" "cluster" "image" "npipe" "tmpfs" "volume"
	// The mount type:
	//   - `bind` a mount of a file or directory from the host into the container.
	//   - `cluster` a Swarm cluster volume.
	//   - `image` an OCI image.
	//   - `npipe` a named pipe from the host into the container.
	//   - `tmpfs` a tmpfs.
	//   - `volume` a docker volume with the given Name.
	Type string `json:"Type"`
	// Name is the name reference to the underlying data defined by Source e.g., the volume name.
	Name string `json:"Name"`
	// Source location of the mount.
	// For volumes, this contains the storage location of the volume (within /var/lib/docker/volumes/).
	// For bind-mounts, and npipe, this contains the source (host) part of the bind-mount.
	// For tmpfs mount points, this field is empty.
	Source string `json:"Source"`
	// Destination is the path relative to the container root (/) where the Source is mounted inside the container.
	Destination string `json:"Destination"`
	// Driver is the volume driver used to create the volume (if it is a volume).
	Driver string `json:"Driver"`
	// Mode is a comma separated list of options supplied by the user when creating the bind/volume mount.
	// The default is platform-specific ("z" on Linux, empty on Windows).
	Mode string `json:"Mode"`
	// Whether the mount is mounted writable (read-write).
	ReadWrite bool `json:"RW"`
	// Propagation describes how mounts are propagated from the host into the mount point, and vice-versa.
	// Refer to the Linux kernel documentation for details.
	// This field is not used on Windows.
	Propagation string `json:"Propagation"`
}

type ContainerHealth struct {
	Status        string `json:"Status"`
	FailingStreak int    `json:"FailingStreak"`
}

type Container struct {
	client *Client

	Id      string   `json:"Id"`
	Names   []string `json:"Names"`
	Image   string   `json:"Image"`
	ImageId string   `json:"ImageID"`
	Command string   `json:"Command"`
	Created int64    `json:"Created"`
	Ports   []struct {
		IP          string `json:"IP"`
		PrivatePort uint16 `json:"PrivatePort"`
		PublicPort  uint16 `json:"PublicPort"`
		Type        string `json:"Type"`
	} `json:"Ports"`
	SizeRw     *int64            `json:"SizeRw"`
	SizeRootFs *int64            `json:"SizeRootFs"`
	Labels     map[string]string `json:"Labels"`
	State      string            `json:"State"`
	Status     string            `json:"Status"`
	HostConfig struct {
		NetworkMode string            `json:"NetworkMode"`
		Annotations map[string]string `json:"Annotations"`
	} `json:"HostConfig"`
	Mounts []MountPoint    `json:"Mounts"`
	Health ContainerHealth `json:"Health"`
}

// ContainerInspect is the full Inspect response.
type ContainerInspect struct {
	Id      string   `json:"Id"`
	Created string   `json:"Created"`
	Path    string   `json:"Path"`
	Args    []string `json:"Args"`
	State   struct {
		Status     string          `json:"Status"`
		Running    bool            `json:"Running"`
		Paused     bool            `json:"Paused"`
		Restarting bool            `json:"Restarting"`
		OOMKilled  bool            `json:"OOMKilled"`
		Dead       bool            `json:"Dead"`
		Pid        int             `json:"Pid"`
		ExitCode   int             `json:"ExitCode"`
		Error      string          `json:"Error"`
		StartedAt  string          `json:"StartedAt"`
		FinishedAt string          `json:"FinishedAt"`
		Health     ContainerHealth `json:"Health"`
	} `json:"State"`
	Name   string `json:"Name"`
	Config struct {
		Hostname     string            `json:"Hostname"`
		Domainname   string            `json:"Domainname"`
		User         string            `json:"User"`
		AttachStdin  bool              `json:"AttachStdin"`
		AttachStdout bool              `json:"AttachStdout"`
		AttachStderr bool              `json:"AttachStderr"`
		Tty          bool              `json:"Tty"`
		OpenStdin    bool              `json:"OpenStdin"`
		StdinOnce    bool              `json:"StdinOnce"`
		Env          []string          `json:"Env"`
		Cmd          []string          `json:"Cmd"`
		Image        string            `json:"Image"`
		WorkingDir   string            `json:"WorkingDir"`
		Entrypoint   []string          `json:"Entrypoint"`
		Labels       map[string]string `json:"Labels"`
	} `json:"Config"`
	Mounts []MountPoint `json:"Mounts"`
}

// List all running containers, optionally filtered by label.
// Each filter has the form "key" or "key=value", e.g. "stasher.enabled=true".
// Multiple filters are ANDed together.
func (c *Client) ListContainers(ctx context.Context, labelFilters ...string) ([]Container, error) {
	path := "containers/json"
	if len(labelFilters) > 0 {
		labels, _ := json.Marshal(labelFilters)
		path += "?filters=" + url.QueryEscape(`{"label":`+string(labels)+`}`)
	}
	resp, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to list containers. unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var out []Container
	err = json.NewDecoder(resp.Body).Decode(&out)
	if err != nil {
		return nil, err
	}

	for i := range out {
		out[i].client = c
	}
	return out, nil
}

func (c *Container) get(ctx context.Context, path string) (*http.Response, error) {
	return c.client.get(ctx, fmt.Sprintf("containers/%s/%s", c.Id, path))
}

func (c *Container) post(ctx context.Context, path string) (*http.Response, error) {
	return c.client.post(ctx, fmt.Sprintf("containers/%s/%s", c.Id, path))
}

// Inspects the detailed information about the container.
func (c *Container) Inspect(ctx context.Context) (*ContainerInspect, error) {
	resp, err := c.get(ctx, "json")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to inspect container. unexpected status %d", resp.StatusCode)
	}
	var out ContainerInspect
	return &out, json.NewDecoder(resp.Body).Decode(&out)
}

// Copies a streams from srcPath inside the container as a tar archive.
// The caller must close the returned ReadCloser.
func (c *Container) CopyFrom(ctx context.Context, srcPath string) (io.ReadCloser, error) {
	resp, err := c.get(ctx, "archive?path="+url.QueryEscape(srcPath))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("failed to copy from container %s. unexpected status %d. content %s", c.Id, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return resp.Body, nil
}

// Stops a container by sending SIGTERM, then SIGKILL after timeout.
func (c *Container) Stop(ctx context.Context, timeout time.Duration) error {
	resp, err := c.post(ctx, fmt.Sprintf("stop?t=%d", int64(timeout.Seconds())))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	// 204 = stopped, 304 = already stopped — both are fine.
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotModified {
		return fmt.Errorf("failed to stop container %s. unexpected status %d", c.Id, resp.StatusCode)
	}
	return nil
}

// Starts a stopped container.
func (c *Container) Start(ctx context.Context) error {
	resp, err := c.post(ctx, "start")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	// 204 = started, 304 = already running — both are fine.
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotModified {
		return fmt.Errorf("failed to start container %s. unexpected status %d", c.Id, resp.StatusCode)
	}
	return nil
}
