package cip

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// Marshaler is the interface implemented by types that can marshal
// themselves into a CIP binary description.
type Marshaler interface {
	MarshalCIP() ([]byte, error)
}

// Marshal returns the CIP encoding of v.
func Marshal(v any) ([]byte, error) {
	// 1. Check if v implements Marshaler
	if m, ok := v.(Marshaler); ok {
		return m.MarshalCIP()
	}

	// 2. Handle basic types and structs using binary.Write
	buf := new(bytes.Buffer)
	if err := binary.Write(buf, binary.LittleEndian, v); err != nil {
		return nil, fmt.Errorf("cip: binary.Write failed: %w", err)
	}

	return buf.Bytes(), nil
}
