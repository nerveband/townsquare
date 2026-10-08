package wa

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Media is a file ready for WhatsApp plus the metadata the message needs.
type Media struct {
	Path     string
	Mime     string
	Width    int
	Height   int
	Seconds  int
	Thumb    []byte // small JPEG preview for images and videos
	Waveform []byte // 64 samples, 0..100, for voice notes
}

// Convert prepares a file for WhatsApp using ffmpeg:
//   - image: JPEG, long edge at most 1600px
//   - video: H.264 + AAC MP4, faststart, long edge at most 1280px
//   - voice: mono OGG/Opus 48 kHz (shows as a voice note)
//   - audio, document: unchanged
func Convert(ctx context.Context, kind, in string) (*Media, error) {
	if _, err := os.Stat(in); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "townsquare-")
	if err != nil {
		return nil, err
	}
	m := &Media{}
	switch kind {
	case "image":
		m.Path, m.Mime = filepath.Join(dir, "image.jpg"), "image/jpeg"
		if Tool("ffmpeg") == "" {
			if m.Width, m.Height, err = goImage(in, m.Path, 1600, 85); err != nil {
				return nil, err
			}
			break
		}
		if err := ff(ctx, "-i", in, "-vf", "scale='min(1600,iw)':'min(1600,ih)':force_original_aspect_ratio=decrease", "-q:v", "3", m.Path); err != nil {
			return nil, err
		}
	case "video":
		if Tool("ffmpeg") == "" {
			return nil, ErrNoFFmpeg
		}
		m.Path, m.Mime = filepath.Join(dir, "video.mp4"), "video/mp4"
		if err := ff(ctx, "-i", in, "-vf", "scale='min(1280,iw)':'min(1280,ih)':force_original_aspect_ratio=decrease,scale=trunc(iw/2)*2:trunc(ih/2)*2",
			"-c:v", "libx264", "-preset", "veryfast", "-crf", "24", "-pix_fmt", "yuv420p", "-profile:v", "main",
			"-c:a", "aac", "-b:a", "128k", "-movflags", "+faststart", m.Path); err != nil {
			return nil, err
		}
	case "voice":
		if Tool("ffmpeg") == "" {
			return nil, ErrNoFFmpeg
		}
		m.Path, m.Mime = filepath.Join(dir, "voice.ogg"), "audio/ogg; codecs=opus"
		if err := ff(ctx, "-i", in, "-vn", "-ac", "1", "-ar", "48000", "-c:a", "libopus", "-b:a", "32k", "-application", "voip", m.Path); err != nil {
			return nil, err
		}
	case "audio", "document":
		m.Path = in
		m.Mime = mime.TypeByExtension(strings.ToLower(filepath.Ext(in)))
		if m.Mime == "" {
			m.Mime = "application/octet-stream"
		}
	default:
		return nil, fmt.Errorf("unknown kind %q", kind)
	}

	if err := describe(ctx, kind, m); err != nil {
		return nil, err
	}
	return m, nil
}

// Describe reads metadata, a WhatsApp thumbnail and a waveform for an
// already-converted file (as produced by Convert).
func Describe(ctx context.Context, kind, path, mimeType string) (*Media, error) {
	m := &Media{Path: path, Mime: mimeType}
	return m, describe(ctx, kind, m)
}

func describe(ctx context.Context, kind string, m *Media) error {
	dir, err := os.MkdirTemp("", "townsquare-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	if kind != "document" {
		probe(ctx, m)
	}
	if kind == "image" || kind == "video" {
		thumb := filepath.Join(dir, "thumb.jpg")
		args := []string{"-i", m.Path, "-frames:v", "1", "-vf", "scale=96:96:force_original_aspect_ratio=decrease", "-q:v", "6", thumb}
		if kind == "video" {
			args = append([]string{"-ss", "0.5"}, args...)
		}
		if ff(ctx, args...) == nil {
			m.Thumb, _ = os.ReadFile(thumb)
		} else if kind == "image" {
			if w, h, err := goImage(m.Path, thumb, 96, 60); err == nil {
				m.Thumb, _ = os.ReadFile(thumb)
				if m.Width == 0 {
					m.Width, m.Height = probeGo(m.Path, w, h)
				}
			}
		}
	}
	if kind == "voice" {
		m.Waveform = waveform(ctx, m.Path)
	}
	return nil
}

// Preview writes a JPEG preview (long edge 640px) of an image or video for the UI.
func Preview(ctx context.Context, kind, in, out string) error {
	args := []string{"-i", in, "-frames:v", "1", "-vf", "scale='min(640,iw)':'min(640,ih)':force_original_aspect_ratio=decrease", "-q:v", "4", out}
	if kind == "video" {
		args = append([]string{"-ss", "0.5"}, args...)
	}
	if kind == "image" && Tool("ffmpeg") == "" {
		_, _, err := goImage(in, out, 640, 80)
		return err
	}
	return ff(ctx, args...)
}

func ff(ctx context.Context, args ...string) error {
	full := append([]string{"-hide_banner", "-loglevel", "error", "-y"}, args...)
	bin := Tool("ffmpeg")
	if bin == "" {
		return ErrNoFFmpeg
	}
	out, err := exec.CommandContext(ctx, bin, full...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func probe(ctx context.Context, m *Media) {
	bin := Tool("ffprobe")
	if bin == "" {
		return
	}
	out, err := exec.CommandContext(ctx, bin, "-v", "error", "-show_entries", "format=duration:stream=width,height",
		"-of", "json", m.Path).Output()
	if err != nil {
		return
	}
	var p struct {
		Streams []struct{ Width, Height int } `json:"streams"`
		Format  struct{ Duration string }     `json:"format"`
	}
	if json.Unmarshal(out, &p) != nil {
		return
	}
	for _, s := range p.Streams {
		if s.Width > 0 {
			m.Width, m.Height = s.Width, s.Height
			break
		}
	}
	if d, err := strconv.ParseFloat(p.Format.Duration, 64); err == nil {
		m.Seconds = int(math.Max(1, math.Round(d)))
	}
}

// waveform returns 64 loudness samples (0..100) for the voice-note bubble.
func waveform(ctx context.Context, path string) []byte {
	bin := Tool("ffmpeg")
	if bin == "" {
		return nil
	}
	out, err := exec.CommandContext(ctx, bin, "-hide_banner", "-loglevel", "error", "-i", path,
		"-ac", "1", "-ar", "8000", "-f", "s16le", "-").Output()
	if err != nil || len(out) < 128 {
		return nil
	}
	samples := make([]int16, len(out)/2)
	_ = binary.Read(bytes.NewReader(out), binary.LittleEndian, samples)
	const n = 64
	wf := make([]byte, n)
	chunk := len(samples) / n
	var peak float64
	vals := make([]float64, n)
	for i := 0; i < n; i++ {
		var sum float64
		for _, s := range samples[i*chunk : (i+1)*chunk] {
			sum += math.Abs(float64(s))
		}
		vals[i] = sum / float64(chunk)
		peak = math.Max(peak, vals[i])
	}
	for i, v := range vals {
		if peak > 0 {
			wf[i] = byte(v / peak * 100)
		}
	}
	return wf
}
