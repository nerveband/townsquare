package wa

import (
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// Photos must work on installs without ffmpeg (the Mac app and Windows exe don't ship it).
func TestGoImageWithoutFFmpeg(t *testing.T) {
	dir := t.TempDir()
	src := image.NewNRGBA(image.Rect(0, 0, 3000, 1500))
	for x := 0; x < 3000; x++ {
		src.Set(x, x%1500, color.NRGBA{200, 30, 30, 255})
	}
	in := filepath.Join(dir, "in.png")
	f, _ := os.Create(in)
	_ = png.Encode(f, src)
	f.Close()
	out := filepath.Join(dir, "out.jpg")
	w, h, err := goImage(in, out, 1600, 85)
	if err != nil || w != 1600 || h != 800 {
		t.Fatalf("goImage = %d x %d, %v", w, h, err)
	}
	o, _ := os.Open(out)
	defer o.Close()
	if c, err := jpeg.DecodeConfig(o); err != nil || c.Width != 1600 {
		t.Fatalf("output: %+v %v", c, err)
	}
	if _, _, err := goImage(filepath.Join(dir, "missing.png"), out, 100, 80); err == nil {
		t.Fatal("missing file accepted")
	}
	_ = os.WriteFile(filepath.Join(dir, "x.heic"), []byte("not an image"), 0o600)
	if _, _, err := goImage(filepath.Join(dir, "x.heic"), out, 100, 80); err == nil {
		t.Fatal("unreadable format accepted")
	}
}
