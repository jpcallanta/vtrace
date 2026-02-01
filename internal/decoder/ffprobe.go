package decoder

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

var (
	ErrNoFramesFound   = errors.New("no video frames found in segment")
	ErrFFprobeNotFound = errors.New("ffprobe not found in PATH")
)

// Frame represents a video frame from ffprobe output
type Frame struct {
	MediaType string `json:"media_type"`
	KeyFrame  int    `json:"key_frame"`
	PtsTime   string `json:"pts_time"`
	PktPos    string `json:"pkt_pos"`
}

// FFprobeOutput represents the JSON output from ffprobe
type FFprobeOutput struct {
	Frames []Frame `json:"frames"`
}

// DetectFirstFrame pipes segment data to ffprobe and detects the first video frame
func DetectFirstFrame(ctx context.Context, segmentData []byte) (time.Duration, error) {
	// Check if ffprobe is available
	if _, err := exec.LookPath("ffprobe"); err != nil {
		return 0, ErrFFprobeNotFound
	}

	start := time.Now()

	// Build ffprobe command
	cmd := exec.CommandContext(ctx,
		"ffprobe",
		"-show_frames",
		"-select_streams", "v:0",
		"-print_format", "json",
		"-read_intervals", "%+#1",
		"-i", "pipe:0",
	)

	// Set up pipes
	cmd.Stdin = bytes.NewReader(segmentData)

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run ffprobe
	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("ffprobe failed: %w (stderr: %s)", err, stderr.String())
	}

	elapsed := time.Since(start)

	// Parse JSON output
	var output FFprobeOutput

	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		return 0, fmt.Errorf("failed to parse ffprobe output: %w", err)
	}

	// Check if we found any frames
	if len(output.Frames) == 0 {
		return 0, ErrNoFramesFound
	}

	return elapsed, nil
}

// CheckFFprobe verifies that ffprobe is available
func CheckFFprobe() error {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		return ErrFFprobeNotFound
	}

	return nil
}
