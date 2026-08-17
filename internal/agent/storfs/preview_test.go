package storfs

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/knot-infra/knot/pkg/protocol"
)

func TestReadPreviewTextBoundedAndCachedOutsideStorage(t *testing.T) {
	root := t.TempDir()
	cache := t.TempDir()
	t.Setenv("KNOT_PREVIEW_DIR", cache)
	if err := os.MkdirAll(filepath.Join(root, "projects"), 0o700); err != nil {
		t.Fatal(err)
	}
	body := strings.Repeat("hello\n", 20000)
	if err := os.WriteFile(filepath.Join(root, "projects", "notes.txt"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := New(root, func(any) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	res := m.readPreview("projects/notes.txt", "preview", 0)
	if !res.OK {
		t.Fatalf("preview failed: %s", res.Error)
	}
	raw, err := base64.StdEncoding.DecodeString(res.DataB64)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > int(maxTextPreviewBytes) {
		t.Fatalf("preview not bounded: %d", len(raw))
	}
	if _, err := os.Stat(filepath.Join(root, ".knot")); !os.IsNotExist(err) {
		t.Fatalf("preview cache leaked into storage root: err=%v", err)
	}
	var cacheFiles []string
	_ = filepath.Walk(cache, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			cacheFiles = append(cacheFiles, p)
		}
		return nil
	})
	if len(cacheFiles) == 0 {
		t.Fatal("expected preview cache file")
	}
}

func TestReadPreviewImageThumbnail(t *testing.T) {
	root := t.TempDir()
	cache := t.TempDir()
	t.Setenv("KNOT_PREVIEW_DIR", cache)
	if err := os.MkdirAll(filepath.Join(root, "photos"), 0o700); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 800, 600))
	for y := 0; y < 600; y++ {
		for x := 0; x < 800; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 255), G: uint8(y % 255), B: 120, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "photos", "image.png"), buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := New(root, func(any) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	res := m.readPreview("photos/image.png", "thumb", 128)
	if !res.OK {
		t.Fatalf("thumb failed: %s", res.Error)
	}
	if got, want := res.MimeType, "image/jpeg"; got != want {
		t.Fatalf("mime=%q want %q", got, want)
	}
	raw, err := base64.StdEncoding.DecodeString(res.DataB64)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width > 128 || cfg.Height > 128 {
		t.Fatalf("thumb too large: %dx%d", cfg.Width, cfg.Height)
	}
}

func TestPreviewUnsupportedGenericFile(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KNOT_PREVIEW_DIR", t.TempDir())
	if err := os.MkdirAll(filepath.Join(root, "db"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "db", "data.db"), []byte{0x00, 0x01, 0xff, 0x10, 0x80, 0x44}, 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := New(root, func(any) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	res := m.readPreview("db/data.db", "thumb", 128)
	if res.OK || !strings.Contains(res.Error, "unsupported") {
		t.Fatalf("expected unsupported preview, got ok=%v err=%q", res.OK, res.Error)
	}
}

var _ = protocol.StorageOpPreview
