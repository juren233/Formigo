package converter

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	maxExtractedTotalBytes = 2 * 1024 * 1024 * 1024 // 2 GB safety cap
	maxExtractedEntries    = 10000
)

func (p *Pipeline) convertArchive(
	ctx context.Context,
	inPath, srcExt, outPath, dstExt, workDir string,
	opts Options,
	report func(float64, string),
) ([]string, error) {
	extractDir := filepath.Join(workDir, "archive-unpack")
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		return nil, err
	}

	report(0.15, "Extracting source archive")
	switch srcExt {
	case "zip":
		if err := unpackZipSafe(ctx, inPath, extractDir); err != nil {
			return nil, err
		}
	case "tar":
		if err := unpackTarSafe(ctx, inPath, false, extractDir); err != nil {
			return nil, err
		}
	case "tar.gz":
		if err := unpackTarSafe(ctx, inPath, true, extractDir); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported archive input: .%s", srcExt)
	}

	// Archive of images -> Merged PDF
	if dstExt == "pdf" {
		var imgs []string
		_ = filepath.WalkDir(extractDir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			base := d.Name()
			if strings.HasPrefix(base, ".") || strings.Contains(path, "__MACOSX") {
				return nil
			}
			if CategoryForExt(base) == CategoryImage {
				imgs = append(imgs, path)
			}
			return nil
		})
		sort.Strings(imgs)
		if len(imgs) == 0 {
			return nil, errors.New("archive contains no supported image files to merge into PDF")
		}
		return p.mergeImagesToPDF(ctx, imgs, outPath, workDir, opts, report)
	}

	report(0.55, "Repacking to ."+dstExt)
	switch dstExt {
	case "zip":
		return nil, packDirectoryToZip(ctx, extractDir, outPath)
	case "tar":
		return nil, packDirectoryToTar(ctx, extractDir, false, outPath)
	case "tar.gz":
		return nil, packDirectoryToTar(ctx, extractDir, true, outPath)
	default:
		return nil, fmt.Errorf("unsupported archive target format: .%s", dstExt)
	}
}

// packFilesToZip writes top-level files (deduplicating colliding basenames) into a ZIP archive.
func packFilesToZip(ctx context.Context, files []string, outZip string, report func(float64, string)) error {
	f, err := os.Create(outZip)
	if err != nil {
		return err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	used := make(map[string]int)

	for i, src := range files {
		if err := ctx.Err(); err != nil {
			_ = zw.Close()
			return ErrCanceled
		}
		if report != nil && len(files) > 0 {
			report(float64(i)/float64(len(files)), filepath.Base(src))
		}
		info, err := os.Stat(src)
		if err != nil {
			_ = zw.Close()
			return err
		}
		name := filepath.Base(src)
		if cnt := used[name]; cnt > 0 {
			ext := filepath.Ext(name)
			stem := strings.TrimSuffix(name, ext)
			name = fmt.Sprintf("%s (%d)%s", stem, cnt, ext)
		}
		used[filepath.Base(src)]++

		hdr, err := zip.FileInfoHeader(info)
		if err != nil {
			_ = zw.Close()
			return err
		}
		hdr.Name = name
		hdr.Method = zip.Deflate
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			_ = zw.Close()
			return err
		}
		in, err := os.Open(src)
		if err != nil {
			_ = zw.Close()
			return err
		}
		_, copyErr := io.Copy(w, in)
		_ = in.Close()
		if copyErr != nil {
			_ = zw.Close()
			return copyErr
		}
	}
	return zw.Close()
}

func unpackZipSafe(ctx context.Context, zipPath, destDir string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("invalid ZIP archive: %w", err)
	}
	defer zr.Close()

	if len(zr.File) > maxExtractedEntries {
		return fmt.Errorf("archive exceeds entry safety limit (%d files)", maxExtractedEntries)
	}

	cleanDest := filepath.Clean(destDir) + string(os.PathSeparator)
	var totalBytes int64

	for _, zf := range zr.File {
		if err := ctx.Err(); err != nil {
			return ErrCanceled
		}
		rel := filepath.FromSlash(zf.Name)
		target := filepath.Join(destDir, rel)
		if !strings.HasPrefix(filepath.Clean(target)+string(os.PathSeparator), cleanDest) && filepath.Clean(target) != filepath.Clean(destDir) {
			return fmt.Errorf("unsafe path traversal rejected in archive: %s", zf.Name)
		}
		if zf.Mode()&os.ModeSymlink != 0 {
			continue // Never materialize symlinks inside conversion staging
		}
		if zf.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		remain := maxExtractedTotalBytes - totalBytes
		if remain <= 0 {
			_ = rc.Close()
			return errors.New("archive unpacked size exceeds 2 GB budget")
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			_ = rc.Close()
			return err
		}
		written, copyErr := io.Copy(out, io.LimitReader(rc, remain+1))
		_ = out.Close()
		_ = rc.Close()
		if copyErr != nil {
			return copyErr
		}
		totalBytes += written
		if totalBytes > maxExtractedTotalBytes {
			return errors.New("archive unpacked size exceeds 2 GB budget")
		}
	}
	return nil
}

func unpackTarSafe(ctx context.Context, tarPath string, gzipped bool, destDir string) error {
	f, err := os.Open(tarPath)
	if err != nil {
		return err
	}
	defer f.Close()

	var r io.Reader = f
	if gzipped {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return fmt.Errorf("invalid GZIP stream: %w", err)
		}
		defer gz.Close()
		r = gz
	}

	tr := tar.NewReader(r)
	cleanDest := filepath.Clean(destDir) + string(os.PathSeparator)
	var totalBytes int64
	entries := 0

	for {
		if err := ctx.Err(); err != nil {
			return ErrCanceled
		}
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("invalid TAR stream: %w", err)
		}
		entries++
		if entries > maxExtractedEntries {
			return fmt.Errorf("archive exceeds entry safety limit (%d files)", maxExtractedEntries)
		}
		target := filepath.Join(destDir, filepath.FromSlash(hdr.Name))
		if !strings.HasPrefix(filepath.Clean(target)+string(os.PathSeparator), cleanDest) && filepath.Clean(target) != filepath.Clean(destDir) {
			return fmt.Errorf("unsafe path traversal rejected in TAR: %s", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			remain := maxExtractedTotalBytes - totalBytes
			if remain <= 0 {
				return errors.New("archive unpacked size exceeds 2 GB budget")
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
			if err != nil {
				return err
			}
			written, copyErr := io.Copy(out, io.LimitReader(tr, remain+1))
			_ = out.Close()
			if copyErr != nil {
				return copyErr
			}
			totalBytes += written
			if totalBytes > maxExtractedTotalBytes {
				return errors.New("archive unpacked size exceeds 2 GB budget")
			}
		}
	}
	return nil
}

func packDirectoryToZip(ctx context.Context, rootDir, outZip string) error {
	f, err := os.Create(outZip)
	if err != nil {
		return err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	walkErr := filepath.WalkDir(rootDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ErrCanceled
		}
		if path == rootDir || d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(rootDir, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		hdr, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		hdr.Method = zip.Deflate
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(w, in)
		_ = in.Close()
		return copyErr
	})
	if walkErr != nil {
		_ = zw.Close()
		return walkErr
	}
	return zw.Close()
}

func packDirectoryToTar(ctx context.Context, rootDir string, gzipped bool, outTar string) error {
	f, err := os.Create(outTar)
	if err != nil {
		return err
	}
	defer f.Close()

	var dst io.Writer = f
	var gz *gzip.Writer
	if gzipped {
		gz = gzip.NewWriter(f)
		dst = gz
	}
	tw := tar.NewWriter(dst)

	walkErr := filepath.WalkDir(rootDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ErrCanceled
		}
		if path == rootDir {
			return nil
		}
		rel, err := filepath.Rel(rootDir, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if d.IsDir() && !strings.HasSuffix(hdr.Name, "/") {
			hdr.Name += "/"
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tw, in)
		_ = in.Close()
		return copyErr
	})
	if walkErr != nil {
		_ = tw.Close()
		if gz != nil {
			_ = gz.Close()
		}
		return walkErr
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if gz != nil {
		return gz.Close()
	}
	return nil
}
