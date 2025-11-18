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

// DecodeToAIFFFile decodes audio to an AIFF file at the specified path.
//
// Note: AIFF format has better metadata support than WAV.
// AIFF supports ID3 tags and other metadata formats, which helps preserve
// title, artist, album, and other tags from the source audio.
//
// Returns the audio format.
func DecodeToAIFFFile(source string, outputPath string) (*AudioFormat, error) {
	// First probe to get native format
	nativeFormat, err := ProbeFormat(source)
	if err != nil {
		return nil, fmt.Errorf("failed to probe audio format: %w", err)
	}

	// Build ffmpeg command to decode to AIFF
	// AIFF supports better metadata preservation than WAV
	args := []string{
		"-i", source,
		"-f", "aiff",
		"-map_metadata", "0",     // Preserve metadata (AIFF supports ID3 tags)
		"-write_id3v2", "1",      // Write ID3v2 tags
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
