package decoder

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// AudioFormat represents decoded audio format
type AudioFormat struct {
	SampleRate    int
	BitsPerSample int
	Channels      int
}

// ProbeFormat detects the native audio format of a file/URL using ffprobe
func ProbeFormat(source string) (*AudioFormat, error) {
	// Check if file exists first (for non-URL sources)
	if _, err := os.Stat(source); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file does not exist: %s", source)
		}
		return nil, fmt.Errorf("cannot access file: %w", err)
	}

	// Use bits_per_raw_sample which works for compressed formats like FLAC
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-print_format", "default=noprint_wrappers=1:nokey=1",
		"-select_streams", "a:0",
		"-show_entries", "stream=sample_rate,channels,bits_per_raw_sample",
		source,
	)

	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffprobe failed: %w\nstderr: %s", err, stderr.String())
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) < 2 {
		return nil, fmt.Errorf("unexpected ffprobe output")
	}

	sampleRate, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil {
		return nil, fmt.Errorf("invalid sample rate: %w", err)
	}

	channels, err := strconv.Atoi(strings.TrimSpace(lines[1]))
	if err != nil {
		return nil, fmt.Errorf("invalid channels: %w", err)
	}

	// bits_per_raw_sample gives the actual bit depth for compressed formats
	bitsPerSample := 16 // default to 16-bit if not available
	if len(lines) > 2 && lines[2] != "N/A" && strings.TrimSpace(lines[2]) != "" {
		if bps, err := strconv.Atoi(strings.TrimSpace(lines[2])); err == nil && bps > 0 {
			// 24-bit audio is typically stored in 32-bit containers
			if bps == 24 {
				bitsPerSample = 32
			} else {
				bitsPerSample = bps
			}
		}
	}

	return &AudioFormat{
		SampleRate:    sampleRate,
		BitsPerSample: bitsPerSample,
		Channels:      channels,
	}, nil
}

// DecodeToWAVFile decodes audio to a WAV file at the specified path.
//
// Note: WAV format has limited metadata support compared to formats like FLAC or MP3.
// WAV files only support INFO chunks for metadata, which may not preserve all tags.
// Title, artist, album, etc. may be lost or incomplete in the conversion.
//
// Returns the audio format.
func DecodeToWAVFile(source string, outputPath string) (*AudioFormat, error) {
	// First probe to get native format
	nativeFormat, err := ProbeFormat(source)
	if err != nil {
		return nil, fmt.Errorf("failed to probe audio format: %w", err)
	}

	// Build ffmpeg command to decode to WAV
	// Note: -map_metadata attempts to preserve metadata, but WAV format
	// only supports INFO chunks, so many tags may be lost
	args := []string{
		"-i", source,
		"-f", "wav",
		"-map_metadata", "0",     // Attempt to preserve metadata (limited by WAV format)
		"-write_id3v2", "1",      // Try to write ID3v2 tags if possible
		"-id3v2_version", "3",    // Use ID3v2.3 for better compatibility
		"-metadata:s:a:0", "encoder=ffmpeg", // Preserve stream metadata
		"-y",                     // Overwrite output file
		outputPath,
	}

	cmd := exec.Command("ffmpeg", args...)

	// Capture stderr for error messages
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		os.Remove(outputPath)
		return nil, fmt.Errorf("ffmpeg failed: %w\nstderr: %s", err, stderr.String())
	}

	return nativeFormat, nil
}
