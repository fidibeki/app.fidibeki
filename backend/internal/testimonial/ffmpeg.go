package testimonial

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Encoder turns an author's upload into the derivatives a wall can serve.
//
// It is an interface so the job that orchestrates it can be reasoned about and
// tested without a video file or a working ffmpeg on the machine.
type Encoder interface {
	Probe(ctx context.Context, path string) (Probed, error)
	EncodeVideo(ctx context.Context, source, destination string, target Dimensions) error
	ExtractPoster(ctx context.Context, source, destination string) error
}

// Probed is what the source file says about itself.
type Probed struct {
	Dimensions
	DurationSeconds int
}

// FFmpeg drives the binaries of the same name.
type FFmpeg struct {
	// FFmpegPath and FFprobePath default to the names on PATH.
	FFmpegPath  string
	FFprobePath string
}

func (f FFmpeg) ffmpeg() string {
	if f.FFmpegPath != "" {
		return f.FFmpegPath
	}
	return "ffmpeg"
}

func (f FFmpeg) ffprobe() string {
	if f.FFprobePath != "" {
		return f.FFprobePath
	}
	return "ffprobe"
}

// Probe reads the dimensions and duration without decoding the whole file.
//
// The rotation side data matters more than it looks: a phone records portrait
// footage as a landscape stream plus a 90-degree rotation flag, so the raw
// width and height describe a picture nobody will ever see. ffprobe is asked to
// resolve that for us, so what comes back is the shape a viewer gets.
func (f FFmpeg) Probe(ctx context.Context, path string) (Probed, error) {
	output, err := exec.CommandContext(ctx, f.ffprobe(),
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height:format=duration",
		"-of", "json",
		path,
	).Output()
	if err != nil {
		return Probed{}, fmt.Errorf("probe testimonial video: %w", err)
	}
	return parseProbe(output)
}

// parseProbe reads ffprobe's JSON.
//
// Split from the call that produces it so the interesting half — what we do
// with a file that has no video stream, or a duration ffprobe declined to
// report — can be tested without a video file.
func parseProbe(output []byte) (Probed, error) {
	var parsed struct {
		Streams []struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(output, &parsed); err != nil {
		return Probed{}, fmt.Errorf("decode probe output: %w", err)
	}
	if len(parsed.Streams) == 0 {
		return Probed{}, fmt.Errorf("probe testimonial video: no video stream")
	}
	probed := Probed{Dimensions: Dimensions{
		Width:  parsed.Streams[0].Width,
		Height: parsed.Streams[0].Height,
	}}
	// A stream ffprobe cannot time is not a stream we refuse: the duration is
	// advisory, and a zero here is better than failing an upload that plays.
	if seconds, parseErr := strconv.ParseFloat(strings.TrimSpace(parsed.Format.Duration), 64); parseErr == nil {
		probed.DurationSeconds = int(seconds)
	}
	return probed, nil
}

// EncodeVideo writes an H.264 rendition at the given size.
//
// faststart moves the index to the front of the file so a browser can begin
// playing before the whole thing has arrived, which on someone else's marketing
// page is the difference between a video and a blank rectangle.
func (f FFmpeg) EncodeVideo(ctx context.Context, source, destination string, target Dimensions) error {
	return run(ctx, f.ffmpeg(),
		"-v", "error", "-y",
		"-i", source,
		"-vf", fmt.Sprintf("scale=%d:%d", target.Width, target.Height),
		"-c:v", "libx264", "-preset", "medium", "-crf", "23",
		"-profile:v", "high", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", "128k",
		"-movflags", "+faststart",
		destination,
	)
}

// ExtractPoster grabs a still for the player to show before playback.
//
// Taken a second in rather than at zero: the first frame of a phone recording
// is very often a blur, or the inside of someone's pocket.
func (f FFmpeg) ExtractPoster(ctx context.Context, source, destination string) error {
	return run(ctx, f.ffmpeg(),
		"-v", "error", "-y",
		"-ss", "1",
		"-i", source,
		"-frames:v", "1",
		"-q:v", "3",
		destination,
	)
}

func run(ctx context.Context, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	var stderr strings.Builder
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("%s: %w (%s)", name, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
