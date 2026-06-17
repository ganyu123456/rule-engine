package target

import (
	"bytes"
	"fmt"
	"net/http"
	"time"

	"k8s.io/klog/v2"
)

// HTTPTarget sends processed payloads via HTTP POST (or configured method).
type HTTPTarget struct {
	url     string
	method  string
	headers map[string]string
	timeout time.Duration
	client  *http.Client
}

func (t *HTTPTarget) httpClient() *http.Client {
	if t.client == nil {
		t.client = &http.Client{Timeout: t.timeout}
	}
	return t.client
}

func (t *HTTPTarget) Send(payload []byte) error {
	req, err := http.NewRequest(t.method, t.url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build HTTP request: %w", err)
	}

	// Set default content type; allow override via headers.
	req.Header.Set("Content-Type", "application/json")
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}

	resp, err := t.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("HTTP %s %s: %w", t.method, t.url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %s %s: unexpected status %d", t.method, t.url, resp.StatusCode)
	}

	klog.V(5).Infof("HTTP target: %s %s → %d (%d bytes sent)", t.method, t.url, resp.StatusCode, len(payload))
	return nil
}

func (t *HTTPTarget) Close() error {
	return nil
}
