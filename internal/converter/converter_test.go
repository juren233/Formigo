package converter

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func newTestPipeline(t *testing.T) *Pipeline {
	t.Helper()
	rt := NewRuntime(nil)
	p, err := NewPipeline(rt)
	if err != nil {
		t.Fatalf("NewPipeline: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p
}

func TestImageSVGICOWebPRoundTrip(t *testing.T) {
	p := newTestPipeline(t)
	dir := t.TempDir()

	svgPath := filepath.Join(dir, "logo.svg")
	svgContent := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 128 128">
  <rect x="8" y="8" width="112" height="112" rx="24" fill="#4F46E5"/>
  <circle cx="64" cy="64" r="32" fill="#FFFFFF"/>
</svg>`
	if err := os.WriteFile(svgPath, []byte(svgContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// SVG -> PNG (with max width 256)
	opts := DefaultOptions()
	opts.ImageMaxWidth = 256
	resPNG, err := p.Convert(context.Background(), Request{
		InputPaths: []string{svgPath},
		TargetExt:  "png",
		OutputDir:  dir,
		Options:    opts,
	}, nil)
	if err != nil {
		t.Fatalf("SVG -> PNG failed: %v", err)
	}

	// PNG -> WebP
	resWebP, err := p.Convert(context.Background(), Request{
		InputPaths: []string{resPNG.OutputPath},
		TargetExt:  "webp",
		OutputDir:  dir,
		Options:    DefaultOptions(),
	}, nil)
	if err != nil {
		t.Fatalf("PNG -> WebP failed: %v", err)
	}

	// WebP -> Multi-size ICO
	resICO, err := p.Convert(context.Background(), Request{
		InputPaths: []string{resWebP.OutputPath},
		TargetExt:  "ico",
		OutputDir:  dir,
		Options:    DefaultOptions(),
	}, nil)
	if err != nil {
		t.Fatalf("WebP -> ICO failed: %v", err)
	}

	// ICO -> JPG & BMP & TIFF
	for _, dst := range []string{"jpg", "bmp", "tiff"} {
		res, err := p.Convert(context.Background(), Request{
			InputPaths: []string{resICO.OutputPath},
			TargetExt:  dst,
			OutputDir:  dir,
			Options:    DefaultOptions(),
		}, nil)
		if err != nil {
			t.Fatalf("ICO -> %s failed: %v", dst, err)
		}
		if res.OutputSize == 0 {
			t.Fatalf("ICO -> %s output empty", dst)
		}
	}
}

func TestPDFWorkflowsAndChineseTextRoundTrip(t *testing.T) {
	p := newTestPipeline(t)
	dir := t.TempDir()

	// 1. Markdown with Chinese + English -> Vector PDF -> Extract back to Markdown
	mdPath := filepath.Join(dir, "notes.md")
	mdContent := "# 项目架构说明\n\nFormigo 支持纯 Go 离线格式互转与跨平台 GPU 界面。\n\n- 零 CGO 依赖\n- 原子文件写入校验\n"
	if err := os.WriteFile(mdPath, []byte(mdContent), 0o644); err != nil {
		t.Fatal(err)
	}

	resPDF, err := p.Convert(context.Background(), Request{
		InputPaths: []string{mdPath},
		TargetExt:  "pdf",
		OutputDir:  dir,
		Options:    DefaultOptions(),
	}, nil)
	if err != nil {
		t.Fatalf("MD -> PDF failed: %v", err)
	}

	resExtracted, err := p.Convert(context.Background(), Request{
		InputPaths: []string{resPDF.OutputPath},
		TargetExt:  "txt",
		OutputDir:  dir,
		Options:    DefaultOptions(),
	}, nil)
	if err != nil {
		t.Fatalf("PDF -> TXT failed: %v", err)
	}
	extractedBytes, _ := os.ReadFile(resExtracted.OutputPath)
	extractedText := string(extractedBytes)
	if !strings.Contains(extractedText, "项目架构说明") || !strings.Contains(extractedText, "Formigo") {
		t.Fatalf("expected Chinese & English text in extracted PDF output, got: %q", extractedText)
	}

	// 2. Images -> Multi-page PDF -> Split PDF (ZIP) -> Encrypt & Decrypt PDF
	svgA := filepath.Join(dir, "p1.svg")
	svgB := filepath.Join(dir, "p2.svg")
	_ = os.WriteFile(svgA, []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><rect width="100" height="100" fill="#10B981"/></svg>`), 0o644)
	_ = os.WriteFile(svgB, []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><rect width="100" height="100" fill="#3B82F6"/></svg>`), 0o644)

	mergedPDF, err := p.Convert(context.Background(), Request{
		InputPaths: []string{svgA, svgB},
		MergeMode:  MergeImagesToPDF,
		OutputDir:  dir,
		Options:    DefaultOptions(),
	}, nil)
	if err != nil {
		t.Fatalf("Images -> PDF merge failed: %v", err)
	}

	// Split 2-page PDF into ZIP
	splitOpts := DefaultOptions()
	splitOpts.PDFOperation = PDFOpSplit
	splitOpts.PDFSplitSpan = 1
	splitZip, err := p.Convert(context.Background(), Request{
		InputPaths: []string{mergedPDF.OutputPath},
		OutputDir:  dir,
		Options:    splitOpts,
	}, nil)
	if err != nil {
		t.Fatalf("PDF Split failed: %v", err)
	}
	zr, err := zip.OpenReader(splitZip.OutputPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(zr.File) != 2 {
		t.Fatalf("expected 2 split PDFs in zip, got %d", len(zr.File))
	}
	_ = zr.Close()

	// Encrypt & Decrypt PDF
	encOpts := DefaultOptions()
	encOpts.PDFOperation = PDFOpEncrypt
	encOpts.Password = "s3cr3t-pass!"
	encPDF, err := p.Convert(context.Background(), Request{
		InputPaths: []string{mergedPDF.OutputPath},
		OutputDir:  dir,
		Options:    encOpts,
	}, nil)
	if err != nil {
		t.Fatalf("PDF Encrypt failed: %v", err)
	}

	decOpts := DefaultOptions()
	decOpts.PDFOperation = PDFOpDecrypt
	decOpts.Password = "s3cr3t-pass!"
	decPDF, err := p.Convert(context.Background(), Request{
		InputPaths: []string{encPDF.OutputPath},
		OutputDir:  dir,
		Options:    decOpts,
	}, nil)
	if err != nil {
		t.Fatalf("PDF Decrypt failed: %v", err)
	}

	// Extract images from decrypted PDF
	extImgOpts := DefaultOptions()
	extImgOpts.PDFOperation = PDFOpExtractImages
	imgZip, err := p.Convert(context.Background(), Request{
		InputPaths: []string{decPDF.OutputPath},
		OutputDir:  dir,
		Options:    extImgOpts,
	}, nil)
	if err != nil {
		t.Fatalf("PDF ExtractImages failed: %v", err)
	}
	if imgZip.OutputSize == 0 {
		t.Fatal("extracted images zip is empty")
	}
}

func TestGB18030SpreadsheetAndDataInterchange(t *testing.T) {
	p := newTestPipeline(t)
	dir := t.TempDir()

	// Encode Chinese CSV in GB18030 (Windows CN Excel style) to verify auto-detection!
	utf8CSV := "姓名,部门,得分\n张三,研发部,98.5\n李四,设计部,95\n"
	gbkBytes, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(utf8CSV))
	if err != nil {
		t.Fatal(err)
	}
	csvPath := filepath.Join(dir, "report.csv")
	if err := os.WriteFile(csvPath, gbkBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	// GB18030 CSV -> XLSX
	resXLSX, err := p.Convert(context.Background(), Request{
		InputPaths: []string{csvPath},
		TargetExt:  "xlsx",
		OutputDir:  dir,
		Options:    DefaultOptions(),
	}, nil)
	if err != nil {
		t.Fatalf("CSV -> XLSX failed: %v", err)
	}

	// XLSX -> JSON -> YAML -> TOML -> XML -> Markdown
	curPath := resXLSX.OutputPath
	for _, nextExt := range []string{"json", "yaml", "toml", "xml", "md"} {
		res, err := p.Convert(context.Background(), Request{
			InputPaths: []string{curPath},
			TargetExt:  nextExt,
			OutputDir:  dir,
			Options:    DefaultOptions(),
		}, nil)
		if err != nil {
			t.Fatalf("%s -> %s failed: %v", filepath.Base(curPath), nextExt, err)
		}
		curPath = res.OutputPath
	}

	mdOut, _ := os.ReadFile(curPath)
	if !strings.Contains(string(mdOut), "张三") || !strings.Contains(string(mdOut), "研发部") {
		t.Fatalf("Chinese characters lost across data pipeline: %s", string(mdOut))
	}
}

func TestDOCXAndEPUBRoundTrip(t *testing.T) {
	p := newTestPipeline(t)
	dir := t.TempDir()

	mdPath := filepath.Join(dir, "book.md")
	md := "# 第一章 导言\n\n这是 **加粗文字** 与 `行内代码` 测试。\n\n| 格式 | 状态 |\n| --- | --- |\n| DOCX | 支持 |\n| EPUB | 支持 |\n"
	if err := os.WriteFile(mdPath, []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}

	// MD -> DOCX -> HTML -> MD
	resDOCX, err := p.Convert(context.Background(), Request{
		InputPaths: []string{mdPath},
		TargetExt:  "docx",
		OutputDir:  dir,
		Options:    DefaultOptions(),
	}, nil)
	if err != nil {
		t.Fatalf("MD -> DOCX failed: %v", err)
	}

	resFromDOCX, err := p.Convert(context.Background(), Request{
		InputPaths: []string{resDOCX.OutputPath},
		TargetExt:  "md",
		OutputDir:  dir,
		Options:    DefaultOptions(),
	}, nil)
	if err != nil {
		t.Fatalf("DOCX -> MD failed: %v", err)
	}
	docxMD, _ := os.ReadFile(resFromDOCX.OutputPath)
	if !strings.Contains(string(docxMD), "第一章 导言") || !strings.Contains(string(docxMD), "DOCX") {
		t.Fatalf("unexpected DOCX -> MD content: %s", string(docxMD))
	}

	// MD -> EPUB 3.0 (verify uncompressed mimetype first entry!) -> TXT
	resEPUB, err := p.Convert(context.Background(), Request{
		InputPaths: []string{mdPath},
		TargetExt:  "epub",
		OutputDir:  dir,
		Options:    DefaultOptions(),
	}, nil)
	if err != nil {
		t.Fatalf("MD -> EPUB failed: %v", err)
	}

	zr, err := zip.OpenReader(resEPUB.OutputPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(zr.File) == 0 || zr.File[0].Name != "mimetype" || zr.File[0].Method != zip.Store {
		t.Fatalf("EPUB spec violation: first entry must be uncompressed mimetype")
	}
	_ = zr.Close()

	resEPUBTxt, err := p.Convert(context.Background(), Request{
		InputPaths: []string{resEPUB.OutputPath},
		TargetExt:  "txt",
		OutputDir:  dir,
		Options:    DefaultOptions(),
	}, nil)
	if err != nil {
		t.Fatalf("EPUB -> TXT failed: %v", err)
	}
	epubTxt, _ := os.ReadFile(resEPUBTxt.OutputPath)
	if !strings.Contains(string(epubTxt), "第一章 导言") {
		t.Fatalf("unexpected EPUB -> TXT content: %s", string(epubTxt))
	}
}

func TestSubtitlesArchivesAndZipSlipDefense(t *testing.T) {
	p := newTestPipeline(t)
	dir := t.TempDir()

	// Subtitle chain: SRT -> VTT -> ASS -> TXT
	srtPath := filepath.Join(dir, "sub.srt")
	srt := "1\n00:00:01,250 --> 00:00:04,500\n你好，欢迎使用 Formigo！\n\n2\n00:00:05,000 --> 00:00:08,100\n<i>第二行双语字幕</i>\n"
	_ = os.WriteFile(srtPath, []byte(srt), 0o644)

	resVTT, err := p.Convert(context.Background(), Request{
		InputPaths: []string{srtPath}, TargetExt: "vtt", OutputDir: dir, Options: DefaultOptions(),
	}, nil)
	if err != nil {
		t.Fatalf("SRT -> VTT failed: %v", err)
	}
	resASS, err := p.Convert(context.Background(), Request{
		InputPaths: []string{resVTT.OutputPath}, TargetExt: "ass", OutputDir: dir, Options: DefaultOptions(),
	}, nil)
	if err != nil {
		t.Fatalf("VTT -> ASS failed: %v", err)
	}
	resTXT, err := p.Convert(context.Background(), Request{
		InputPaths: []string{resASS.OutputPath}, TargetExt: "txt", OutputDir: dir, Options: DefaultOptions(),
	}, nil)
	if err != nil {
		t.Fatalf("ASS -> TXT failed: %v", err)
	}
	txtData, _ := os.ReadFile(resTXT.OutputPath)
	if !strings.Contains(string(txtData), "欢迎使用 Formigo") {
		t.Fatalf("subtitle text mismatch: %s", string(txtData))
	}

	// Archive chain: Pack files to ZIP -> TAR.GZ -> ZIP, plus collision naming check
	resZip, err := p.Convert(context.Background(), Request{
		InputPaths: []string{srtPath, resTXT.OutputPath},
		MergeMode:  MergeFilesToZip,
		OutputDir:  dir,
		Options:    DefaultOptions(),
	}, nil)
	if err != nil {
		t.Fatalf("MergeFilesToZip failed: %v", err)
	}
	resTGZ, err := p.Convert(context.Background(), Request{
		InputPaths: []string{resZip.OutputPath}, TargetExt: "tar.gz", OutputDir: dir, Options: DefaultOptions(),
	}, nil)
	if err != nil {
		t.Fatalf("ZIP -> TAR.GZ failed: %v", err)
	}
	resZip2, err := p.Convert(context.Background(), Request{
		InputPaths: []string{resTGZ.OutputPath}, TargetExt: "zip", OutputDir: dir, Options: DefaultOptions(),
	}, nil)
	if err != nil {
		t.Fatalf("TAR.GZ -> ZIP failed: %v", err)
	}
	if resZip2.OutputPath == resZip.OutputPath {
		t.Fatal("expected auto-numbered unique destination instead of clobbering existing zip")
	}

	// Zip-Slip path traversal defense test
	evilZip := filepath.Join(dir, "evil.zip")
	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	w, _ := zw.Create("../../escaped.txt")
	_, _ = w.Write([]byte("pwn"))
	_ = zw.Close()
	_ = os.WriteFile(evilZip, zbuf.Bytes(), 0o644)

	_, err = p.Convert(context.Background(), Request{
		InputPaths: []string{evilZip}, TargetExt: "tar", OutputDir: dir, Options: DefaultOptions(),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "unsafe path traversal") {
		t.Fatalf("expected Zip-Slip traversal error, got: %v", err)
	}
}

func TestFFmpegMediaConversionWhenAvailable(t *testing.T) {
	p := newTestPipeline(t)
	if !p.Runtime().Available(EngineFFmpeg) {
		t.Skip("FFmpeg not installed on test host")
	}
	dir := t.TempDir()

	// Create SVG -> PNG -> MP4 -> MP3 & GIF
	svgPath := filepath.Join(dir, "clip.svg")
	_ = os.WriteFile(svgPath, []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 320 180"><rect width="320" height="180" fill="#4F46E5"/></svg>`), 0o644)

	pngRes, err := p.Convert(context.Background(), Request{
		InputPaths: []string{svgPath}, TargetExt: "png", OutputDir: dir, Options: DefaultOptions(),
	}, nil)
	if err != nil {
		t.Fatalf("SVG -> PNG: %v", err)
	}

	mp4Res, err := p.Convert(context.Background(), Request{
		InputPaths: []string{pngRes.OutputPath}, TargetExt: "mp4", OutputDir: dir, Options: DefaultOptions(),
	}, nil)
	if err != nil {
		t.Fatalf("PNG -> MP4: %v", err)
	}

	gifRes, err := p.Convert(context.Background(), Request{
		InputPaths: []string{mp4Res.OutputPath}, TargetExt: "gif", OutputDir: dir, Options: DefaultOptions(),
	}, nil)
	if err != nil || gifRes.OutputSize == 0 {
		t.Fatalf("MP4 -> GIF failed: %v", err)
	}
}
