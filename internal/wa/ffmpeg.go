package wa

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// What Townsquare asks ffmpeg to produce. Convert and CheckFFmpeg both use these, so
// the health check exercises exactly the encoders and filters real uploads use.

// imageEncode: JPEG, long edge at most 1600px (everything after the -i input).
func imageEncode(out string) []string {
	return []string{"-vf", "scale='min(1600,iw)':'min(1600,ih)':force_original_aspect_ratio=decrease", "-q:v", "3", out}
}

// videoEncode: H.264 + AAC MP4, faststart, long edge at most 1280px.
func videoEncode(out string) []string {
	return []string{"-vf", "scale='min(1280,iw)':'min(1280,ih)':force_original_aspect_ratio=decrease,scale=trunc(iw/2)*2:trunc(ih/2)*2",
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "24", "-pix_fmt", "yuv420p", "-profile:v", "main",
		"-c:a", "aac", "-b:a", "128k", "-movflags", "+faststart", out}
}

// voiceEncode: mono OGG/Opus 48 kHz (shows as a voice note).
func voiceEncode(out string) []string {
	return []string{"-vn", "-ac", "1", "-ar", "48000", "-c:a", "libopus", "-b:a", "32k", "-application", "voip", out}
}

// FFmpegStatus is the result of CheckFFmpeg.
type FFmpegStatus struct {
	OK        bool          `json:"ok"`
	Path      string        `json:"path"`              // ffmpeg that was tested ("" = none found)
	ProbePath string        `json:"ffprobe_path"`      // ffprobe that was tested
	Bundled   bool          `json:"bundled"`           // ffmpeg is Townsquare's own copy (downloaded by it), not PATH or Homebrew
	Version   string        `json:"version"`           // e.g. 9.0.2
	Error     string        `json:"error,omitempty"`   // why it failed, with ffmpeg's own words
	Checked   string        `json:"checked,omitempty"` // when, RFC 3339 UTC
	Install   FFmpegInstall `json:"install"`           // the download of Townsquare's own copy
}

var versionRE = regexp.MustCompile(`(?m)^ff(?:mpeg|probe) version (\S+)`)

// CheckFFmpeg runs a tiny real conversion with the ffmpeg Townsquare would use: a
// photo to JPEG, a one second video with sound to H.264/AAC, a voice note to
// Opus, and ffprobe on the video. It never touches user data.
func CheckFFmpeg(ctx context.Context) FFmpegStatus {
	st := FFmpegStatus{Checked: time.Now().UTC().Format(time.RFC3339)}
	st.Path, st.Bundled = ToolSource("ffmpeg")
	st.Install = CurrentInstall()
	st.ProbePath = Tool("ffprobe")
	fail := func(format string, a ...any) FFmpegStatus {
		st.Error = fmt.Sprintf(format, a...)
		return st
	}
	if st.Path == "" {
		return fail("%v", ErrNoFFmpeg)
	}
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, st.Path, "-hide_banner", "-version").CombinedOutput()
	if err != nil {
		return fail("%s doesn't start: %v: %s", st.Path, err, strings.TrimSpace(string(out)))
	}
	if m := versionRE.FindSubmatch(out); m != nil {
		st.Version = string(m[1])
	} else {
		return fail("%s ran but isn't ffmpeg: %s", st.Path, firstLine(string(out)))
	}

	dir, err := os.MkdirTemp("", "townsquare-ffcheck-")
	if err != nil {
		return fail("%v", err)
	}
	defer os.RemoveAll(dir)

	// Photo: PNG in (written here, so decoding is tested too), Townsquare's JPEG out.
	src := filepath.Join(dir, "in.png")
	img := image.NewNRGBA(image.Rect(0, 0, 96, 64))
	for x := range 96 {
		for y := range 64 {
			img.SetNRGBA(x, y, color.NRGBA{uint8(x * 2), uint8(y * 3), 128, 255})
		}
	}
	f, err := os.Create(src)
	if err != nil {
		return fail("%v", err)
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return fail("%v", err)
	}
	if err := f.Close(); err != nil {
		return fail("%v", err)
	}
	jpg := filepath.Join(dir, "image.jpg")
	if err := ff(ctx, append([]string{"-i", src}, imageEncode(jpg)...)...); err != nil {
		return fail("photo to JPEG failed: %v", err)
	}
	if !nonEmpty(jpg) {
		return fail("photo to JPEG wrote no file")
	}

	// Video: one second of picture and sound through the real H.264 + AAC settings.
	mp4 := filepath.Join(dir, "video.mp4")
	if err := ff(ctx, append([]string{"-f", "lavfi", "-i", "testsrc=size=320x240:rate=25:duration=1",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1", "-shortest"}, videoEncode(mp4)...)...); err != nil {
		return fail("1 second video (H.264 + AAC) failed: %v", err)
	}
	if !nonEmpty(mp4) {
		return fail("1 second video wrote no file")
	}

	// Voice note: Opus in OGG.
	ogg := filepath.Join(dir, "voice.ogg")
	if err := ff(ctx, append([]string{"-f", "lavfi", "-i", "sine=frequency=300:duration=1"}, voiceEncode(ogg)...)...); err != nil {
		return fail("voice note (Opus) failed: %v", err)
	}
	if !nonEmpty(ogg) {
		return fail("voice note wrote no file")
	}

	// ffprobe reads the video back.
	m := &Media{Path: mp4}
	if err := probeErr(ctx, m); err != nil {
		return fail("%v", err)
	}
	if m.Width != 320 || m.Height != 240 {
		return fail("ffprobe read the test video as %dx%d, expected 320x240", m.Width, m.Height)
	}
	st.OK = true
	return st
}

func nonEmpty(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Size() > 0
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}
