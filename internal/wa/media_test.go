package wa

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestConvert(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	ctx := context.Background()
	dir := t.TempDir()
	src := map[string][]string{
		"image": {"-f", "lavfi", "-i", "testsrc=size=2400x1600", "-frames:v", "1", filepath.Join(dir, "in.png")},
		"video": {"-f", "lavfi", "-i", "testsrc=size=1920x1080:duration=3", "-f", "lavfi", "-i", "sine=duration=3", "-shortest", "-c:v", "libvpx-vp9", filepath.Join(dir, "in.webm")},
		"voice": {"-f", "lavfi", "-i", "sine=frequency=300:duration=4", filepath.Join(dir, "in.m4a")},
	}
	in := map[string]string{"image": "in.png", "video": "in.webm", "voice": "in.m4a"}
	for kind, args := range src {
		if err := ff(ctx, args...); err != nil {
			t.Fatalf("make %s: %v", kind, err)
		}
		m, err := Convert(ctx, kind, filepath.Join(dir, in[kind]))
		if err != nil {
			t.Fatalf("convert %s: %v", kind, err)
		}
		t.Logf("%s: %s %dx%d %ds thumb=%dB wave=%d", kind, m.Mime, m.Width, m.Height, m.Seconds, len(m.Thumb), len(m.Waveform))
		switch kind {
		case "image":
			if m.Width != 1600 || len(m.Thumb) == 0 {
				t.Errorf("image: want width 1600 and a thumb, got %d, %d", m.Width, len(m.Thumb))
			}
		case "video":
			if m.Width != 1280 || m.Seconds != 3 || len(m.Thumb) == 0 {
				t.Errorf("video: got %dx%d %ds thumb %d", m.Width, m.Height, m.Seconds, len(m.Thumb))
			}
		case "voice":
			if m.Seconds != 4 || len(m.Waveform) != 64 {
				t.Errorf("voice: got %ds wave %d", m.Seconds, len(m.Waveform))
			}
		}
	}
}
