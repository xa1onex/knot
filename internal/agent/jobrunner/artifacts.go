package jobrunner

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/knot-infra/knot/internal/storage/pathsafe"
	"github.com/knot-infra/knot/pkg/protocol"
)

const (
	jobInputLeaf   = "input"
	jobOutputLeaf  = "output"
	jobPartialLeaf = ".knot.part"
)

var errArtifactLimit = errors.New("artifact_limit")

func canonicalJobInput(jobID string) string {
	return path.Join("jobs", jobID, jobInputLeaf)
}

func canonicalJobOutput(jobID string) string {
	return path.Join("jobs", jobID, jobOutputLeaf)
}

func canonicalJobPartial(jobID string) string {
	return path.Join("jobs", jobID, jobPartialLeaf)
}

func artifactLimits(pol Policy, spec protocol.JobSpec) (maxBytes, maxTotal int64, maxFiles, maxDirs, maxDepth int) {
	p := pol.normalized()
	maxBytes = p.MaxArtifactBytes
	if maxBytes <= 0 {
		maxBytes = protocol.DefaultMaxArtifactBytes
	}
	maxFiles = p.MaxArtifactFiles
	if maxFiles <= 0 {
		maxFiles = protocol.DefaultMaxArtifactFiles
	}
	maxDirs = p.MaxArtifactDirs
	if maxDirs <= 0 {
		maxDirs = protocol.DefaultMaxArtifactDirs
	}
	maxDepth = p.MaxArtifactDepth
	if maxDepth <= 0 {
		maxDepth = protocol.DefaultMaxArtifactDepth
	}
	disk := spec.Resources.DiskMB
	if disk <= 0 {
		disk = protocol.DefaultJobDiskMB
	}
	maxTotal = disk << 20
	if p.MaxDiskMB > 0 && maxTotal > p.MaxDiskMB<<20 {
		maxTotal = p.MaxDiskMB << 20
	}
	return maxBytes, maxTotal, maxFiles, maxDirs, maxDepth
}

// stageJobInput copies Storage into the isolated workspace /input.
// The job never sees the storage root, host drives, or sibling directories.
func stageJobInput(storageRoot, jobID, sourcePath, inDir string) error {
	if storageRoot == "" || jobID == "" {
		return nil
	}
	canon := canonicalJobInput(jobID)
	src := strings.TrimSpace(strings.ReplaceAll(sourcePath, `\`, "/"))
	src = strings.Trim(src, "/")
	if src == "." {
		src = ""
	}
	if src != "" && src != canon {
		if err := copyStorageToStorage(storageRoot, src, canon); err != nil {
			return err
		}
	}
	return copyStorageToDir(storageRoot, canon, inDir)
}

func copyStorageToStorage(storageRoot, fromRel, toRel string) error {
	if fromRel == "" || fromRel == "." || toRel == "" || toRel == "." {
		return fmt.Errorf("refusing to copy storage root")
	}
	src, err := pathsafe.ResolveUnderRoot(storageRoot, fromRel)
	if err != nil {
		return err
	}
	dst, err := pathsafe.ResolveUnderRoot(storageRoot, toRel)
	if err != nil {
		return err
	}
	st, err := os.Stat(src)
	if err != nil {
		return err
	}
	if st.IsDir() {
		return copyTree(src, dst)
	}
	return copyFile(src, filepath.Join(dst, filepath.Base(src)))
}

func copyStorageToDir(storageRoot, fromRel, absDst string) error {
	if fromRel == "" || fromRel == "." {
		return nil
	}
	src, err := pathsafe.ResolveUnderRoot(storageRoot, fromRel)
	if err != nil {
		return err
	}
	st, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if st.IsDir() {
		return copyTree(src, absDst)
	}
	return copyFile(src, filepath.Join(absDst, filepath.Base(src)))
}

func inspectOutput(outDir string, maxBytes, maxTotal int64, maxFiles, maxDirs, maxDepth int) error {
	var files, dirs int
	var total int64
	err := filepath.Walk(outDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info == nil {
			return nil
		}
		rel, err := filepath.Rel(outDir, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		depth := strings.Count(filepath.ToSlash(rel), "/") + 1
		if maxDepth > 0 && depth > maxDepth {
			return fmt.Errorf("%w: path depth %d exceeds %d", errArtifactLimit, depth, maxDepth)
		}
		if info.IsDir() {
			dirs++
			if maxDirs > 0 && dirs > maxDirs {
				return fmt.Errorf("%w: too many directories (%d > %d)", errArtifactLimit, dirs, maxDirs)
			}
			return nil
		}
		files++
		if maxFiles > 0 && files > maxFiles {
			return fmt.Errorf("%w: too many files (%d > %d)", errArtifactLimit, files, maxFiles)
		}
		if maxBytes > 0 && info.Size() > maxBytes {
			return fmt.Errorf("%w: file %s exceeds %d bytes", errArtifactLimit, rel, maxBytes)
		}
		total += info.Size()
		if maxTotal > 0 && total > maxTotal {
			return fmt.Errorf("%w: total output exceeds disk limit", errArtifactLimit)
		}
		return nil
	})
	return err
}

func commitJobOutput(storageRoot string, spec protocol.JobSpec, outDir string, pol Policy) ([]protocol.JobArtifact, error) {
	maxBytes, maxTotal, maxFiles, maxDirs, maxDepth := artifactLimits(pol, spec)
	if err := inspectOutput(outDir, maxBytes, maxTotal, maxFiles, maxDirs, maxDepth); err != nil {
		return nil, err
	}
	outputPath := spec.OutputPath
	if outputPath == "" {
		outputPath = canonicalJobOutput(spec.JobID)
	}
	partialPath := canonicalJobPartial(spec.JobID)
	if err := removeStoragePath(storageRoot, partialPath); err != nil {
		return nil, err
	}
	arts, err := writePartialOutput(storageRoot, partialPath, outputPath, outDir)
	if err != nil {
		_ = removeStoragePath(storageRoot, partialPath)
		return nil, err
	}
	if err := removeStoragePath(storageRoot, outputPath); err != nil {
		_ = removeStoragePath(storageRoot, partialPath)
		return nil, err
	}
	if err := renameStoragePath(storageRoot, partialPath, outputPath); err != nil {
		_ = removeStoragePath(storageRoot, partialPath)
		return nil, err
	}
	return arts, nil
}

func writePartialOutput(storageRoot, partialRel, outputRel, outDir string) ([]protocol.JobArtifact, error) {
	partialAbs, err := pathsafe.ResolveUnderRoot(storageRoot, partialRel)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(partialAbs, 0o700); err != nil {
		return nil, err
	}
	var arts []protocol.JobArtifact
	err = filepath.Walk(outDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info == nil {
			return nil
		}
		rel, err := filepath.Rel(outDir, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		if strings.Contains(relSlash, ".knot.part") {
			return nil
		}
		target := filepath.Join(partialAbs, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if err := copyFile(p, target); err != nil {
			return err
		}
		sum, err := fileSHA256(p)
		if err != nil {
			return err
		}
		storagePath := strings.TrimSuffix(outputRel, "/") + "/" + relSlash
		arts = append(arts, protocol.JobArtifact{
			Name:     filepath.Base(relSlash),
			Path:     storagePath,
			Size:     info.Size(),
			SHA256:   sum,
			MimeType: mimeFromName(filepath.Base(relSlash)),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if arts == nil {
		arts = []protocol.JobArtifact{}
	}
	return arts, nil
}

func cleanupJobOutput(storageRoot, jobID string) {
	if storageRoot == "" || jobID == "" {
		return
	}
	_ = removeStoragePath(storageRoot, canonicalJobPartial(jobID))
	_ = removeStoragePath(storageRoot, canonicalJobOutput(jobID))
}

func removeStoragePath(storageRoot, rel string) error {
	if storageRoot == "" || rel == "" {
		return nil
	}
	abs, err := pathsafe.ResolveUnderRoot(storageRoot, rel)
	if err != nil {
		return err
	}
	return os.RemoveAll(abs)
}

func renameStoragePath(storageRoot, fromRel, toRel string) error {
	from, err := pathsafe.ResolveUnderRoot(storageRoot, fromRel)
	if err != nil {
		return err
	}
	to, err := pathsafe.ResolveUnderRoot(storageRoot, toRel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
		return err
	}
	return os.Rename(from, to)
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

func mimeFromName(name string) string {
	if ext := filepath.Ext(name); ext != "" {
		if mt := mime.TypeByExtension(ext); mt != "" {
			return mt
		}
	}
	return "application/octet-stream"
}

func outputFilePaths(arts []protocol.JobArtifact) []string {
	out := make([]string, 0, len(arts))
	for _, a := range arts {
		out = append(out, a.Path)
	}
	return out
}

func failedArtifactLimit(spec protocol.JobSpec, err error) protocol.JobResult {
	return protocol.JobResult{
		JobID:      spec.JobID,
		OutputPath: spec.OutputPath,
		Status:     protocol.JobStatusFailed,
		Reason:     protocol.JobReasonArtifactLimit,
		Error:      err.Error(),
		ExitCode:   intPtr(1),
	}
}

func applyCommit(res *protocol.JobResult, spec protocol.JobSpec, arts []protocol.JobArtifact) {
	res.OutputPath = spec.OutputPath
	if res.OutputPath == "" {
		res.OutputPath = canonicalJobOutput(spec.JobID)
	}
	res.Artifacts = arts
	res.OutputFiles = outputFilePaths(arts)
	res.Status = protocol.JobStatusArtifactsCommitted
	res.ExitCode = intPtr(0)
	res.OK = true
}

func sweepLeftovers(storageRoot, workRoot string) {
	if workRoot != "" {
		entries, err := os.ReadDir(workRoot)
		if err == nil {
			for _, e := range entries {
				_ = os.RemoveAll(filepath.Join(workRoot, e.Name()))
			}
		}
	}
	if storageRoot == "" {
		return
	}
	jobsDir, err := pathsafe.ResolveUnderRoot(storageRoot, "jobs")
	if err != nil {
		return
	}
	ents, err := os.ReadDir(jobsDir)
	if err != nil {
		return
	}
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		_ = os.RemoveAll(filepath.Join(jobsDir, e.Name(), jobPartialLeaf))
	}
}

func defaultWorkRoot(storageRoot string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(storageRoot)))
	return filepath.Join(os.TempDir(), "knot-jobs-"+hex.EncodeToString(sum[:8]))
}
