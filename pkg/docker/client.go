package docker

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
)

const (
	API_VERSION = "v1.54"
)

// Client talks to the Docker daemon REST API.
type Client struct {
	hc   *http.Client
	base string // scheme + host + API version prefix, e.g. "http://docker/v1.54"
}

// Creates a client from DOCKER_HOST (default: unix:///var/run/docker.sock).
// Supported schemes: unix://, tcp://.
func NewClient() (*Client, error) {
	host := os.Getenv("DOCKER_HOST")
	if host == "" {
		host = "unix:///var/run/docker.sock"
	}

	var hc *http.Client
	var base string // base URL without version

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
		base = "http://docker"
	case strings.HasPrefix(host, "tcp://"):
		addr := strings.TrimPrefix(host, "tcp://")
		hc = &http.Client{}
		base = "http://" + addr
	default:
		return nil, fmt.Errorf("unsupported DOCKER_HOST %q (supported schemes: unix://, tcp://)", host)
	}

	return &Client{
		hc:   hc,
		base: fmt.Sprintf("%s/%s/", base, API_VERSION),
	}, nil
}

// Perform an HTTP GET request and returns the response.
func (c *Client) get(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return nil, err
	}
	return c.hc.Do(req)
}

// Perform an HTTP POST request and returns the response.
func (c *Client) post(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, nil)
	if err != nil {
		return nil, err
	}
	return c.hc.Do(req)
}
