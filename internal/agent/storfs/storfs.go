package storfs

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/knot-infra/knot/internal/storage/pathsafe"
	"github.com/knot-infra/knot/pkg/protocol"
)

// Manager performs real-FS storage ops under Root (knot-storage).
type Manager struct {
	Root string
	Send func(v any) error

	pathMu  sync.Map // path -> *sync.Mutex for atomic rename/move races
	writeMu sync.Mutex
	writes  map[string]*writeSession
}

type writeSession struct {
	path     string
	partPath string
	file     *os.File
	hasher   hash.Hash
	size     int64
	sha256   string
	written  int64
}

func New(root string, send func(v any) error) (*Manager, error) {
	if root == "" {
		return nil, fmt.Errorf("storage root required")
	}
	if err := EnsureRoot(root); err != nil {
		return nil, err
	}
	return &Manager{Root: root, Send: send, writes: make(map[string]*writeSession)}, nil
}

func EnsureRoot(root string) error {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	for _, d := range pathsafe.DefaultDirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o700); err != nil {
			return err
		}
	}
	return nil
}

func DefaultDir() string {
	if v := os.Getenv("KNOT_STORAGE_DIR"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "./knot-storage"
	}
	return filepath.Join(home, "knot-storage")
}

func (m *Manager) lockPath(rel string) func() {
	v, _ := m.pathMu.LoadOrStore(rel, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func (m *Manager) Handle(data []byte) {
	var op protocol.StorageOp
	if err := json.Unmarshal(data, &op); err != nil || op.Type != protocol.TypeStorageOp {
		return
	}
	res := m.exec(op)
	res.Type = protocol.TypeStorageOpResult
	res.RequestID = op.RequestID
	_ = m.Send(res)
}

func (m *Manager) exec(op protocol.StorageOp) protocol.StorageOpResult {
	switch op.Op {
	case protocol.StorageOpEnsure:
		if err := EnsureRoot(m.Root); err != nil {
			return fail(err)
		}
		return protocol.StorageOpResult{OK: true}
	case protocol.StorageOpList:
		return m.list(op.Path)
	case protocol.StorageOpStat:
		return m.stat(op.Path)
	case protocol.StorageOpMkdir:
		return m.mkdir(op.Path)
	case protocol.StorageOpDelete:
		return m.delete(op.Path)
	case protocol.StorageOpPartial:
		return m.partial(op.Path, op.FileID)
	case protocol.StorageOpMove:
		return m.move(op.FromPath, op.ToPath)
	case protocol.StorageOpCopy:
		return m.copy(op.FromPath, op.ToPath)
	case protocol.StorageOpWriteStart:
		return m.writeStart(op)
	case protocol.StorageOpWriteChunk:
		return m.writeChunk(op)
	case protocol.StorageOpWriteCommit:
		return m.writeCommit(op)
	case protocol.StorageOpWriteAbort:
		return m.writeAbort(op.FileID)
	case protocol.StorageOpRead:
		return m.readContent(op.Path, op.MaxBytes)
	case protocol.StorageOpPreview:
		return m.readPreview(op.Path, op.Preview, op.MaxPixels)
	default:
		return fail(fmt.Errorf("unknown op %q", op.Op))
	}
}

func (m *Manager) list(rel string) protocol.StorageOpResult {
	var full string
	var err error
	var baseRel string
	if rel == "" || rel == "." {
		full = m.Root
		baseRel = ""
	} else {
		full, err = pathsafe.ResolveUnderRoot(m.Root, rel)
		if err != nil {
			return fail(err)
		}
		baseRel, err = pathsafe.CanonicalRel(rel)
		if err != nil {
			return fail(err)
		}
	}
	ents, err := os.ReadDir(full)
	if err != nil {
		return fail(err)
	}
	out := make([]protocol.StorageEntry, 0, len(ents))
	for _, e := range ents {
		name := e.Name()
		if strings.Contains(name, ".knot.part") {
			continue // hide incomplete uploads
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		p := name
		if baseRel != "" {
			p = baseRel + "/" + name
		}
		entry := protocol.StorageEntry{
			Name:  name,
			Path:  p,
			IsDir: e.IsDir(),
			Mtime: info.ModTime().UTC().Format(time.RFC3339Nano),
		}
		if !e.IsDir() {
			entry.Size = info.Size()
			entry.MimeType = detectMime(name, filepath.Join(full, name))
		}
		out = append(out, entry)
	}
	return protocol.StorageOpResult{OK: true, Entries: out}
}

func (m *Manager) stat(rel string) protocol.StorageOpResult {
	if rel == "" || rel == "." {
		fi, err := os.Stat(m.Root)
		if err != nil {
			return fail(err)
		}
		return protocol.StorageOpResult{OK: true, Stat: &protocol.StorageStat{
			Name: "", Path: "", IsDir: true, Size: 0,
			Mtime: fi.ModTime().UTC().Format(time.RFC3339Nano),
			Mode:  fi.Mode().String(),
		}}
	}
	full, err := pathsafe.ResolveUnderRoot(m.Root, rel)
	if err != nil {
		return fail(err)
	}
	fi, err := os.Lstat(full)
	if err != nil {
		return fail(err)
	}
	canon, _ := pathsafe.CanonicalRel(rel)
	st := &protocol.StorageStat{
		Name:  path.Base(canon),
		Path:  canon,
		IsDir: fi.IsDir(),
		Size:  fi.Size(),
		Mtime: fi.ModTime().UTC().Format(time.RFC3339Nano),
		Mode:  fi.Mode().String(),
	}
	if fi.IsDir() {
		st.Size = 0
	} else {
		sum, err := fileSHA256(full)
		if err != nil {
			return fail(err)
		}
		st.SHA256 = sum
		st.MimeType = detectMime(st.Name, full)
	}
	return protocol.StorageOpResult{OK: true, Stat: st}
}

func (m *Manager) mkdir(rel string) protocol.StorageOpResult {
	unlock := m.lockPath(rel)
	defer unlock()
	full, err := pathsafe.ResolveUnderRoot(m.Root, rel)
	if err != nil {
		return fail(err)
	}
	if err := os.MkdirAll(full, 0o700); err != nil {
		return fail(err)
	}
	return m.stat(rel)
}

func (m *Manager) delete(rel string) protocol.StorageOpResult {
	if rel == "" || rel == "." {
		return fail(fmt.Errorf("refusing to delete storage root"))
	}
	unlock := m.lockPath(rel)
	defer unlock()
	full, err := pathsafe.ResolveUnderRoot(m.Root, rel)
	if err != nil {
		return fail(err)
	}
	// Remove any in-progress parts for this path
	dir := filepath.Dir(full)
	base := filepath.Base(full)
	if ents, err := os.ReadDir(dir); err == nil {
		prefix := base + protocol.PartSuffixPrefix
		legacy := base + ".knot.part"
		for _, e := range ents {
			n := e.Name()
			if n == legacy || strings.HasPrefix(n, prefix) {
				_ = os.Remove(filepath.Join(dir, n))
			}
		}
	}
	if err := os.RemoveAll(full); err != nil {
		return fail(err)
	}
	return protocol.StorageOpResult{OK: true}
}

func (m *Manager) partial(rel, fileID string) protocol.StorageOpResult {
	if rel == "" || rel == "." {
		return fail(fmt.Errorf("path required"))
	}
	full, err := pathsafe.ResolveUnderRoot(m.Root, rel)
	if err != nil {
		return fail(err)
	}
	if fileID != "" {
		part := protocol.PartPath(full, fileID)
		if st, err := os.Stat(part); err == nil {
			return protocol.StorageOpResult{OK: true, PartialBytes: st.Size()}
		}
	}
	// Fallback: largest part for this path (resume after interrupt).
	dir := filepath.Dir(full)
	base := filepath.Base(full)
	ents, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return protocol.StorageOpResult{OK: true, PartialBytes: 0}
		}
		return fail(err)
	}
	var max int64
	prefix := base + protocol.PartSuffixPrefix
	legacy := base + ".knot.part"
	for _, e := range ents {
		n := e.Name()
		if n != legacy && !strings.HasPrefix(n, prefix) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.Size() > max {
			max = info.Size()
		}
	}
	return protocol.StorageOpResult{OK: true, PartialBytes: max}
}

func (m *Manager) move(from, to string) protocol.StorageOpResult {
	if from == "" || to == "" {
		return fail(fmt.Errorf("from_path and to_path required"))
	}
	unlockFrom := m.lockPath(from)
	defer unlockFrom()
	unlockTo := m.lockPath(to)
	defer unlockTo()
	src, err := pathsafe.ResolveUnderRoot(m.Root, from)
	if err != nil {
		return fail(err)
	}
	dst, err := pathsafe.ResolveUnderRoot(m.Root, to)
	if err != nil {
		return fail(err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return fail(err)
	}
	if err := os.Rename(src, dst); err != nil {
		return fail(err)
	}
	return m.stat(to)
}

func (m *Manager) copy(from, to string) protocol.StorageOpResult {
	if from == "" || to == "" {
		return fail(fmt.Errorf("from_path and to_path required"))
	}
	unlockTo := m.lockPath(to)
	defer unlockTo()
	src, err := pathsafe.ResolveUnderRoot(m.Root, from)
	if err != nil {
		return fail(err)
	}
	dst, err := pathsafe.ResolveUnderRoot(m.Root, to)
	if err != nil {
		return fail(err)
	}
	fi, err := os.Stat(src)
	if err != nil {
		return fail(err)
	}
	if fi.IsDir() {
		return fail(fmt.Errorf("copy of directories not supported in Stage 4.1"))
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return fail(err)
	}
	in, err := os.Open(src)
	if err != nil {
		return fail(err)
	}
	defer in.Close()
	tmp := dst + ".knot.copy." + fmt.Sprintf("%d", time.Now().UnixNano())
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fail(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return fail(err)
	}
	_ = out.Close()
	_ = os.Remove(dst)
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return fail(err)
	}
	return m.stat(to)
}

func (m *Manager) writeStart(op protocol.StorageOp) protocol.StorageOpResult {
	if op.FileID == "" || op.Path == "" || op.Size <= 0 || op.SHA256 == "" {
		return fail(fmt.Errorf("file_id, path, size, sha256 required"))
	}
	if op.Size > protocol.MaxStorageTransferBytes {
		return fail(fmt.Errorf("file too large"))
	}
	full, err := pathsafe.ResolveUnderRoot(m.Root, op.Path)
	if err != nil {
		return fail(err)
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		return fail(err)
	}
	part := protocol.PartPath(full, op.FileID)
	f, err := os.OpenFile(part, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fail(err)
	}
	m.writeMu.Lock()
	if old := m.writes[op.FileID]; old != nil {
		_ = old.file.Close()
		_ = os.Remove(old.partPath)
	}
	m.writes[op.FileID] = &writeSession{
		path: op.Path, partPath: part, file: f, hasher: sha256.New(),
		size: op.Size, sha256: strings.ToLower(op.SHA256),
	}
	m.writeMu.Unlock()
	return protocol.StorageOpResult{OK: true}
}

func (m *Manager) writeChunk(op protocol.StorageOp) protocol.StorageOpResult {
	raw, err := base64.StdEncoding.DecodeString(op.DataB64)
	if err != nil {
		return fail(fmt.Errorf("bad chunk encoding"))
	}
	m.writeMu.Lock()
	st := m.writes[op.FileID]
	m.writeMu.Unlock()
	if st == nil {
		return fail(fmt.Errorf("no write session"))
	}
	if op.Offset != st.written {
		return fail(fmt.Errorf("offset mismatch: got %d want %d", op.Offset, st.written))
	}
	if _, err := st.file.Write(raw); err != nil {
		return fail(err)
	}
	_, _ = st.hasher.Write(raw)
	st.written += int64(len(raw))
	return protocol.StorageOpResult{OK: true, PartialBytes: st.written}
}

func (m *Manager) writeCommit(op protocol.StorageOp) protocol.StorageOpResult {
	m.writeMu.Lock()
	st := m.writes[op.FileID]
	delete(m.writes, op.FileID)
	m.writeMu.Unlock()
	if st == nil {
		return fail(fmt.Errorf("no write session"))
	}
	_ = st.file.Sync()
	_ = st.file.Close()
	sum := hex.EncodeToString(st.hasher.Sum(nil))
	if st.written != st.size {
		_ = os.Remove(st.partPath)
		return fail(fmt.Errorf("size mismatch: got %d want %d", st.written, st.size))
	}
	if !strings.EqualFold(sum, st.sha256) {
		_ = os.Remove(st.partPath)
		return fail(fmt.Errorf("sha256 mismatch"))
	}
	unlock := m.lockPath(st.path)
	defer unlock()
	full, err := pathsafe.ResolveUnderRoot(m.Root, st.path)
	if err != nil {
		_ = os.Remove(st.partPath)
		return fail(err)
	}
	_ = os.Remove(full)
	if err := os.Rename(st.partPath, full); err != nil {
		_ = os.Remove(st.partPath)
		return fail(err)
	}
	return m.stat(st.path)
}

func (m *Manager) writeAbort(fileID string) protocol.StorageOpResult {
	m.writeMu.Lock()
	st := m.writes[fileID]
	delete(m.writes, fileID)
	m.writeMu.Unlock()
	if st != nil {
		_ = st.file.Close()
		_ = os.Remove(st.partPath)
	}
	return protocol.StorageOpResult{OK: true}
}

func (m *Manager) readContent(rel string, maxBytes int64) protocol.StorageOpResult {
	if rel == "" || rel == "." {
		return fail(fmt.Errorf("path required"))
	}
	if maxBytes <= 0 || maxBytes > 8<<20 {
		maxBytes = 8 << 20
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
	if fi.Size() > maxBytes {
		return fail(fmt.Errorf("file too large for inline read (%d > %d)", fi.Size(), maxBytes))
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return fail(err)
	}
	canon, _ := pathsafe.CanonicalRel(rel)
	return protocol.StorageOpResult{
		OK: true, DataB64: base64.StdEncoding.EncodeToString(data),
		Size: fi.Size(), MimeType: detectMime(path.Base(canon), full),
		Stat: &protocol.StorageStat{
			Name: path.Base(canon), Path: canon, Size: fi.Size(),
			Mtime: fi.ModTime().UTC().Format(time.RFC3339Nano),
			MimeType: detectMime(path.Base(canon), full),
		},
	}
}

func fail(err error) protocol.StorageOpResult {
	return protocol.StorageOpResult{OK: false, Error: err.Error()}
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func detectMime(name, full string) string {
	if ext := filepath.Ext(name); ext != "" {
		if mt := mime.TypeByExtension(ext); mt != "" {
			return mt
		}
	}
	f, err := os.Open(full)
	if err != nil {
		return "application/octet-stream"
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	if n == 0 {
		return "application/octet-stream"
	}
	return http.DetectContentType(buf[:n])
}
