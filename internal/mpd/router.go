package mpd

import (
	"fmt"
	"strings"
)

// handleCommand processes a single MPD command
func (s *Server) handleCommand(line string) string {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return "OK\n"
	}

	command := strings.ToLower(parts[0])
	args := parts[1:]

	switch command {
	case "ping":
		return "OK\n"

	case "add":
		return s.cmdAdd(args)

	case "addid":
		return s.cmdAddId(args)

	case "play":
		return s.cmdPlay(args)

	case "pause":
		return s.cmdPause(args)

	case "stop":
		return s.cmdStop(args)

	case "next":
		return s.cmdNext(args)

	case "previous":
		return s.cmdPrevious(args)

	case "status":
		return s.cmdStatus(args)

	case "playlistinfo":
		return s.cmdPlaylistInfo(args)

	case "clear":
		return s.cmdClear(args)

	case "currentsong":
		return s.cmdCurrentSong(args)

	case "plchanges":
		return s.cmdPlChanges(args)

	case "tagtypes":
		return s.cmdTagTypes(args)

	case "outputs":
		return s.cmdOutputs(args)

	case "decoders":
		return s.cmdDecoders(args)

	case "single":
		return s.cmdSingle(args)

	case "consume":
		return s.cmdConsume(args)

	case "repeat":
		return s.cmdRepeat(args)

	case "random":
		return s.cmdRandom(args)

	case "close":
		return "" // Client will close connection

	default:
// 		log.Fatalf("Unknown MPD command received: %s (full line: %s)", command, line)
		return fmt.Sprintf("ACK [5@0] {%s} unknown command\n", command)
	}
}