package decoder

import (
	"bytes"
	"fmt"
	"io"
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

// Decoder handles audio decoding via ffmpeg
type Decoder struct {
	cmd    *exec.Cmd
	stdout io.ReadCloser
	stderr io.ReadCloser
	format *AudioFormat
}

// NewDecoder creates a decoder for the given URL/file
// It preserves the native format of the source audio
func NewDecoder(source string) (*Decoder, error) {
	// First probe to get native format
	nativeFormat, err := ProbeFormat(source)
	if err != nil {
		return nil, fmt.Errorf("failed to probe audio format: %w", err)
	}

	// Determine PCM format based on bit depth
	var pcmFormat string
	var codecFormat string

	switch nativeFormat.BitsPerSample {
	case 16:
		pcmFormat = "s16le"
		codecFormat = "pcm_s16le"
	case 24:
		pcmFormat = "s24le"
		codecFormat = "pcm_s24le"
	case 32:
		pcmFormat = "s32le"
		codecFormat = "pcm_s32le"
	default:
		// Default to 16-bit for unusual bit depths
		nativeFormat.BitsPerSample = 16
		pcmFormat = "s16le"
		codecFormat = "pcm_s16le"
	}

	// Build ffmpeg command to decode to raw PCM (preserving native format)
	args := []string{
		"-i", source,
		"-f", pcmFormat,
		"-acodec", codecFormat,
		"-ar", strconv.Itoa(nativeFormat.SampleRate),
		"-ac", strconv.Itoa(nativeFormat.Channels),
		"-",
	}

	cmd := exec.Command("ffmpeg", args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	return &Decoder{
		cmd:    cmd,
		stdout: stdout,
		stderr: stderr,
		format: nativeFormat,
	}, nil
}

// Read reads decoded PCM audio data
func (d *Decoder) Read(p []byte) (n int, err error) {
	return d.stdout.Read(p)
}

// Close stops the decoder
func (d *Decoder) Close() error {
	if d.cmd != nil && d.cmd.Process != nil {
		d.cmd.Process.Kill()
		return d.cmd.Wait()
	}
	return nil
}

// Format returns the audio format being produced
func (d *Decoder) Format() *AudioFormat {
	return d.format
}

// ProbeFormat detects the native audio format of a file/URL using ffprobe
func ProbeFormat(source string) (*AudioFormat, error) {
	cmd := exec.Command("ffprobe",
		"-v", "quiet",
		"-print_format", "default=noprint_wrappers=1:nokey=1",
		"-select_streams", "a:0",
		"-show_entries", "stream=sample_rate,channels,bits_per_sample",
		source,
	)

	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffprobe failed: %w", err)
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

	// bits_per_sample is often N/A for compressed formats
	bitsPerSample := 16 // default to 16-bit for compressed sources
	if len(lines) > 2 && lines[2] != "N/A" && strings.TrimSpace(lines[2]) != "" {
		if bps, err := strconv.Atoi(strings.TrimSpace(lines[2])); err == nil {
			bitsPerSample = bps
		}
	}

	return &AudioFormat{
		SampleRate:    sampleRate,
		BitsPerSample: bitsPerSample,
		Channels:      channels,
	}, nil
}
