package wa

import (
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	_ "image/gif"
	_ "image/png"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// ErrNoFFmpeg means a video or voice note needs ffmpeg, which isn't installed.
var ErrNoFFmpeg = errors.New("videos and voice notes need ffmpeg, which isn't installed. " +
	"Get it from https://ffmpeg.org/download.html (Mac: brew install ffmpeg, Windows: winget install ffmpeg, " +
	"Linux: sudo apt install ffmpeg), then try again. Photos and files work without it")

// Tool finds ffmpeg or ffprobe. Apps opened from Finder or the Start menu get a
// short PATH, so common install folders are checked too. "" = not installed.
func Tool(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	var dirs []string
	switch runtime.GOOS {
	case "darwin":
		dirs = []string{"/opt/homebrew/bin", "/usr/local/bin", "/opt/local/bin"}
	case "windows":
		name += ".exe"
		dirs = []string{filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "WinGet", "Links"),
			filepath.Join(os.Getenv("ProgramFiles"), "ffmpeg", "bin"), `C:\ffmpeg\bin`, filepath.Join(os.Getenv("USERPROFILE"), "scoop", "shims")}
	default:
		dirs = []string{"/usr/bin", "/usr/local/bin", "/snap/bin"}
	}
	for _, d := range dirs {
		p := filepath.Join(d, name)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}

// goImage converts a photo without ffmpeg: JPEG, PNG, GIF or WebP in, a JPEG
// whose long edge is at most maxEdge out. It returns the new size.
func goImage(in, out string, maxEdge, quality int) (int, int, error) {
	f, err := os.Open(in)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	src, _, err := image.Decode(f)
	if err != nil {
		return 0, 0, fmt.Errorf("can't read this photo (%v); use JPEG, PNG, GIF or WebP, or install ffmpeg for other formats", err)
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w > maxEdge || h > maxEdge {
		if w >= h {
			w, h = maxEdge, max(1, h*maxEdge/w)
		} else {
			w, h = max(1, w*maxEdge/h), maxEdge
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src) // flatten transparency onto white
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)
	o, err := os.Create(out)
	if err != nil {
		return 0, 0, err
	}
	if err := jpeg.Encode(o, dst, &jpeg.Options{Quality: quality}); err != nil {
		o.Close()
		return 0, 0, err
	}
	return w, h, o.Close()
}

// probeGo reads a photo's size without ffprobe (fallback: the given size).
func probeGo(path string, w, h int) (int, int) {
	f, err := os.Open(path)
	if err != nil {
		return w, h
	}
	defer f.Close()
	if c, _, err := image.DecodeConfig(f); err == nil {
		return c.Width, c.Height
	}
	return w, h
}
