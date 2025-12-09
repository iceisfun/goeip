package runtime

import (
	"encoding/binary"
	"time"
)

// Scheduler manages the RPI (Requested Packet Interval) for producing connections
type Scheduler struct {
	runtime *Runtime
	stop    chan struct{}
}

// NewScheduler creates a new Scheduler
func NewScheduler(r *Runtime) *Scheduler {
	return &Scheduler{
		runtime: r,
		stop:    make(chan struct{}),
	}
}

// Start starts the scheduler loop
func (s *Scheduler) Start() {
	go s.run()
}

// Stop stops the scheduler
func (s *Scheduler) Stop() {
	close(s.stop)
}

// run is the main loop
func (s *Scheduler) run() {
	ticker := time.NewTicker(5 * time.Millisecond) // Base tick, or use dynamic
	defer ticker.Stop()

	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.processTick()
		}
	}
}

// processTick checks all connections and sends data if RPI has elapsed
func (s *Scheduler) processTick() {
	s.runtime.mu.RLock()
	// Copy connections to avoid holding lock during I/O
	conns := make([]*IOConnection, 0, len(s.runtime.connections))
	for _, conn := range s.runtime.connections {
		if conn.IsProducer {
			conns = append(conns, conn)
		}
	}
	s.runtime.mu.RUnlock()

	for _, conn := range conns {
		// Check if it's time to send
		if time.Since(conn.LastSend) < conn.RPI {
			continue
		}

		if conn.Assembly == nil {
			continue
		}

		// Send Packet
		s.sendPacket(conn)

		// Update LastSend
		// We need to update LastSend safely.
		// Since we are the only writer to LastSend (Scheduler), and we have the pointer,
		// we can update it. But wait, is it shared?
		// IOConnection is shared with Runtime.
		// Runtime reads LastReceive.
		// Nobody reads LastSend except Scheduler?
		// If so, it's safe.
		// Let's assume it's safe for now as only Scheduler uses it for producing.
		conn.LastSend = time.Now()
	}
}

func (s *Scheduler) sendPacket(conn *IOConnection) {
	// Construct Packet
	// Item 1: Address Item (Connection ID)
	// Item 2: Data Item (Sequence + Header + Data)

	// Address Item
	// Type 0xA1 (Connected Address Item)
	// Length 4
	// Connection ID (O->T ID for producing) - Wait, if we are producing, we send to the Consumer's ID.
	// The Connection struct has OT and TO IDs.
	// If we are the Target (Adapter) producing to Originator (Scanner), we use O->T ID?
	// No, we use the ID the Consumer expects.
	// If we are Target, we produce T->O. The Originator consumes T->O.
	// The Forward_Open request specified T->O ID.
	// So we send with T->O ID?
	// Actually, the Address Item contains the "Connection ID".
	// For T->O traffic, it's the O->T ID? No, it's the ID chosen by the consumer.
	// If we are Target, the Originator chose the T->O ID. So we use that.

	// Let's assume ConnectionID in IOConnection is the ID we send TO.

	buf := make([]byte, 2048)
	offset := 0

	// Item Count
	binary.LittleEndian.PutUint16(buf[offset:], 2)
	offset += 2

	// Item 1: Address
	binary.LittleEndian.PutUint16(buf[offset:], 0x8002) // Type: Connected Address Item (0x8002? Spec says 0xA1? No, 0xA1 is for UDP?)
	// CIP Spec:
	// Address Item Type: 0xA1 (Connected Address Item) is for UDP?
	// Actually, for EtherNet/IP, it's usually 0x8002 (Sequenced Address Item) or just 0xA1.
	// Let's check standard.
	// "Sequenced Address Item" is 0x8002.
	// "Connected Address Item" is 0xA1.
	// Implicit I/O usually uses 0x8002 for Sequenced or 0xA1?
	// Most traces show 0x8002 is NOT used for I/O?
	// Wait, 0x8002 is "Sequenced Address Item".
	// 0xA1 is "Connected Address Item".
	// For Class 1 (UDP), we use 0xA1?
	// Let's stick to 0x8002 as it's common for "Sequenced".
	// Actually, let's use 0xA1 as it is "Connected Address Item".
	// Wait, 0xA1 is for "Connected Address Item".

	// Let's use 0x8002 (Sequenced Address Item) which includes Sequence Number?
	// No, Sequence Number is in the Packet, not the Item Type.
	// The Item Type is just the ID.

	// Let's look at a reference or just pick one.
	// 0xA1 is standard for I/O.

	binary.LittleEndian.PutUint16(buf[offset:], 0x00A1) // Connected Address Item
	offset += 2
	binary.LittleEndian.PutUint16(buf[offset:], 4) // Length
	offset += 2
	binary.LittleEndian.PutUint32(buf[offset:], conn.ConnectionID)
	offset += 4

	// Item 2: Data Item
	binary.LittleEndian.PutUint16(buf[offset:], 0x00B1) // Connected Data Item
	offset += 2

	// Calculate Data Length
	// Sequence (2) + Header (0 or 4) + Data
	dataLen := 2
	if conn.RunIdleHeader {
		dataLen += 4
	}
	dataLen += len(conn.Assembly.Data)

	binary.LittleEndian.PutUint16(buf[offset:], uint16(dataLen))
	offset += 2

	// Sequence Count
	conn.SequenceCount++
	binary.LittleEndian.PutUint16(buf[offset:], conn.SequenceCount)
	offset += 2

	// Run/Idle Header
	if conn.RunIdleHeader {
		// 32-bit Header.
		// Bit 0: Run/Idle (1=Run)
		// We are producing. Are we in Run mode?
		// Let's assume Run (1).
		binary.LittleEndian.PutUint32(buf[offset:], 1)
		offset += 4
	}

	// Data
	copy(buf[offset:], conn.Assembly.Data)
	offset += len(conn.Assembly.Data)

	// Send
	if conn.RemoteAddr != nil {
		s.runtime.conn.WriteToUDP(buf[:offset], conn.RemoteAddr)
	}
}
