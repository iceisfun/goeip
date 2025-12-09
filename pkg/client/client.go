package client

import (
	"fmt"
	"github.com/iceisfun/goeip/internal"
	"github.com/iceisfun/goeip/pkg/cip"
	"github.com/iceisfun/goeip/pkg/session"
	"github.com/iceisfun/goeip/pkg/transport"
)

// Client is a high-level EIP client
type Client struct {
	session *session.Session
	logger  internal.Logger
}

// NewClient creates a new client
func NewClient(address string, logger internal.Logger) (*Client, error) {
	t, err := transport.NewTCPTransport(address)
	if err != nil {
		return nil, err
	}

	s := session.NewSession(t, logger)
	if err := s.Register(); err != nil {
		t.Close()
		return nil, err
	}

	return &Client{session: s, logger: logger}, nil
}

// Close closes the client connection
// Close closes the client connection
func (c *Client) Close() error {
	if err := c.session.Unregister(); err != nil {
		// Log error but continue to close transport
		c.logger.Errorf("Failed to unregister session: %v", err)
	}
	return c.session.Close()
}

// ReadTag reads a tag from the PLC
func (c *Client) ReadTag(tagName string) ([]byte, error) {
	// Build Path
	p := cip.NewPath()
	p.AddSymbolicSegment(tagName)

	// Create Request (Read 1 element)
	req := cip.NewReadTagRequest(p, 1)

	// Send Request
	resp, err := c.session.SendCIPRequest(req)
	if err != nil {
		return nil, err
	}

	if err := resp.Error(); err != nil {
		return nil, err
	}

	// Response Data for Read Tag:
	// Type (UINT)
	// Data (...)
	// We return the raw data including type for now, or we could parse it.
	// Let's return the raw data payload (excluding the type? No, type is important).
	return resp.ResponseData, nil
}

// ReadTagInto reads a tag from the PLC and unmarshals it into dst.
// dst must be a pointer to a type that can be unmarshaled (basic type, struct, or Unmarshaler).
func (c *Client) ReadTagInto(tagName string, dst any) error {
	data, err := c.ReadTag(tagName)
	if err != nil {
		return err
	}

	// The ReadTag response includes the data type code (UINT) at the beginning.
	// We need to skip it to get to the actual data.
	// Response format: [Type:UINT] [Data...]
	if len(data) < 2 {
		return fmt.Errorf("response too short to contain type code")
	}

	// Skip the first 2 bytes (Type)
	return cip.Unmarshal(data[2:], dst)
}

// ReadTimer reads a Timer tag from the PLC and decodes it.
func (c *Client) ReadTimer(tagName string) (*cip.Timer, error) {
	data, err := c.ReadTag(tagName)
	if err != nil {
		return nil, err
	}

	// The ReadTag response includes the data type code (UINT) at the beginning.
	// We need to skip it to get to the actual Timer structure data.
	// Response format: [Type:UINT] [Data...]
	if len(data) < 2 {
		return nil, fmt.Errorf("response too short to contain type code")
	}

	// Check if the type is a structure (0x02A0) or just raw bytes?
	// Actually, for a TIMER, it might return the raw bytes of the structure.
	// The type code for a structure is typically 0x02A0 (Structure).
	// But let's just skip the first 2 bytes (Type) and pass the rest to DecodeTimer.
	// Note: DecodeTimer expects 14 bytes.
	// So we need at least 2 + 14 = 16 bytes.

	return cip.DecodeTimer(data[2:])
}
