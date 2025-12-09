package main

import (
	"flag"
	"os"

	"github.com/iceisfun/goeip/internal"
	"github.com/iceisfun/goeip/pkg/client"
	"github.com/iceisfun/goeip/pkg/utils"
)

func main() {
	address := flag.String("addr", "192.168.1.10:44818", "PLC Address (IP:Port)")
	tagName := flag.String("tag", "TestTag", "Tag Name to read")
	flag.Parse()

	logger := internal.NewConsoleLogger()
	logger.Infof("Connecting to %s...", *address)

	c, err := client.NewClient(*address, logger)
	if err != nil {
		logger.Errorf("Failed to connect: %v", err)
		os.Exit(1)
	}
	defer c.Close()

	logger.Infof("Reading tag '%s'...", *tagName)
	data, err := c.ReadTag(*tagName)
	if err != nil {
		logger.Errorf("Failed to read tag: %v", err)
		os.Exit(1)
	}

	logger.Infof("Read success! Data:\n%s", utils.HexDump(data))
}
