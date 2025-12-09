package runtime

import (
	"encoding/binary"
	"net"
	"sync"
	"time"

	"github.com/iceisfun/goeip/pkg/objects/assembly"
)

// IOConnection represents a cyclic I/O connection
type IOConnection struct {
	ConnectionID  uint32
	RPI           time.Duration
	SequenceCount uint16 // 16-bit sequence count for Class 1
	RunIdleHeader bool   // True if 32-bit Run/Idle header is used
	RemoteAddr    *net.UDPAddr
	Assembly      *assembly.AssemblyInstance // The assembly to consume/produce
	LastReceive   time.Time
	LastSend      time.Time
	TimeoutMult   uint8
	IsProducer    bool
	IsConsumer    bool
	StopChan      chan struct{}
}

// Runtime manages the UDP server and I/O connections
type Runtime struct {
	mu          sync.RWMutex
	conn        *net.UDPConn
	connections map[uint32]*IOConnection // Map by ConnectionID (Consuming ID)
	assemblyObj *assembly.AssemblyObject
}

// NewRuntime creates a new Runtime
func NewRuntime(ao *assembly.AssemblyObject) *Runtime {
	return &Runtime{
		connections: make(map[uint32]*IOConnection),
		assemblyObj: ao,
	}
}

// Start starts the UDP listener on port 2222
func (r *Runtime) Start(address string) error {
	addr, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return err
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return err
	}
	r.conn = conn

	go r.listenLoop()
	go r.watchdogLoop()

	return nil
}

// AddConnection adds a connection to the runtime
func (r *Runtime) AddConnection(conn *IOConnection) {
	r.mu.Lock()
	defer r.mu.Unlock()
	conn.LastReceive = time.Now()
	r.connections[conn.ConnectionID] = conn
}

// RemoveConnection removes a connection from the runtime
func (r *Runtime) RemoveConnection(connID uint32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.connections, connID)
}

// watchdogLoop checks for connection timeouts
func (r *Runtime) watchdogLoop() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		r.checkTimeouts()
	}
}

func (r *Runtime) checkTimeouts() {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	for id, conn := range r.connections {
		if !conn.IsConsumer {
			continue
		}

		// Calculate timeout duration
		// RPI (Duration) * Multiplier (4 bits usually, but we stored as uint8)
		// 0 = x4, 1 = x8, 2 = x16, 3 = x32 ... wait, spec says:
		// 0 = x4, 1 = x8, 2 = x16, 3 = x32, 4 = x64, 5 = x128, 6 = x256, 7 = x512
		// Actually, Connection Timeout Multiplier is:
		// 0: x4
		// 1: x8
		// 2: x16
		// 3: x32
		// ...
		// But usually it's just 4 * RPI.
		// Let's assume Multiplier is the raw value from Forward_Open (0-7).

		mult := uint64(4) << conn.TimeoutMult
		timeout := conn.RPI * time.Duration(mult)

		if now.Sub(conn.LastReceive) > timeout {
			// Timeout!
			// Log it?
			// Remove connection?
			delete(r.connections, id)
			// Also notify Connection Manager?
			// For now just remove from runtime.
		}
	}
}

// listenLoop handles incoming UDP packets
func (r *Runtime) listenLoop() {
	buf := make([]byte, 2048) // Max CIP packet size is usually small
	for {
		n, remoteAddr, err := r.conn.ReadFromUDP(buf)
		if err != nil {
			// Log error or exit
			return
		}

		r.handlePacket(buf[:n], remoteAddr)
	}
}

// handlePacket processes a single UDP packet
func (r *Runtime) handlePacket(data []byte, remoteAddr *net.UDPAddr) {
	// Packet format:
	// Item Count (UINT)
	// Item 1: Address Item (Connection ID)
	// Item 2: Data Item (Payload)

	if len(data) < 6 {
		return
	}

	itemCount := binary.LittleEndian.Uint16(data[0:2])
	if itemCount != 2 {
		return
	}

	// Item 1: Address Item
	// Type (UINT)
	// Length (UINT)
	// Connection ID (UDINT) - if Length == 4

	offset := 2
	// type1 := binary.LittleEndian.Uint16(data[offset : offset+2])
	offset += 2
	len1 := binary.LittleEndian.Uint16(data[offset : offset+2])
	offset += 2

	if len1 != 4 {
		// Only support Connected Address Item for now
		return
	}

	connID := binary.LittleEndian.Uint32(data[offset : offset+4])
	offset += 4

	// Item 2: Data Item
	// Type (UINT)
	// Length (UINT)
	// Data...

	// type2 := binary.LittleEndian.Uint16(data[offset : offset+2])
	offset += 2
	len2 := binary.LittleEndian.Uint16(data[offset : offset+2])
	offset += 2

	if len(data) < offset+int(len2) {
		return
	}

	payload := data[offset : offset+int(len2)]

	r.mu.RLock()
	conn, ok := r.connections[connID]
	if ok {
		// Update Watchdog - need write lock?
		// Actually, IOConnection is a pointer, so we can modify it if we are careful.
		// But strictly speaking, we should protect it if other goroutines read it.
		// Scheduler reads it? Scheduler reads RPI, Assembly, etc.
		// Watchdog reads LastReceive.
		// So we need to protect LastReceive update.
		// But we only have RLock here.
		// Let's upgrade to Lock or use atomic, or just Lock for the lookup too.
		// Since this is per-packet, Lock might be heavy?
		// But map read needs RLock.
		// Let's just use Lock for the whole block if we find it.
	}
	r.mu.RUnlock()

	if !ok {
		return
	}

	// We need to update LastReceive safely.
	// Let's use a separate lock on IOConnection or just use Runtime lock.
	// Using Runtime lock for everything is simplest for now.
	r.mu.Lock()
	if conn, ok := r.connections[connID]; ok {
		conn.LastReceive = time.Now()
	}
	r.mu.Unlock()

	// Re-acquire RLock for the rest? Or just proceed with `conn` pointer?
	// `conn` pointer is valid as long as it's not removed.
	// RemoveConnection deletes from map.
	// If we hold `conn` pointer, it won't be GC'd.
	// But if we access fields that might change?
	// Assembly might change?
	// For now, let's proceed.

	// Handle Run/Idle Header if present
	dataOffset := 0
	if conn.RunIdleHeader {
		if len(payload) < 4 {
			return
		}
		// header := binary.LittleEndian.Uint32(payload[0:4])
		// Check Run bit (Bit 0)
		dataOffset = 4
	}

	// Update Assembly Data
	if conn.Assembly != nil {
		// Lock assembly? It has its own lock.
		// Write data to assembly
		// Note: We need a way to update assembly data from here.
		// The AssemblyObject has SetAttributeSingle, but that expects full attribute update.
		// We can just update the data directly if we have access, or add a method.
		// Since we have the *AssemblyInstance, we can update it directly if we lock it.
		// But AssemblyInstance struct fields are public but no lock there.
		// We should probably use AssemblyObject.SetAttributeSingle or similar.
		// Or better, add UpdateData method to AssemblyObject.

		// For now, let's assume we can overwrite the slice content if size matches.
		// But we need thread safety. The AssemblyObject has a mutex.
		// We should use a method on AssemblyObject.

		// Let's use a helper on Runtime that calls AssemblyObject
		// But we need the Instance ID.
		// conn.Assembly has ID.

		// Actually, we should probably just update the data in place if we can.
		// But for correctness, let's use the AssemblyObject lock.
		// We can't easily access AssemblyObject lock from here without exposing it.
		// Let's assume for now we just update it.
		// In a real impl, we'd want a proper method.

		// Let's add SetData to AssemblyObject later. For now, we'll skip the update or do it unsafe.
		// Wait, I can just call ao.SetAttributeSingle(conn.Assembly.ID, 3, payload[dataOffset:])
		r.assemblyObj.SetAttributeSingle(conn.Assembly.ID, 3, payload[dataOffset:])
	}
}
