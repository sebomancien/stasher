package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// ContainerSummary is one item in the ContainerList response.
type ContainerSummary struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	Labels map[string]string `json:"Labels"`
}

// ContainerJSON is the full ContainerInspect response.
type ContainerJSON struct {
	ID    string `json:"Id"`
	Name  string `json:"Name"`
	State struct {
		Running bool `json:"Running"`
	} `json:"State"`
	Config struct {
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	Mounts []MountPoint `json:"Mounts"`
}

// MountPoint describes a single volume or bind mount on a container.
type MountPoint struct {
	Type        string `json:"Type"`        // "bind", "volume", or "tmpfs"
	Name        string `json:"Name"`        // named-volume name (when Type == "volume")
	Source      string `json:"Source"`      // host path
	Destination string `json:"Destination"` // container path
}

// dockerClient talks to the Docker daemon REST API.
// Only the six endpoints used by stasher are implemented.
type dockerClient struct {
	hc   *http.Client
	base string // scheme + host + API version prefix, e.g. "http://docker/v1.48"
}

// newDockerClient creates a client from DOCKER_HOST (default: unix:///var/run/docker.sock).
// Supported schemes: unix://, tcp://.
// The API version is negotiated from the daemon at startup via GET /version.
func newDockerClient() (*dockerClient, error) {
	host := os.Getenv("DOCKER_HOST")
	if host == "" {
		host = "unix:///var/run/docker.sock"
	}

	var hc *http.Client
	var unversioned string // base URL without version, for the /version probe

	switch {
	case strings.HasPrefix(host, "unix://"):
		sock := strings.TrimPrefix(host, "unix://")
		hc = &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, "unix", sock)
				},
			},
		}
		unversioned = "http://docker"
	case strings.HasPrefix(host, "tcp://"):
		addr := strings.TrimPrefix(host, "tcp://")
		hc = &http.Client{}
		unversioned = "http://" + addr
	default:
		return nil, fmt.Errorf("unsupported DOCKER_HOST %q (supported schemes: unix://, tcp://)", host)
	}

	// Negotiate the API version with the daemon.
	apiVersion, err := negotiateAPIVersion(hc, unversioned)
	if err != nil {
		return nil, fmt.Errorf("negotiate API version: %w", err)
	}

	return &dockerClient{hc: hc, base: unversioned + "/" + apiVersion}, nil
}

// negotiateAPIVersion calls GET /_ping, which is always available without a
// version prefix, and reads the daemon's current API version from the
// Api-Version response header (documented Docker behavior since Engine 1.12).
func negotiateAPIVersion(hc *http.Client, unversioned string) (string, error) {
	resp, err := hc.Get(unversioned + "/_ping")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if v := resp.Header.Get("Api-Version"); v != "" {
		return "v" + v, nil
	}
	return "", fmt.Errorf("daemon did not return Api-Version header in /_ping response (status %d)", resp.StatusCode)
}

// ContainerList returns all running containers, optionally filtered by label.
// Each filter has the form "key" or "key=value", e.g. "stasher.enabled=true".
// Multiple filters are ANDed together.
func (c *dockerClient) ContainerList(ctx context.Context, labelFilters ...string) ([]ContainerSummary, error) {
	path := "/containers/json"
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
		return nil, apiError("ContainerList", resp)
	}
	var out []ContainerSummary
	return out, json.NewDecoder(resp.Body).Decode(&out)
}

// ContainerInspect returns detailed information about a single container.
func (c *dockerClient) ContainerInspect(ctx context.Context, id string) (ContainerJSON, error) {
	resp, err := c.get(ctx, "/containers/"+id+"/json")
	if err != nil {
		return ContainerJSON{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ContainerJSON{}, apiError("ContainerInspect", resp)
	}
	var out ContainerJSON
	return out, json.NewDecoder(resp.Body).Decode(&out)
}

// CopyFromContainer streams the contents of srcPath inside the container as a
// tar archive. The caller must close the returned ReadCloser.
func (c *dockerClient) CopyFromContainer(ctx context.Context, id, srcPath string) (io.ReadCloser, error) {
	resp, err := c.get(ctx, "/containers/"+id+"/archive?path="+url.QueryEscape(srcPath))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("CopyFromContainer %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return resp.Body, nil
}

// ContainerStop sends SIGTERM, then SIGKILL after timeoutSecs.
func (c *dockerClient) ContainerStop(ctx context.Context, id string, timeoutSecs int) error {
	resp, err := c.post(ctx, fmt.Sprintf("/containers/%s/stop?t=%d", id, timeoutSecs))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	// 204 = stopped, 304 = already stopped — both are fine.
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotModified {
		return fmt.Errorf("ContainerStop: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// ContainerStart starts a stopped container.
func (c *dockerClient) ContainerStart(ctx context.Context, id string) error {
	resp, err := c.post(ctx, "/containers/"+id+"/start")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	// 204 = started, 304 = already running — both are fine.
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotModified {
		return fmt.Errorf("ContainerStart: unexpected status %d", resp.StatusCode)
	}
	return nil
}

func (c *dockerClient) get(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return nil, err
	}
	return c.hc.Do(req)
}

func (c *dockerClient) post(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, nil)
	if err != nil {
		return nil, err
	}
	return c.hc.Do(req)
}

func apiError(op string, resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("%s: status %d: %s", op, resp.StatusCode, strings.TrimSpace(string(body)))
}
