package testimonial

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// stubBinary writes an executable shell script and returns its path.
//
// The point is to exercise the argument assembly and the error wrapping without
// requiring a working ffmpeg on the machine running the tests. The script
// records the arguments it was handed so a test can assert on the command line
// we actually build, which is the part that goes wrong in practice.
func stubBinary(t *testing.T, body string) (path, recorded string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stub relies on a POSIX shell")
	}
	dir := t.TempDir()
	path = filepath.Join(dir, "stub")
	recorded = filepath.Join(dir, "argv")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + recorded + "\n" + body
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	return path, recorded
}

func TestFFmpegBinariesDefaultToPath(t *testing.T) {
	t.Parallel()
	bare := FFmpeg{}
	if bare.ffmpeg() != "ffmpeg" || bare.ffprobe() != "ffprobe" {
		t.Fatalf("bare FFmpeg = %q/%q, want the names on PATH", bare.ffmpeg(), bare.ffprobe())
	}
	pinned := FFmpeg{FFmpegPath: "/opt/ffmpeg", FFprobePath: "/opt/ffprobe"}
	if pinned.ffmpeg() != "/opt/ffmpeg" || pinned.ffprobe() != "/opt/ffprobe" {
		t.Fatalf("pinned FFmpeg = %q/%q, want the configured paths", pinned.ffmpeg(), pinned.ffprobe())
	}
}

func TestParseProbe(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		output   string
		want     Probed
		wantFail bool
	}{
		{
			name:   "landscape with a duration",
			output: `{"streams":[{"width":1920,"height":1080}],"format":{"duration":"37.418000"}}`,
			want:   Probed{Dimensions: Dimensions{Width: 1920, Height: 1080}, DurationSeconds: 37},
		},
		{
			// ffprobe resolves the rotation flag for us, so an upright phone
			// recording arrives already described as upright.
			name:   "portrait",
			output: `{"streams":[{"width":1080,"height":1920}],"format":{"duration":"4.017"}}`,
			want:   Probed{Dimensions: Dimensions{Width: 1080, Height: 1920}, DurationSeconds: 4},
		},
		{
			name:   "duration ffprobe declined to report",
			output: `{"streams":[{"width":640,"height":480}],"format":{"duration":"N/A"}}`,
			want:   Probed{Dimensions: Dimensions{Width: 640, Height: 480}},
		},
		{
			name:     "audio only",
			output:   `{"streams":[],"format":{"duration":"12.0"}}`,
			wantFail: true,
		},
		{name: "not json at all", output: "ffprobe: command not found", wantFail: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseProbe([]byte(test.output))
			if test.wantFail {
				if err == nil {
					t.Fatalf("parseProbe(%q) = %#v, want an error", test.output, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseProbe: %v", err)
			}
			if got != test.want {
				t.Fatalf("parseProbe = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestFFmpegProbeAsksFFprobeForTheResolvedShape(t *testing.T) {
	t.Parallel()
	path, recorded := stubBinary(t,
		`printf '{"streams":[{"width":1080,"height":1920}],"format":{"duration":"4.0"}}'`+"\n")
	probed, err := FFmpeg{FFprobePath: path}.Probe(context.Background(), "/tmp/clip.mp4")
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if probed.Width != 1080 || probed.Height != 1920 || probed.DurationSeconds != 4 {
		t.Fatalf("Probe = %#v, want the stubbed portrait clip", probed)
	}
	argv := readFile(t, recorded)
	// Without -select_streams a file whose first stream is audio reports no
	// dimensions at all, and without the rotation-resolving JSON output a
	// portrait recording comes back as landscape.
	for _, want := range []string{"-select_streams", "v:0", "-of", "json", "/tmp/clip.mp4"} {
		if !strings.Contains(argv, want) {
			t.Errorf("probe arguments %q are missing %q", argv, want)
		}
	}
}

func TestFFmpegProbeSurfacesABinaryThatFailed(t *testing.T) {
	t.Parallel()
	path, _ := stubBinary(t, "exit 3\n")
	if _, err := (FFmpeg{FFprobePath: path}).Probe(context.Background(), "/tmp/clip.mp4"); err == nil {
		t.Fatal("a probe whose binary exits non-zero must fail")
	}
}

func TestFFmpegEncodeVideoBuildsTheRenditionCommand(t *testing.T) {
	t.Parallel()
	path, recorded := stubBinary(t, "")
	target := Dimensions{Width: 406, Height: 720}
	err := FFmpeg{FFmpegPath: path}.EncodeVideo(context.Background(), "/in.mov", "/out.mp4", target)
	if err != nil {
		t.Fatalf("EncodeVideo: %v", err)
	}
	argv := readFile(t, recorded)
	// faststart is the one that is invisible until it is missing: without it the
	// index sits at the end of the file and the embed shows a blank rectangle
	// until the whole download finishes.
	for _, want := range []string{
		"scale=406:720", "libx264", "yuv420p", "+faststart",
		"-fs", "209715200", "/in.mov", "/out.mp4",
	} {
		if !strings.Contains(argv, want) {
			t.Errorf("encode arguments %q are missing %q", argv, want)
		}
	}
}

func TestFFmpegExtractPosterSkipsTheFirstSecond(t *testing.T) {
	t.Parallel()
	path, recorded := stubBinary(t, "")
	if err := (FFmpeg{FFmpegPath: path}).ExtractPoster(context.Background(), "/in.mov", "/out.jpg"); err != nil {
		t.Fatalf("ExtractPoster: %v", err)
	}
	argv := readFile(t, recorded)
	// Frame zero of a phone recording is very often a blur or a pocket.
	if !strings.Contains(argv, "-ss") || !strings.Contains(argv, "1") {
		t.Errorf("poster arguments %q must seek past the opening frame", argv)
	}
	if !strings.Contains(argv, "/out.jpg") {
		t.Errorf("poster arguments %q are missing the destination", argv)
	}
}

func TestRunCarriesStderrIntoTheError(t *testing.T) {
	t.Parallel()
	path, _ := stubBinary(t, "echo 'moov atom not found' >&2\nexit 1\n")
	err := run(context.Background(), path)
	if err == nil {
		t.Fatal("a non-zero exit must be an error")
	}
	// The exit status alone says nothing a developer can act on; the diagnosis
	// is always on stderr.
	if !strings.Contains(err.Error(), "moov atom not found") {
		t.Fatalf("error %q must carry what the binary printed", err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}
