package toggleflow

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Client holds all flag configs in memory and keeps them up to date via SSE and polling.
type Client struct {
	opts Options

	mu    sync.RWMutex
	flags map[string]FlagConfig
	etag  string

	listeners   map[string][]func()
	listenersMu sync.RWMutex

	cancel context.CancelFunc
	wg     sync.WaitGroup

	http *http.Client
}

// New creates a new Client. Call Init to start fetching flags.
func New(opts Options) *Client {
	if opts.PollInterval == 0 {
		opts.PollInterval = 30
	}
	return &Client{
		opts:      opts,
		flags:     make(map[string]FlagConfig),
		listeners: make(map[string][]func()),
		http:      &http.Client{Timeout: 10 * time.Second},
	}
}

// Init fetches all flags and starts the SSE stream and poll loop.
// Returns an error if the initial flag fetch fails.
func (c *Client) Init(ctx context.Context) error {
	if err := c.fetchFlags(ctx); err != nil {
		return fmt.Errorf("toggleflow: initial fetch failed: %w", err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	c.wg.Add(2)
	go c.sseLoop(runCtx)
	go c.pollLoop(runCtx)

	return nil
}

// Close stops the SSE stream and poll loop and waits for them to exit.
func (c *Client) Close() {
	if c.cancel != nil {
		c.cancel()
	}
	c.wg.Wait()
}

// IsEnabled returns true if the flag is enabled and the evaluated variation is truthy.
func (c *Client) IsEnabled(flagKey string, user UserContext) bool {
	flag, ok := c.getFlag(flagKey)
	if !ok || !flag.Enabled {
		return false
	}
	idx := evaluate(flag, user, nil)
	if idx < 0 || idx >= len(flag.Variations) {
		return false
	}
	var val any
	_ = json.Unmarshal(flag.Variations[idx].Value, &val)
	return val != nil && val != false && val != 0.0 && val != ""
}

// GetVariation returns the raw JSON bytes of the evaluated variation value.
// Returns nil if the flag doesn't exist.
func (c *Client) GetVariation(flagKey string, user UserContext) json.RawMessage {
	flag, ok := c.getFlag(flagKey)
	if !ok {
		return nil
	}
	idx := evaluate(flag, user, nil)
	if idx < 0 || idx >= len(flag.Variations) {
		return nil
	}
	return flag.Variations[idx].Value
}

// GetBoolVariation returns the boolean variation value, or fallback if the flag doesn't exist.
func (c *Client) GetBoolVariation(flagKey string, user UserContext, fallback bool) bool {
	raw := c.GetVariation(flagKey, user)
	if raw == nil {
		return fallback
	}
	var v bool
	if err := json.Unmarshal(raw, &v); err != nil {
		return fallback
	}
	return v
}

// GetStringVariation returns the string variation value, or fallback if the flag doesn't exist.
func (c *Client) GetStringVariation(flagKey string, user UserContext, fallback string) string {
	raw := c.GetVariation(flagKey, user)
	if raw == nil {
		return fallback
	}
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		return fallback
	}
	return v
}

// GetNumberVariation returns the number variation value, or fallback if the flag doesn't exist.
func (c *Client) GetNumberVariation(flagKey string, user UserContext, fallback float64) float64 {
	raw := c.GetVariation(flagKey, user)
	if raw == nil {
		return fallback
	}
	var v float64
	if err := json.Unmarshal(raw, &v); err != nil {
		return fallback
	}
	return v
}

// GetJSONVariation unmarshals the variation value into target, or leaves it unchanged if the flag doesn't exist.
func (c *Client) GetJSONVariation(flagKey string, user UserContext, target any) error {
	raw := c.GetVariation(flagKey, user)
	if raw == nil {
		return fmt.Errorf("flag %q not found", flagKey)
	}
	return json.Unmarshal(raw, target)
}

// OnChange registers a callback that fires when a specific flag changes.
// Use "*" to listen for any flag change. Returns an unsubscribe function.
func (c *Client) OnChange(flagKey string, fn func()) func() {
	c.listenersMu.Lock()
	c.listeners[flagKey] = append(c.listeners[flagKey], fn)
	c.listenersMu.Unlock()

	return func() {
		c.listenersMu.Lock()
		defer c.listenersMu.Unlock()
		fns := c.listeners[flagKey]
		for i, f := range fns {
			if fmt.Sprintf("%p", f) == fmt.Sprintf("%p", fn) {
				c.listeners[flagKey] = append(fns[:i], fns[i+1:]...)
				break
			}
		}
	}
}

func (c *Client) getFlag(key string) (FlagConfig, bool) {
	c.mu.RLock()
	f, ok := c.flags[key]
	c.mu.RUnlock()
	return f, ok
}

func (c *Client) fetchFlags(ctx context.Context) error {
	url := fmt.Sprintf("%s/sdk/flags?sdk_key=%s", c.opts.BaseURL, c.opts.SDKKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	c.mu.RLock()
	etag := c.etag
	c.mu.RUnlock()
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var configs []FlagConfig
	if err := json.NewDecoder(resp.Body).Decode(&configs); err != nil {
		return err
	}

	newEtag := resp.Header.Get("ETag")
	changed := c.applyConfigs(configs, newEtag)
	c.notify(changed)
	return nil
}

func (c *Client) applyConfigs(configs []FlagConfig, etag string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	if etag != "" {
		c.etag = etag
	}

	var changed []string
	for _, cfg := range configs {
		existing, ok := c.flags[cfg.Key]
		if !ok || flagChanged(existing, cfg) {
			c.flags[cfg.Key] = cfg
			changed = append(changed, cfg.Key)
		}
	}
	return changed
}

func flagChanged(a, b FlagConfig) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) != string(bb)
}

func (c *Client) notify(changed []string) {
	if len(changed) == 0 {
		return
	}
	c.listenersMu.RLock()
	defer c.listenersMu.RUnlock()
	for _, key := range changed {
		for _, fn := range c.listeners[key] {
			go fn()
		}
	}
	for _, fn := range c.listeners["*"] {
		go fn()
	}
}

func (c *Client) pollLoop(ctx context.Context) {
	defer c.wg.Done()
	ticker := time.NewTicker(time.Duration(c.opts.PollInterval) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = c.fetchFlags(ctx)
		}
	}
}

func (c *Client) sseLoop(ctx context.Context) {
	defer c.wg.Done()
	delay := time.Second
	url := fmt.Sprintf("%s/sdk/stream?sdk_key=%s", c.opts.BaseURL, c.opts.SDKKey)

	for {
		if ctx.Err() != nil {
			return
		}
		if err := c.streamOnce(ctx, url); err != nil && ctx.Err() == nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
				if delay < 30*time.Second {
					delay *= 2
				}
			}
		} else {
			delay = time.Second
		}
	}
}

func (c *Client) streamOnce(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")

	// Use a client without timeout for the SSE connection
	sseClient := &http.Client{}
	resp, err := sseClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		var event map[string]any
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}
		eventType, _ := event["type"].(string)
		if eventType != "" && eventType != "connected" {
			_ = c.fetchFlags(ctx)
		}
	}
	return scanner.Err()
}
