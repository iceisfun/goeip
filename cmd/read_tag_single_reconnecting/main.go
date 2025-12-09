package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/iceisfun/goeip/internal"
	"github.com/iceisfun/goeip/pkg/client"
	"github.com/iceisfun/goeip/pkg/utils"
)

/*
Testing Network Interrupts with SOCAT
====================================

You can simulate network disconnections using `socat`. This allows you to
create a TCP proxy that you can kill and restart to test the ReconnectingClient.

1.  **Assume your PLC is at 192.168.1.10:44818**
2.  **Start `socat` to proxy local port 44818 to the PLC**:
    ```bash
    socat -v TCP4-LISTEN:44818,fork,reuseaddr TCP4:192.168.1.10:44818
    ```
    (You might need `sudo` to listen on 44818, or use a higher port like 8888)

    If you use port 8888:
    ```bash
    socat -v TCP4-LISTEN:8888,fork,reuseaddr TCP4:192.168.1.10:44818
    ```

3.  **Run this tool pointing to the proxy**:
    ```bash
    go run cmd/read_tag_single_reconnecting/main.go --addr localhost:8888 --tag MyTag
    ```

4.  **Interrupt the Connection**:
    - Kill the `socat` process (Ctrl+C).
    - Observe the tool reporting errors.
    - Restart `socat`.
    - Observe the tool recovering and reading data again.

5.  **Observe reconnecting behavior**:

$ go run ./cmd/read_tag_single_reconnecting/ --addr 127.0.0.1 --tag TheTestTag


	Connecting to 127.0.0.1...
	Reading tag 'TheTestTag' every second. Press Ctrl+C to stop.
	---------------------------------------------------------
	[12:53:40] #1 SUCCESS: 3 bytes
	00000000  c1 00 01                                          |...|

	[12:53:41] #2 SUCCESS: 3 bytes
	00000000  c1 00 01                                          |...|

	[12:53:42] #3 SUCCESS: 3 bytes
	00000000  c1 00 01                                          |...|

	[12:53:43] #4 SUCCESS: 3 bytes
	00000000  c1 00 01                                          |...|

	[12:53:47] #5 ERROR: max retries exceeded: dial tcp 127.0.0.1:44818: connect: connection refused
	[12:53:49] #6 ERROR: max retries exceeded: dial tcp 127.0.0.1:44818: connect: connection refused
	[12:53:50] #7 SUCCESS: 3 bytes
	00000000  c1 00 01                                          |...|

	[12:53:50] #8 SUCCESS: 3 bytes
	00000000  c1 00 01                                          |...|

	[12:53:51] #9 SUCCESS: 3 bytes
	00000000  c1 00 01                                          |...|

	[12:53:55] #10 ERROR: max retries exceeded: dial tcp 127.0.0.1:44818: connect: connection refused
	[12:53:57] #11 ERROR: max retries exceeded: dial tcp 127.0.0.1:44818: connect: connection refused
	[12:54:00] #12 ERROR: max retries exceeded: dial tcp 127.0.0.1:44818: connect: connection refused
	[12:54:01] #13 SUCCESS: 3 bytes
	00000000  c1 00 01                                          |...|

	[12:54:01] #14 SUCCESS: 3 bytes
	00000000  c1 00 01                                          |...|

	[12:54:01] #15 SUCCESS: 3 bytes
	00000000  c1 00 01                                          |...|

*/

func main() {
	addr := flag.String("addr", "localhost:44818", "PLC address")
	tagName := flag.String("tag", "", "Tag name to read")
	verbose := flag.Bool("v", false, "Verbose logging")
	flag.Parse()

	if *tagName == "" {
		fmt.Println("Error: --tag is required")
		flag.Usage()
		os.Exit(1)
	}

	logger := internal.NopLogger()
	if *verbose {
		logger = internal.NewConsoleLogger()
	}

	fmt.Printf("Connecting to %s...\n", *addr)

	// Create ReconnectingClient
	// We use a short retry delay for demo purposes to start faster after reconnect
	rc, err := client.NewReconnectingClient(*addr, logger,
		client.WithMaxRetries(5),
		client.WithRetryDelay(500*time.Millisecond),
	)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer rc.Close()

	fmt.Printf("Reading tag '%s' every second. Press Ctrl+C to stop.\n", *tagName)
	fmt.Println("---------------------------------------------------------")

	idx := 1
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			val, err := rc.ReadTag(*tagName)
			ts := time.Now().Format("15:04:05")

			if err != nil {
				// We just log the error. The client handles reconnection internally.
				fmt.Printf("[%s] #%d ERROR: %v\n", ts, idx, err)
			} else {
				// Try to format nicely if it's just raw bytes
				// The client returns raw response bytes including type code.
				// For display, hex dump works.
				fmt.Printf("[%s] #%d SUCCESS: %d bytes\n%s\n", ts, idx, len(val), utils.HexDump(val))
			}
			idx++
		}
	}
}
