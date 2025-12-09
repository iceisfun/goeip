package cip

// NewGetAttributeSingleRequest creates a request to read a single attribute
func NewGetAttributeSingleRequest(path Path) *MessageRouterRequest {
	return &MessageRouterRequest{
		Service:     ServiceGetAttributeSingle,
		RequestPath: path,
		RequestData: nil,
	}
}

// NewSetAttributeSingleRequest creates a request to write a single attribute
func NewSetAttributeSingleRequest(path Path, data []byte) *MessageRouterRequest {
	return &MessageRouterRequest{
		Service:     ServiceSetAttributeSingle,
		RequestPath: path,
		RequestData: data,
	}
}

// NewReadTagRequest creates a request to read a tag (symbolic segment)
// Note: This often uses a specific service or just GetAttributeSingle on the symbol?
// Actually, for Logix tags, we usually use "Read Tag" service (0x4C) or "Read Tag Fragmented" (0x52).
// But standard CIP uses GetAttributeSingle on the symbol object.
// Let's implement the Rockwell Logix "Read Tag" service (0x4C) as it's most common for "EIP PLCs".
const ServiceReadTag USINT = 0x4C
const ServiceWriteTag USINT = 0x4D

func NewReadTagRequest(tagPath Path, elements uint16) *MessageRouterRequest {
	// Read Tag Request Data:
	// Number of Elements (UINT)
	// For atomic types, elements = 1.

	// However, the path should be the Symbolic Path to the tag.

	reqData := make([]byte, 2)
	// binary.LittleEndian.PutUint16(reqData, elements)
	// Wait, we need binary package.
	reqData[0] = byte(elements)
	reqData[1] = byte(elements >> 8)

	return &MessageRouterRequest{
		Service:     ServiceReadTag,
		RequestPath: tagPath,
		RequestData: reqData,
	}
}
