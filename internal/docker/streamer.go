package docker

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
)

// LineHandler is called for each line of output from a container
type LineHandler func(source string, line string, timestamp time.Time, isStderr bool)

// ContainerStreamer streams logs from a Docker container
type ContainerStreamer struct {
	client      *Client
	containerID string
	name        string
	handler     LineHandler
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
}

// NewContainerStreamer creates a new streamer for the given container
func NewContainerStreamer(client *Client, containerID, name string, handler LineHandler) *ContainerStreamer {
	ctx, cancel := context.WithCancel(context.Background())

	return &ContainerStreamer{
		client:      client,
		containerID: containerID,
		name:        name,
		handler:     handler,
		ctx:         ctx,
		cancel:      cancel,
	}
}

// Start begins streaming logs from the container
func (s *ContainerStreamer) Start() error {
	// Get container logs
	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Timestamps: false,
		Since:      "0", // Stream from now
	}

	logStream, err := s.client.cli.ContainerLogs(s.ctx, s.containerID, options)
	if err != nil {
		return fmt.Errorf("failed to attach to container logs: %w", err)
	}

	// Start goroutine to read logs
	s.wg.Add(1)
	go s.streamLogs(logStream)

	return nil
}

// streamLogs reads from the log stream and forwards to handler
func (s *ContainerStreamer) streamLogs(stream io.ReadCloser) {
	defer s.wg.Done()
	defer stream.Close()

	// Docker multiplexes stdout/stderr in a single stream with headers
	// Header format: [8]byte{STREAM_TYPE, 0, 0, 0, SIZE1, SIZE2, SIZE3, SIZE4}
	// STREAM_TYPE: 0=stdin, 1=stdout, 2=stderr

	reader := bufio.NewReader(stream)

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		// Read header (8 bytes)
		header := make([]byte, 8)
		_, err := io.ReadFull(reader, header)
		if err != nil {
			if err != io.EOF {
				fmt.Fprintf(os.Stderr, "[running-man] Error reading container log header: %v\n", err)
			}
			return
		}

		// Parse header
		streamType := header[0]
		size := uint32(header[4])<<24 | uint32(header[5])<<16 | uint32(header[6])<<8 | uint32(header[7])

		// Read payload
		payload := make([]byte, size)
		_, err = io.ReadFull(reader, payload)
		if err != nil {
			if err != io.EOF {
				fmt.Fprintf(os.Stderr, "[running-man] Error reading container log payload: %v\n", err)
			}
			return
		}

		// Process line
		line := string(payload)
		// Remove trailing newline if present
		if len(line) > 0 && line[len(line)-1] == '\n' {
			line = line[:len(line)-1]
		}

		timestamp := time.Now()
		isStderr := streamType == 2

		// Pass-through to terminal with container name prefix
		if isStderr {
			fmt.Fprintf(os.Stderr, "[%s] %s\n", s.name, line)
		} else {
			fmt.Printf("[%s] %s\n", s.name, line)
		}

		// Call handler if provided
		if s.handler != nil {
			s.handler(s.name, line, timestamp, isStderr)
		}
	}
}

// Stop stops streaming logs
func (s *ContainerStreamer) Stop() error {
	s.cancel()
	return nil
}

// Wait waits for the streaming to complete
func (s *ContainerStreamer) Wait() error {
	s.wg.Wait()
	return nil
}
