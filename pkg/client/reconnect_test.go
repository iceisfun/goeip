package client

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/iceisfun/goeip/internal"
	"github.com/iceisfun/goeip/pkg/eip"
	"github.com/iceisfun/goeip/pkg/session"
)

// MockTransport implements transport.Transport
type MockTransport struct {
	sendFunc    func(cmd eip.Command, data []byte, sessionHandle eip.SessionHandle) error
	receiveFunc func() (*eip.EncapsulationHeader, []byte, error)
	closeFunc   func() error
}

func (m *MockTransport) Send(cmd eip.Command, data []byte, sessionHandle eip.SessionHandle) error {
	if m.sendFunc != nil {
		return m.sendFunc(cmd, data, sessionHandle)
	}
	return nil
}

func (m *MockTransport) Receive() (*eip.EncapsulationHeader, []byte, error) {
	if m.receiveFunc != nil {
		return m.receiveFunc()
	}
	return &eip.EncapsulationHeader{Status: eip.StatusSuccess}, nil, nil
}

func (m *MockTransport) Close() error {
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	return nil
}

func TestReconnectingClient_Retry(t *testing.T) {
	// Setup checks
	attempts := 0

	// Factory that returns a client which fails 'attempts' times then succeeds
	factory := func(address string, logger internal.Logger) (*Client, error) {
		mockT := &MockTransport{
			sendFunc: func(cmd eip.Command, data []byte, sessionHandle eip.SessionHandle) error {
				attempts++
				if attempts <= 2 {
					return fmt.Errorf("connection lost")
				}
				return nil
			},
			receiveFunc: func() (*eip.EncapsulationHeader, []byte, error) {
				// For ReadTag, Receive is called after Send.
				// If Send succeeds (attempt > 2), this is called.
				// We need to return a valid response for ReadTag.
				// Client expects (Header, Data).
				// Data for ReadTag response: [Type:2 bytes][Data...]
				// But Client.ReadTag wraps recursive SendCIPRequest.
				// SendCIPRequest -> SendRRData -> Send(RRData) -> Receive(RRDataSafe)
				// It's getting complicated to mock the full CIP conversation.

				// SIMPLIFICATION:
				// We only care that *Client methods fail.
				// If sendFunc fails, Client.ReadTag returns error.
				return nil, nil, fmt.Errorf("should not be reached if send fails")
			},
			closeFunc: func() error { return nil },
		}

		// To properly construct a client we need a Session.
		// Session needs Transport.
		s := session.NewSession(mockT, logger)
		// We can inject session into *Client (private field)
		return &Client{session: s, logger: logger}, nil
	}

	rc, err := NewReconnectingClient("localhost", nil,
		WithClientFactory(factory),
		WithMaxRetries(5),
		WithRetryDelay(1*time.Millisecond),
		WithAutoConnect(false), // don't connect in constructor to control calls
	)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// First call should fail twice (attempts 1, 2) and succeed on 3rd?
	// But wait, executeWithRetry loop:
	// i=0: getClient -> connects (attempt=0? no factory called inside connect).
	// connect calls factory.
	// factory returns a Client (with the mock transport).
	// i=0: op(client) -> calls ReadTag -> calls transport.Send -> increments attempts=1. Returns error "connection lost".
	// Loop catches error. Warning logged.
	// client closed. client = nil.
	// Sleep.
	// i=1: getClient -> connects -> factory called (new client, new mock? No, factory logic needs to be stateful across calls if we create new transports).
	// actually my factory creates a new closure?
	// "factory" variable above captures "attempts".
	// So new calls to factory return new client with same "attempts" counter.
	// i=1: op(client) -> ReadTag -> Send -> attempts=2. Error.
	// i=2: op(client) -> ReadTag -> Send -> attempts=3. Success.

	// BUT connection/register logic?
	// NewClient normally registers.
	// My factory returns &Client{...}. It skips Register.
	// executeWithRetry calls op(client). op is c.ReadTag.
	// mocking transport.Send handles ReadTag traffic.
	// But `Send` is called for RRData as well.
	// Client.ReadTag does `SendCIPRequest`.
	// For this test, purely assuming transport fails is enough.

	// However, success case needs to return valid data to avoid other errors.
	// Mocking success is hard without real packets.
	// Maybe we just check that we retried N times?
	// If it eventually fails with specific error (decoding error) after 3 retries of connection error, that proves retries happened.

	rc.ReadTag("test")
	// attempts should be 3 (1 fail, 2 fail, 3 success-send).
	// But wait, if Send succeeds, Receive is called.
	// My mock Receive returns error.
	// So ReadTag checks Send (ok), then Receive (error).
	// isNetworkError checks the error.
	// If Receive returns error, does it retry?
	// My implementation `isNetworkError` returns true always.
	// So it should retry again?
	// attempts=3: Send OK, Receive Error -> Retry.
	// attempts=4: Send OK, Receive Error -> Retry.
	// ... until MaxRetries.

	// Verify behavior:
	// If we want success, we need Receive to return something valid, OR just count matching attempts.
	if _, err := rc.ReadTag("test"); err == nil {
		// If it succeeded, attempts must be > 2
		if attempts < 3 {
			t.Errorf("Expected at least 3 attempts, got %d", attempts)
		}
	} else {
		// If it failed (which it will because receiveFunc returns error),
		// we still expect attempts to have happened.
		// receiveFunc error is not "connection" error so it might stop retries?
		// No, isNetworkError isn't checked in current implementation, relying on any error?
		// Recheck implementation: yes, naive retry on any error.
		if attempts < 3 {
			t.Errorf("Expected at least 3 attempts, got %d (err: %v)", attempts, err)
		}
	}
}

func TestReconnectingClient_InfiniteRetry(t *testing.T) {
	attempts := 0
	factory := func(addr string, l internal.Logger) (*Client, error) {
		mockT := &MockTransport{
			sendFunc: func(cmd eip.Command, data []byte, sessionHandle eip.SessionHandle) error {
				attempts++
				if attempts < 10 {
					return fmt.Errorf("fail")
				}
				return nil
			},
		}
		s := session.NewSession(mockT, l)
		return &Client{session: s, logger: l}, nil
	}

	rc, _ := NewReconnectingClient("addr", nil,
		WithClientFactory(factory),
		WithMaxRetries(-1), // Infinite
		WithRetryDelay(1*time.Microsecond),
	)

	// Should block until success (after 10 failures)
	// If infinite retry is broken (e.g. treats -1 as 0), it would fail immediately.
	_, err := rc.ReadTag("foo")
	if err != nil {
		t.Fatalf("Expected success after retries, got error: %v", err)
	}

	if attempts < 10 {
		t.Errorf("Expected at least 10 attempts, got %d", attempts)
	}
}

func TestReconnectingClient_Reconnect(t *testing.T) {
	// Test that it actually creates NEW clients
	clientCount := 0
	factory := func(addr string, l internal.Logger) (*Client, error) {
		clientCount++
		mockT := &MockTransport{
			sendFunc: func(cmd eip.Command, data []byte, sessionHandle eip.SessionHandle) error {
				return errors.New("always fail")
			},
		}
		s := session.NewSession(mockT, l)
		return &Client{session: s, logger: l}, nil
	}

	rc, _ := NewReconnectingClient("addr", nil,
		WithClientFactory(factory),
		WithMaxRetries(2),
		WithRetryDelay(1*time.Millisecond),
	)

	rc.ReadTag("foo")

	// Attempts:
	// 0: connect (clientCount=1). op -> fail. close.
	// 1: connect (clientCount=2). op -> fail. close.
	// 2: connect (clientCount=3). op -> fail. close.
	// Retries exceeded.

	// MaxRetries=2 means: initial attempt + 2 retries = 3 total? Or 0..2 loop = 3 attempts?
	// Code: for i := 0; i <= rc.maxRetries; i++ { ... }
	// So i=0, 1, 2. Three iterations.

	if clientCount != 3 {
		t.Errorf("Expected 3 client creations, got %d", clientCount)
	}
}
