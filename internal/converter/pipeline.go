package converter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

var jobCounter atomic.Uint64

// Pipeline coordinates staging workspaces, engine dispatch, output integrity verification, and atomic file delivery.
type Pipeline struct {
	rt      *Runtime
	tempDir string
}

// NewPipeline initializes a conversion pipeline with its own isolated runtime workspace.
func NewPipeline(rt *Runtime) (*Pipeline, error) {
	if rt == nil {
		rt = NewRuntime(nil)
	}
	dir, err := os.MkdirTemp("", "formigo-runtime-*")
	if err != nil {
		return nil, err
	}
	return &Pipeline{
		rt:      rt,
		tempDir: dir,
	}, nil
}

// Runtime returns the underlying Runtime engine manager.
func (p *Pipeline) Runtime() *Runtime {
	return p.rt
}

// Close cleans up all staging files created during this session.
func (p *Pipeline) Close() error {
	if p == nil || p.tempDir == "" {
		return nil
	}
	return os.RemoveAll(p.tempDir)
}

// Convert executes a single-file or multi-file merge conversion atomically.
func (p *Pipeline) Convert(ctx context.Context, req Request, onProgress ProgressFunc) (Result, error) {
	start := time.Now()
	emit := func(st Stage, pct float64, detail string) {
		if onProgress != nil {
			onProgress(ProgressEvent{Stage: st, Progress: pct, Detail: detail})
		}
	}

	if len(req.InputPaths) == 0 {
		return Result{}, errors.New("no input files specified")
	}

	emit(StagePreparing, 0.02, "Checking input files")
	for _, in := range req.InputPaths {
		info, err := os.Stat(in)
		if err != nil {
			return Result{}, fmt.Errorf("cannot read %s: %w", filepath.Base(in), err)
		}
		if info.IsDir() {
			return Result{}, fmt.Errorf("%s is a directory, expected file", filepath.Base(in))
		}
		if info.Size() == 0 {
			return Result{}, fmt.Errorf("%s is empty (0 bytes)", filepath.Base(in))
		}
	}

	if req.Options.ImageQuality <= 0 || req.Options.ImageQuality > 100 {
		req.Options.ImageQuality = 90
	}
	if req.Options.PDFSplitSpan <= 0 {
		req.Options.PDFSplitSpan = 1
	}

	// Create isolated per-job work directory inside session tempDir.
	id := jobCounter.Add(1)
	workDir := filepath.Join(p.tempDir, fmt.Sprintf("job-%d", id))
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(workDir)

	// Handle multi-file merge modes first.
	if req.MergeMode != "" {
		return p.executeMerge(ctx, req, workDir, start, emit)
	}

	inPath := req.InputPaths[0]
	srcExt := NormalizeExt(inPath)
	dstExt := NormalizeExt(req.TargetExt)

	// Special PDF operations override effective output extension (e.g. split / extract-images -> zip, encrypt/decrypt -> pdf).
	if srcExt == "pdf" {
		switch req.Options.PDFOperation {
		case PDFOpSplit, PDFOpExtractImages:
			dstExt = "zip"
		case PDFOpEncrypt, PDFOpDecrypt:
			dstExt = "pdf"
		}
	}

	if dstExt == "" {
		return Result{}, errors.New("target format is required")
	}

	engine, ok := ResolveEngine(srcExt, dstExt, p.rt)
	if !ok {
		return Result{}, fmt.Errorf("unsupported conversion: .%s -> .%s (required engine may not be installed)", srcExt, dstExt)
	}

	stagedOut := filepath.Join(workDir, "output."+dstExt)
	emit(StageConverting, 0.1, fmt.Sprintf("%s -> %s (%s)", strings.ToUpper(srcExt), strings.ToUpper(dstExt), engine))

	warnings, err := p.dispatchSingle(ctx, engine, inPath, srcExt, stagedOut, dstExt, workDir, req.Options, func(pct float64, detail string) {
		if pct >= 0 {
			// Map [0..1] engine progress into [0.10..0.92]
			emit(StageConverting, 0.10+pct*0.82, detail)
		} else {
			emit(StageConverting, -1, detail)
		}
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, ErrCanceled) {
			return Result{}, ErrCanceled
		}
		return Result{}, err
	}

	emit(StageValidating, 0.94, "Validating output integrity")
	outSize, err := validateConvertedFile(stagedOut, dstExt)
	if err != nil {
		return Result{}, err
	}

	finalPath, err := resolveDestination(inPath, dstExt, req)
	if err != nil {
		return Result{}, err
	}
	if err := deliverAtomically(stagedOut, finalPath); err != nil {
		return Result{}, err
	}

	emit(StageDone, 1.0, filepath.Base(finalPath))
	return Result{
		InputPaths: []string{inPath},
		OutputPath: finalPath,
		SourceExt:  srcExt,
		TargetExt:  dstExt,
		Engine:     engine,
		OutputSize: outSize,
		Elapsed:    time.Since(start),
		Warnings:   warnings,
	}, nil
}

func (p *Pipeline) executeMerge(ctx context.Context, req Request, workDir string, start time.Time, emit func(Stage, float64, string)) (Result, error) {
	var dstExt string
	switch req.MergeMode {
	case MergeImagesToPDF, MergePDFs:
		dstExt = "pdf"
	case MergeFilesToZip:
		dstExt = "zip"
	default:
		return Result{}, fmt.Errorf("unknown merge mode: %s", req.MergeMode)
	}

	stagedOut := filepath.Join(workDir, "merged."+dstExt)
	emit(StageConverting, 0.15, fmt.Sprintf("Merging %d files -> .%s", len(req.InputPaths), dstExt))

	var warnings []string
	var err error
	switch req.MergeMode {
	case MergeImagesToPDF:
		warnings, err = p.mergeImagesToPDF(ctx, req.InputPaths, stagedOut, workDir, req.Options, func(pct float64, detail string) {
			emit(StageConverting, 0.15+pct*0.75, detail)
		})
	case MergePDFs:
		err = mergePDFsNative(ctx, req.InputPaths, stagedOut)
	case MergeFilesToZip:
		err = packFilesToZip(ctx, req.InputPaths, stagedOut, func(pct float64, detail string) {
			emit(StageConverting, 0.15+pct*0.75, detail)
		})
	}
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, ErrCanceled) {
			return Result{}, ErrCanceled
		}
		return Result{}, err
	}

	emit(StageValidating, 0.94, "Validating merged output")
	outSize, err := validateConvertedFile(stagedOut, dstExt)
	if err != nil {
		return Result{}, err
	}

	finalPath, err := resolveDestination(req.InputPaths[0], dstExt, req)
	if err != nil {
		return Result{}, err
	}
	if err := deliverAtomically(stagedOut, finalPath); err != nil {
		return Result{}, err
	}

	emit(StageDone, 1.0, filepath.Base(finalPath))
	return Result{
		InputPaths: append([]string(nil), req.InputPaths...),
		OutputPath: finalPath,
		SourceExt:  "multi",
		TargetExt:  dstExt,
		Engine:     EngineBuiltin,
		OutputSize: outSize,
		Elapsed:    time.Since(start),
		Warnings:   warnings,
	}, nil
}

func (p *Pipeline) dispatchSingle(
	ctx context.Context,
	engine EngineID,
	inPath, srcExt, outPath, dstExt, workDir string,
	opts Options,
	report func(pct float64, detail string),
) ([]string, error) {
	cat := CategoryForExt(srcExt)

	// Universal pack-single-file-to-zip route (excluding archive-to-archive or PDF split/extract-images).
	if dstExt == "zip" && cat != CategoryArchive && srcExt != "pdf" {
		return nil, packFilesToZip(ctx, []string{inPath}, outPath, report)
	}

	switch engine {
	case EngineBuiltin:
		switch {
		case cat == CategoryImage:
			return p.convertImage(ctx, EngineBuiltin, inPath, srcExt, outPath, dstExt, workDir, opts, report)
		case srcExt == "pdf":
			return p.convertPDF(ctx, EngineBuiltin, inPath, outPath, dstExt, workDir, opts, report)
		case cat == CategorySubtitle:
			return nil, convertSubtitleNative(ctx, inPath, srcExt, outPath, dstExt, opts)
		case cat == CategoryArchive:
			return p.convertArchive(ctx, inPath, srcExt, outPath, dstExt, workDir, opts, report)
		case cat == CategorySpreadsheet || cat == CategoryData:
			return convertDataOrSheetNative(ctx, inPath, srcExt, outPath, dstExt, opts)
		case cat == CategoryDocument || cat == CategoryEbook:
			return convertDocumentOrEbookNative(ctx, inPath, srcExt, outPath, dstExt, workDir, opts)
		default:
			return nil, fmt.Errorf("no builtin handler for .%s -> .%s", srcExt, dstExt)
		}

	case EngineFFmpeg:
		if cat == CategoryImage {
			return p.convertImage(ctx, EngineFFmpeg, inPath, srcExt, outPath, dstExt, workDir, opts, report)
		}
		return p.convertMediaFFmpeg(ctx, inPath, srcExt, outPath, dstExt, opts, report)

	case EngineSips:
		if srcExt == "pdf" {
			return p.convertPDF(ctx, EngineSips, inPath, outPath, dstExt, workDir, opts, report)
		}
		return p.convertImage(ctx, EngineSips, inPath, srcExt, outPath, dstExt, workDir, opts, report)

	case EnginePoppler:
		return p.convertPDF(ctx, EnginePoppler, inPath, outPath, dstExt, workDir, opts, report)

	case EngineTesseract:
		return p.convertImageOCR(ctx, inPath, outPath, dstExt, workDir, opts)

	case EngineLibreOffice:
		return p.convertViaLibreOffice(ctx, inPath, srcExt, outPath, dstExt, workDir)

	case EnginePandoc:
		return p.convertViaPandoc(ctx, inPath, srcExt, outPath, dstExt)

	default:
		return nil, fmt.Errorf("engine %s is not supported", engine)
	}
}

func validateConvertedFile(path, dstExt string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("conversion produced no output file: %w", err)
	}
	if info.Size() == 0 {
		return 0, errors.New("conversion produced an empty file (0 bytes)")
	}

	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	head := make([]byte, 16)
	n, _ := io.ReadFull(f, head)
	head = head[:n]

	switch dstExt {
	case "png":
		if !bytes.HasPrefix(head, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n'}) {
			return 0, errors.New("output PNG header verification failed")
		}
	case "jpg":
		if !bytes.HasPrefix(head, []byte{0xFF, 0xD8, 0xFF}) {
			return 0, errors.New("output JPEG header verification failed")
		}
	case "gif":
		if !bytes.HasPrefix(head, []byte("GIF87a")) && !bytes.HasPrefix(head, []byte("GIF89a")) {
			return 0, errors.New("output GIF header verification failed")
		}
	case "webp":
		if len(head) < 12 || !bytes.Equal(head[:4], []byte("RIFF")) || !bytes.Equal(head[8:12], []byte("WEBP")) {
			return 0, errors.New("output WebP header verification failed")
		}
	case "bmp":
		if !bytes.HasPrefix(head, []byte("BM")) {
			return 0, errors.New("output BMP header verification failed")
		}
	case "ico":
		if len(head) < 4 || head[0] != 0 || head[1] != 0 || head[2] != 1 || head[3] != 0 {
			return 0, errors.New("output ICO header verification failed")
		}
	case "pdf":
		if !bytes.HasPrefix(head, []byte("%PDF-")) {
			return 0, errors.New("output PDF header verification failed")
		}
	case "zip", "docx", "xlsx", "epub":
		if !bytes.HasPrefix(head, []byte("PK\x03\x04")) && !bytes.HasPrefix(head, []byte("PK\x05\x06")) {
			return 0, fmt.Errorf("output %s container signature verification failed", strings.ToUpper(dstExt))
		}
	case "tar.gz":
		if len(head) < 2 || head[0] != 0x1F || head[1] != 0x8B {
			return 0, errors.New("output GZIP header verification failed")
		}
	}

	return info.Size(), nil
}

func resolveDestination(primaryInput, dstExt string, req Request) (string, error) {
	if strings.TrimSpace(req.OutputPath) != "" {
		abs, err := filepath.Abs(req.OutputPath)
		if err != nil {
			return "", err
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return "", err
		}
		if !req.Options.Overwrite && sameOrExisting(abs, primaryInput) {
			return UniqueDestination(abs), nil
		}
		return abs, nil
	}

	outDir := strings.TrimSpace(req.OutputDir)
	if outDir == "" {
		outDir = filepath.Dir(primaryInput)
	}
	absDir, err := filepath.Abs(outDir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return "", err
	}

	base := safeStem(primaryInput)
	switch {
	case req.MergeMode == MergeImagesToPDF || req.MergeMode == MergePDFs:
		base += "-merged"
	case req.MergeMode == MergeFilesToZip:
		base += "-archive"
	case NormalizeExt(primaryInput) == "pdf" && req.Options.PDFOperation == PDFOpSplit:
		base += "-split"
	case NormalizeExt(primaryInput) == "pdf" && req.Options.PDFOperation == PDFOpEncrypt:
		base += "-encrypted"
	case NormalizeExt(primaryInput) == "pdf" && req.Options.PDFOperation == PDFOpDecrypt:
		base += "-decrypted"
	case NormalizeExt(primaryInput) == "pdf" && req.Options.PDFOperation == PDFOpExtractImages:
		base += "-images"
	}

	candidate := filepath.Join(absDir, base+"."+dstExt)
	// Never clobber the source input file itself even when Overwrite is true!
	if sameFilePath(candidate, primaryInput) {
		return UniqueDestination(candidate), nil
	}
	if !req.Options.Overwrite {
		return UniqueDestination(candidate), nil
	}
	return candidate, nil
}

// UniqueDestination appends " (1)", " (2)", ... if path already exists on disk.
func UniqueDestination(path string) string {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}
	dir := filepath.Dir(path)
	name := filepath.Base(path)
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	if strings.HasSuffix(strings.ToLower(name), ".tar.gz") {
		ext = ".tar.gz"
		stem = name[:len(name)-len(".tar.gz")]
	}
	for i := 1; i < 10000; i++ {
		cand := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
		if _, err := os.Stat(cand); os.IsNotExist(err) {
			return cand
		}
	}
	return filepath.Join(dir, fmt.Sprintf("%s-%d%s", stem, time.Now().UnixNano(), ext))
}

func safeStem(p string) string {
	name := filepath.Base(p)
	if strings.HasSuffix(strings.ToLower(name), ".tar.gz") {
		name = name[:len(name)-len(".tar.gz")]
	} else if ext := filepath.Ext(name); ext != "" {
		name = strings.TrimSuffix(name, ext)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "converted"
	}
	return name
}

func sameOrExisting(path, input string) bool {
	if sameFilePath(path, input) {
		return true
	}
	_, err := os.Stat(path)
	return err == nil
}

func sameFilePath(a, b string) bool {
	absA, err1 := filepath.Abs(a)
	absB, err2 := filepath.Abs(b)
	if err1 != nil || err2 != nil {
		return a == b
	}
	return filepath.Clean(absA) == filepath.Clean(absB)
}

func deliverAtomically(stagedPath, finalPath string) error {
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
		return err
	}
	// Try direct rename first (instant when on same filesystem).
	if err := os.Rename(stagedPath, finalPath); err == nil {
		return nil
	}
	// Cross-device fallback: write to sibling temp file then rename into finalPath.
	tmpDest := finalPath + fmt.Sprintf(".formigo-%d.part", time.Now().UnixNano())
	in, err := os.Open(stagedPath)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(tmpDest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(tmpDest)
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		_ = os.Remove(tmpDest)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmpDest)
		return err
	}
	if err := os.Rename(tmpDest, finalPath); err != nil {
		_ = os.Remove(tmpDest)
		return err
	}
	return nil
}
