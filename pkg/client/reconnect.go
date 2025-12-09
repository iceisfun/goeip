package client

import (
	"fmt"
	"sync"
	"time"

	"github.com/iceisfun/goeip/internal"
)

// ClientFactory is a function that creates a new Client.
// Useful for testing to inject mock clients.
type ClientFactory func(address string, logger internal.Logger) (*Client, error)

// ReconnectingClient is a wrapper around Client that automatically reconnects
// when a network error occurs.
type ReconnectingClient struct {
	address string
	logger  internal.Logger
	factory ClientFactory

	mu     sync.RWMutex
	client *Client
	closed bool

	// Configuration
	maxRetries  int
	retryDelay  time.Duration
	autoConnect bool
}

// ReconnectOption configures a ReconnectingClient.
type ReconnectOption func(*ReconnectingClient)

// WithMaxRetries sets the maximum number of retries for a single operation.
// Default is 3. Set to -1 for infinite retries.
func WithMaxRetries(n int) ReconnectOption {
	return func(rc *ReconnectingClient) {
		rc.maxRetries = n
	}
}

// WithRetryDelay sets the delay between retries.
// Default is 1 second.
func WithRetryDelay(d time.Duration) ReconnectOption {
	return func(rc *ReconnectingClient) {
		rc.retryDelay = d
	}
}

// WithAutoConnect determines if the client should connect immediately on creation.
// Default is true.
func WithAutoConnect(b bool) ReconnectOption {
	return func(rc *ReconnectingClient) {
		rc.autoConnect = b
	}
}

// WithClientFactory sets a custom client factory (mainly for testing).
func WithClientFactory(f ClientFactory) ReconnectOption {
	return func(rc *ReconnectingClient) {
		rc.factory = f
	}
}

// NewReconnectingClient creates a new ReconnectingClient.
func NewReconnectingClient(address string, logger internal.Logger, opts ...ReconnectOption) (*ReconnectingClient, error) {
	if logger == nil {
		logger = internal.NopLogger()
	}

	rc := &ReconnectingClient{
		address:     address,
		logger:      logger,
		factory:     NewClient, // Default to standard NewClient
		maxRetries:  3,
		retryDelay:  1 * time.Second,
		autoConnect: true,
	}

	for _, opt := range opts {
		opt(rc)
	}

	if rc.autoConnect {
		if err := rc.connect(); err != nil {
			// If auto-connect fails, we just log it and leave client nil.
			// The first operation will try to connect again.
			rc.logger.Warnf("Initial connection to %s failed: %v. Will retry on first operation.", address, err)
		}
	}

	return rc, nil
}

// connect attempts to establish a connection.
// Caller must hold lock or ensure safety.
func (rc *ReconnectingClient) connect() error {
	if rc.factory == nil {
		return fmt.Errorf("no client factory configured")
	}

	c, err := rc.factory(rc.address, rc.logger)
	if err != nil {
		return err
	}
	rc.client = c
	return nil
}

// getClient returns the current client, or attempts to connect if nil.
func (rc *ReconnectingClient) getClient() (*Client, error) {
	rc.mu.RLock()
	if rc.closed {
		rc.mu.RUnlock()
		return nil, fmt.Errorf("client is closed")
	}
	c := rc.client
	rc.mu.RUnlock()

	if c != nil {
		return c, nil
	}

	// Needs connection
	rc.mu.Lock()
	defer rc.mu.Unlock()

	// Check again in case someone else connected
	if rc.closed {
		return nil, fmt.Errorf("client is closed")
	}
	if rc.client != nil {
		return rc.client, nil
	}

	if err := rc.connect(); err != nil {
		return nil, err
	}
	return rc.client, nil
}

// Close closes the underlying client and prevents future reconnections.
func (rc *ReconnectingClient) Close() error {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	rc.closed = true
	if rc.client != nil {
		err := rc.client.Close()
		rc.client = nil
		return err
	}
	return nil
}

// ReadTag reads a tag with automatic reconnection.
func (rc *ReconnectingClient) ReadTag(name string) ([]byte, error) {
	return rc.executeWithRetry(func(c *Client) ([]byte, error) {
		return c.ReadTag(name)
	})
}

// WriteTag writes a tag with automatic reconnection.
func (rc *ReconnectingClient) WriteTag(name string, value any) error {
	_, err := rc.executeWithRetry(func(c *Client) ([]byte, error) {
		return nil, c.WriteTag(name, value)
	})
	return err
}

// executeWithRetry runs an operation, reconnecting on failure.
func (rc *ReconnectingClient) executeWithRetry(op func(*Client) ([]byte, error)) ([]byte, error) {
	var lastErr error

	for i := 0; rc.maxRetries < 0 || i <= rc.maxRetries; i++ {
		// 1. Get functional client
		client, err := rc.getClient()
		if err != nil {
			lastErr = err
			// Backoff before retry loop continues
			if rc.maxRetries < 0 || i < rc.maxRetries {
				time.Sleep(rc.retryDelay)
			}
			continue
		}

		// 2. Attempt operation
		res, err := op(client)
		if err == nil {
			return res, nil
		}
		lastErr = err

		// 3. Handle failure
		limitStr := fmt.Sprintf("%d", rc.maxRetries+1)
		if rc.maxRetries < 0 {
			limitStr = "∞"
		}
		rc.logger.Warnf("Operation failed (attempt %d/%s): %v", i+1, limitStr, err)

		// Invalidate current client so next loop calls getClient -> connect
		rc.mu.Lock()
		if rc.client == client { // Only if hasn't changed
			if rc.client != nil {
				// Close old client best effort
				rc.client.Close()
				rc.client = nil
			}
		}
		rc.mu.Unlock()

		if rc.maxRetries < 0 || i < rc.maxRetries {
			time.Sleep(rc.retryDelay)
		}

		// Prevent integer overflow for long-running infinite retries
		if i == 2147483647 { // MaxInt32, safe enough
			i = 0
		}
	}

	return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}
