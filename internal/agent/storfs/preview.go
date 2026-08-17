package storfs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/knot-infra/knot/internal/storage/pathsafe"
	"github.com/knot-infra/knot/pkg/protocol"
	"golang.org/x/image/webp"
)

const (
	maxTextPreviewBytes  int64 = 64 << 10
	maxImageSourceBytes  int64 = 64 << 20
	maxVideoSourceBytes  int64 = 512 << 20
	maxPDFSourceBytes    int64 = 128 << 20
	defaultThumbPixels         = 256
	defaultPreviewPixels       = 1600
)

func previewRoot() string {
	if v := os.Getenv("KNOT_PREVIEW_DIR"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "./knot-previews"
	}
	return filepath.Join(home, ".knot", "previews")
}

func previewGC(root string) {
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil && time.Since(info.ModTime()) > 7*24*time.Hour {
			_ = os.Remove(p)
		}
		return nil
	})
}

func (m *Manager) readPreview(rel, variant string, maxPixels int) protocol.StorageOpResult {
	if rel == "" || rel == "." {
		return fail(fmt.Errorf("path required"))
	}
	if variant == "" {
		variant = "preview"
	}
	if variant != "thumb" && variant != "preview" {
		return fail(fmt.Errorf("preview must be thumb or preview"))
	}
	if maxPixels <= 0 {
		if variant == "thumb" {
			maxPixels = defaultThumbPixels
		} else {
			maxPixels = defaultPreviewPixels
		}
	}
	full, err := pathsafe.ResolveUnderRoot(m.Root, rel)
	if err != nil {
		return fail(err)
	}
	fi, err := os.Stat(full)
	if err != nil {
		return fail(err)
	}
	if fi.IsDir() {
		return fail(fmt.Errorf("path is a directory"))
	}
	canon, _ := pathsafe.CanonicalRel(rel)
	mimeType := detectMime(path.Base(canon), full)
	sum, err := fileSHA256(full)
	if err != nil {
		return fail(err)
	}
	cacheKey := previewCacheKey(canon, sum, variant, maxPixels)
	cachePath := previewCachePath(cacheKey)
	if data, outMime, ok := loadCachedPreview(cachePath); ok {
		return previewResult(data, outMime, fi, canon, mimeType, cacheKey, variant)
	}
	_ = os.MkdirAll(filepath.Dir(cachePath), 0o700)
	previewGC(previewRoot())

	var data []byte
	var outMime string
	switch {
	case isImageMime(mimeType):
		if fi.Size() > maxImageSourceBytes {
			return fail(fmt.Errorf("image too large for preview"))
		}
		data, outMime, err = generateImagePreview(full, mimeType, maxPixels)
	case isTextMime(mimeType, canon):
		data, outMime, err = generateTextPreview(full, maxTextPreviewBytes)
	case mimeType == "application/pdf" || strings.HasSuffix(strings.ToLower(canon), ".pdf"):
		if fi.Size() > maxPDFSourceBytes {
			return fail(fmt.Errorf("pdf too large for preview"))
		}
		data, outMime, err = generatePDFPreview(full, maxPixels)
	case isVideoMime(mimeType, canon):
		if fi.Size() > maxVideoSourceBytes {
			return fail(fmt.Errorf("video too large for preview"))
		}
		data, outMime, err = generateVideoPreview(full, maxPixels)
	default:
		return fail(fmt.Errorf("preview unsupported for %s", mimeType))
	}
	if err != nil {
		return fail(err)
	}
	if err := os.WriteFile(cachePath, data, 0o600); err != nil {
		return fail(err)
	}
	return previewResult(data, outMime, fi, canon, mimeType, cacheKey, variant)
}

func previewResult(data []byte, outMime string, fi os.FileInfo, canon, srcMime, cacheKey, variant string) protocol.StorageOpResult {
	return protocol.StorageOpResult{
		OK:          true,
		DataB64:     base64.StdEncoding.EncodeToString(data),
		MimeType:    outMime,
		Size:        int64(len(data)),
		CacheKey:    cacheKey,
		PreviewKind: variant,
		Stat: &protocol.StorageStat{
			Name:     path.Base(canon),
			Path:     canon,
			Size:     fi.Size(),
			Mtime:    fi.ModTime().UTC().Format(time.RFC3339Nano),
			MimeType: srcMime,
		},
	}
}

func previewCacheKey(relPath, sha, variant string, maxPixels int) string {
	h := sha256.Sum256([]byte(relPath + "|" + sha + "|" + variant + "|" + fmt.Sprintf("%d", maxPixels)))
	return hex.EncodeToString(h[:])
}

func previewCachePath(cacheKey string) string {
	root := previewRoot()
	return filepath.Join(root, cacheKey[:2], cacheKey+".bin")
}

func loadCachedPreview(p string) ([]byte, string, bool) {
	data, err := os.ReadFile(p)
	if err != nil || len(data) < 1 {
		return nil, "", false
	}
	switch detectContentTypeBytes(data) {
	case "image/jpeg":
		return data, "image/jpeg", true
	case "image/png":
		return data, "image/png", true
	default:
		return data, "text/plain; charset=utf-8", true
	}
}

func detectContentTypeBytes(data []byte) string {
	n := len(data)
	if n > 512 {
		n = 512
	}
	return strings.ToLower(strings.TrimSpace(http.DetectContentType(data[:n])))
}

func generateTextPreview(full string, maxBytes int64) ([]byte, string, error) {
	f, err := os.Open(full)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	buf := make([]byte, maxBytes)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, "", err
	}
	return buf[:n], "text/plain; charset=utf-8", nil
}

func generateImagePreview(full, mimeType string, maxPixels int) ([]byte, string, error) {
	f, err := os.Open(full)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	img, err := decodeImage(f, mimeType)
	if err != nil {
		return nil, "", err
	}
	thumb := resizeToFit(img, maxPixels)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, thumb, &jpeg.Options{Quality: 82}); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), "image/jpeg", nil
}

func decodeImage(r io.Reader, mimeType string) (image.Image, error) {
	switch {
	case strings.Contains(mimeType, "png"):
		return png.Decode(r)
	case strings.Contains(mimeType, "gif"):
		return gif.Decode(r)
	case strings.Contains(mimeType, "webp"):
		return webp.Decode(r)
	default:
		return jpeg.Decode(r)
	}
}

func resizeToFit(src image.Image, maxPixels int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 || (w <= maxPixels && h <= maxPixels) {
		return src
	}
	nw, nh := w, h
	if w >= h {
		nw = maxPixels
		nh = max(1, int(float64(h)*float64(maxPixels)/float64(w)))
	} else {
		nh = maxPixels
		nw = max(1, int(float64(w)*float64(maxPixels)/float64(h)))
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		sy := b.Min.Y + (y*h)/nh
		for x := 0; x < nw; x++ {
			sx := b.Min.X + (x*w)/nw
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}

func generateVideoPreview(full string, maxPixels int) ([]byte, string, error) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, "", fmt.Errorf("video preview unavailable")
	}
	tmpDir, err := os.MkdirTemp("", "knot-video-preview-*")
	if err != nil {
		return nil, "", err
	}
	defer os.RemoveAll(tmpDir)
	out := filepath.Join(tmpDir, "frame.jpg")
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpeg, "-y", "-i", full, "-frames:v", "1", "-vf", fmt.Sprintf("scale='min(%d,iw)':-1", maxPixels), out)
	if err := cmd.Run(); err != nil {
		return nil, "", fmt.Errorf("video preview failed")
	}
	data, err := os.ReadFile(out)
	if err != nil {
		return nil, "", err
	}
	return data, "image/jpeg", nil
}

func generatePDFPreview(full string, maxPixels int) ([]byte, string, error) {
	if runtime.GOOS != "darwin" {
		return nil, "", fmt.Errorf("pdf preview unavailable")
	}
	ql, err := exec.LookPath("qlmanage")
	if err != nil {
		return nil, "", fmt.Errorf("pdf preview unavailable")
	}
	tmpDir, err := os.MkdirTemp("", "knot-pdf-preview-*")
	if err != nil {
		return nil, "", err
	}
	defer os.RemoveAll(tmpDir)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ql, "-t", "-s", fmt.Sprintf("%d", maxPixels), "-o", tmpDir, full)
	if err := cmd.Run(); err != nil {
		return nil, "", fmt.Errorf("pdf preview failed")
	}
	matches, _ := filepath.Glob(filepath.Join(tmpDir, "*.png"))
	if len(matches) == 0 {
		return nil, "", fmt.Errorf("pdf preview missing")
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		return nil, "", err
	}
	return data, "image/png", nil
}

func isTextMime(mimeType, rel string) bool {
	if strings.HasPrefix(mimeType, "text/") || strings.Contains(mimeType, "json") || strings.Contains(mimeType, "xml") {
		return true
	}
	ext := strings.ToLower(filepath.Ext(rel))
	switch ext {
	case ".txt", ".md", ".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".rs", ".sh", ".yaml", ".yml", ".toml", ".ini", ".conf", ".csv", ".log":
		return true
	}
	return false
}

func isImageMime(mimeType string) bool {
	return strings.HasPrefix(mimeType, "image/")
}

func isVideoMime(mimeType, rel string) bool {
	if strings.HasPrefix(mimeType, "video/") {
		return true
	}
	switch strings.ToLower(filepath.Ext(rel)) {
	case ".mp4", ".mov", ".m4v", ".webm", ".avi", ".mkv":
		return true
	}
	return false
}
