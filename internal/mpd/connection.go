package mpd

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
)

// handleConnection handles a single MPD client connection
func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	log.Printf("New MPD client connected: %s", conn.RemoteAddr())

	// Send MPD greeting
	fmt.Fprintf(conn, "OK MPD 0.25.0\n")

	// Create connection-specific idle state
	var currentIdle *idleConnection
	var idleMu sync.Mutex

	scanner := bufio.NewScanner(conn)
	inCommandList := false
	commandListOk := false // Track if we need list_OK after each command
	var commandListResponses strings.Builder

	// Cleanup idle connection on disconnect
	defer func() {
		idleMu.Lock()
		if currentIdle != nil {
			s.unregisterIdle(currentIdle)
		}
		idleMu.Unlock()
	}()

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		log.Printf("MPD command: %s", line)

		// Handle command list mode
		if line == "command_list_begin" {
			inCommandList = true
			commandListOk = false
			commandListResponses.Reset()
			continue
		}

		if line == "command_list_ok_begin" {
			inCommandList = true
			commandListOk = true
			commandListResponses.Reset()
			continue
		}

		if line == "command_list_end" {
			if inCommandList {
				// Send all buffered responses
				fmt.Fprint(conn, commandListResponses.String())
				fmt.Fprint(conn, "OK\n")
				inCommandList = false
				commandListOk = false
				commandListResponses.Reset()
			}
			continue
		}

		// Check for idle/noidle commands which need special handling
		parts := strings.Fields(line)
		var response string

		if len(parts) > 0 {
			cmd := strings.ToLower(parts[0])
			args := parts[1:]

			if cmd == "idle" {
				// Enter idle mode
				idleMu.Lock()
				if currentIdle != nil {
					// Already in idle mode, ignore
					idleMu.Unlock()
					continue
				}

				// Parse subsystems to watch
				subsystems := make(map[string]bool)
				for _, arg := range args {
					subsystems[strings.ToLower(arg)] = true
				}

				// Create idle connection
				idle := &idleConnection{
					subsystems: subsystems,
					notify:     make(chan string, 10),
					cancel:     make(chan struct{}),
				}
				currentIdle = idle
				s.registerIdle(idle)
				idleMu.Unlock()

				// Wait for notification or cancel
				select {
				case subsystem := <-idle.notify:
					response = fmt.Sprintf("changed: %s\nOK\n", subsystem)
				case <-idle.cancel:
					response = "OK\n"
				}

				// Cleanup
				idleMu.Lock()
				s.unregisterIdle(idle)
				currentIdle = nil
				idleMu.Unlock()

			} else if cmd == "noidle" {
				// Exit idle mode
				idleMu.Lock()
				if currentIdle != nil {
					close(currentIdle.cancel)
					// Don't send response here - idle will send it
					idleMu.Unlock()
					continue
				}
				idleMu.Unlock()
				response = "OK\n"

			} else {
				// Normal command processing
				response = s.handleCommand(line)
			}
		} else {
			response = s.handleCommand(line)
		}

		log.Printf(response)

		if inCommandList {
			// Buffer response (strip the final OK)
			if strings.HasSuffix(response, "OK\n") {
				response = strings.TrimSuffix(response, "OK\n")
			}
			commandListResponses.WriteString(response)

			// For command_list_ok_begin, add list_OK after each command
			if commandListOk {
				commandListResponses.WriteString("list_OK\n")
			}
		} else {
			// Send response immediately
			fmt.Fprint(conn, response)
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("Connection error: %v", err)
	}

	log.Printf("MPD client disconnected: %s", conn.RemoteAddr())
}