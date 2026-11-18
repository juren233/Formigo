package converter

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

func (p *Pipeline) convertPDF(
	ctx context.Context,
	engine EngineID,
	inPath, outPath, dstExt, workDir string,
	opts Options,
	report func(float64, string),
) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, ErrCanceled
	}

	// Special PDF operations first.
	switch opts.PDFOperation {
	case PDFOpSplit:
		return nil, splitPDFToZip(ctx, inPath, outPath, workDir, opts.PDFSplitSpan, opts.Password)
	case PDFOpEncrypt:
		if strings.TrimSpace(opts.Password) == "" {
			return nil, errors.New("PDF encryption requires a non-empty password")
		}
		conf := model.NewAESConfiguration(opts.Password, opts.Password, 256)
		return nil, api.EncryptFile(ctx, inPath, outPath, conf)
	case PDFOpDecrypt:
		if strings.TrimSpace(opts.Password) == "" {
			return nil, errors.New("PDF decryption requires the document password")
		}
		conf := model.NewDefaultConfiguration()
		conf.UserPW = opts.Password
		conf.OwnerPW = opts.Password
		return nil, api.DecryptFile(ctx, inPath, outPath, conf)
	case PDFOpExtractImages:
		return nil, extractPDFImagesToZip(ctx, inPath, outPath, workDir, opts.Password)
	}

	switch dstExt {
	case "pdf":
		// Optimization / copy when no special encrypt/decrypt requested
		data, err := os.ReadFile(inPath)
		if err != nil {
			return nil, err
		}
		return nil, os.WriteFile(outPath, data, 0o644)

	case "zip":
		return nil, splitPDFToZip(ctx, inPath, outPath, workDir, opts.PDFSplitSpan, opts.Password)

	case "txt", "md", "html":
		report(0.3, "Extracting PDF text streams")
		pages, err := extractPDFTextPages(ctx, inPath, workDir, opts.Password)
		if err != nil {
			return nil, err
		}
		var warnings []string
		nonEmpty := 0
		for _, pg := range pages {
			if strings.TrimSpace(pg) != "" {
				nonEmpty++
			}
		}
		if nonEmpty == 0 {
			warnings = append(warnings, "No selectable digital text streams found in PDF (document may be scanned images)")
		}
		title := safeStem(inPath)
		content := formatExtractedPDFPages(title, pages, dstExt)
		return warnings, os.WriteFile(outPath, []byte(content), 0o644)

	case "png", "jpg":
		return p.rasterizePDFPage(ctx, engine, inPath, outPath, dstExt, workDir)

	default:
		return nil, fmt.Errorf("unsupported PDF target format: .%s", dstExt)
	}
}

func (p *Pipeline) mergeImagesToPDF(
	ctx context.Context,
	imgPaths []string,
	outPath, workDir string,
	opts Options,
	report func(float64, string),
) ([]string, error) {
	if len(imgPaths) == 0 {
		return nil, errors.New("no images provided for PDF merge")
	}
	stagedImgs := make([]string, 0, len(imgPaths))
	var warnings []string

	for i, imgPath := range imgPaths {
		if err := ctx.Err(); err != nil {
			return nil, ErrCanceled
		}
		report(float64(i)/float64(len(imgPaths)), fmt.Sprintf("Preparing image %d/%d", i+1, len(imgPaths)))
		ext := NormalizeExt(imgPath)
		// pdfcpu natively imports PNG, JPEG, TIFF, WebP; we normalize all inputs (including SVG, ICO, BMP, GIF, HEIC) into clean PNG/JPG when needed.
		if (ext == "png" || ext == "jpg") && opts.ImageMaxWidth <= 0 {
			stagedImgs = append(stagedImgs, imgPath)
			continue
		}
		eng := EngineBuiltin
		if ext == "heic" || ext == "heif" {
			if p.rt.Available(EngineSips) {
				eng = EngineSips
			} else {
				eng = EngineFFmpeg
			}
		} else if ext == "avif" || ext == "tga" {
			eng = EngineFFmpeg
		}
		img, w, err := p.decodeAnyImage(ctx, eng, imgPath, ext, workDir, opts.ImageMaxWidth)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(imgPath), err)
		}
		warnings = append(warnings, w...)
		if opts.ImageMaxWidth > 0 && img.Bounds().Dx() > opts.ImageMaxWidth {
			nw := opts.ImageMaxWidth
			nh := int(float64(img.Bounds().Dy()) * float64(nw) / float64(img.Bounds().Dx()))
			if nh < 1 {
				nh = 1
			}
			img = resizeCatmullRom(img, nw, nh)
		}
		stageFile := filepath.Join(workDir, fmt.Sprintf("merge-img-%04d.png", i+1))
		if err := encodeImageToFile(img, stageFile, "png", opts.ImageQuality); err != nil {
			return nil, err
		}
		stagedImgs = append(stagedImgs, stageFile)
	}

	report(0.9, "Building multi-page PDF")
	if err := api.ImportImagesFile(ctx, stagedImgs, outPath, nil, nil); err != nil {
		return warnings, fmt.Errorf("failed to build PDF from images: %w", err)
	}
	return warnings, nil
}

func mergePDFsNative(ctx context.Context, inPaths []string, outPath string) error {
	if len(inPaths) == 0 {
		return errors.New("no PDF files provided")
	}
	return api.MergeCreateFile(ctx, inPaths, outPath, false, nil)
}

func splitPDFToZip(ctx context.Context, inPath, outZip, workDir string, span int, password string) error {
	if span <= 0 {
		span = 1
	}
	splitDir := filepath.Join(workDir, "pdf-split")
	if err := os.MkdirAll(splitDir, 0o755); err != nil {
		return err
	}
	var conf *model.Configuration
	if password != "" {
		conf = model.NewDefaultConfiguration()
		conf.UserPW = password
		conf.OwnerPW = password
	}
	if err := api.SplitFile(ctx, inPath, splitDir, span, conf); err != nil {
		return fmt.Errorf("PDF split failed: %w", err)
	}
	entries, err := os.ReadDir(splitDir)
	if err != nil {
		return err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".pdf") {
			files = append(files, filepath.Join(splitDir, e.Name()))
		}
	}
	sort.Strings(files)
	if len(files) == 0 {
		return errors.New("PDF split produced no output pages")
	}
	return packFilesToZip(ctx, files, outZip, nil)
}

func extractPDFImagesToZip(ctx context.Context, inPath, outZip, workDir, password string) error {
	imgDir := filepath.Join(workDir, "pdf-images")
	if err := os.MkdirAll(imgDir, 0o755); err != nil {
		return err
	}
	var conf *model.Configuration
	if password != "" {
		conf = model.NewDefaultConfiguration()
		conf.UserPW = password
		conf.OwnerPW = password
	}
	if err := api.ExtractImagesFile(ctx, inPath, imgDir, nil, conf); err != nil {
		return fmt.Errorf("PDF image extraction failed: %w", err)
	}
	entries, err := os.ReadDir(imgDir)
	if err != nil {
		return err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() {
			files = append(files, filepath.Join(imgDir, e.Name()))
		}
	}
	sort.Strings(files)
	if len(files) == 0 {
		return errors.New("no embedded images found in this PDF")
	}
	return packFilesToZip(ctx, files, outZip, nil)
}

func (p *Pipeline) rasterizePDFPage(ctx context.Context, engine EngineID, inPath, outPath, dstExt, workDir string) ([]string, error) {
	pageCount, _ := api.PageCountFile(ctx, inPath)
	var warnings []string
	if pageCount > 1 {
		warnings = append(warnings, fmt.Sprintf("Rendered page 1 of %d (use PDF Split or Extract Images for multi-page ZIP)", pageCount))
	}

	if engine == EnginePoppler || p.rt.Available(EnginePoppler) {
		bin := p.rt.Path(EnginePoppler)
		prefix := filepath.Join(workDir, "page")
		flagFmt := "-png"
		if dstExt == "jpg" {
			flagFmt = "-jpeg"
		}
		_, _, err := RunCommand(ctx, bin, []string{flagFmt, "-r", "150", "-f", "1", "-l", "1", "-singlefile", inPath, prefix}, nil)
		if err == nil {
			extRendered := ".png"
			if dstExt == "jpg" {
				extRendered = ".jpg"
			}
			rendered := prefix + extRendered
			if data, readErr := os.ReadFile(rendered); readErr == nil {
				return warnings, os.WriteFile(outPath, data, 0o644)
			}
		}
	}

	if p.rt.Available(EngineSips) {
		sipsBin := p.rt.Path(EngineSips)
		sipsFmt := "png"
		if dstExt == "jpg" {
			sipsFmt = "jpeg"
		}
		_, _, err := RunCommand(ctx, sipsBin, []string{"-s", "format", sipsFmt, inPath, "--out", outPath}, nil)
		if err != nil {
			return warnings, err
		}
		return warnings, nil
	}

	return nil, errors.New("PDF rasterization requires Poppler (pdftoppm) or macOS sips")
}

// extractPDFTextPages extracts readable text page-by-page using pdfcpu content extraction + pure-Go PDF text operator parser.
func extractPDFTextPages(ctx context.Context, inPath, workDir, password string) ([]string, error) {
	contentDir := filepath.Join(workDir, "pdf-content")
	if err := os.MkdirAll(contentDir, 0o755); err != nil {
		return nil, err
	}
	var conf *model.Configuration
	if password != "" {
		conf = model.NewDefaultConfiguration()
		conf.UserPW = password
		conf.OwnerPW = password
	}

	// Build any ToUnicode CMap mappings present in the raw PDF bytes so hex glyph strings decode cleanly.
	rawPDF, _ := os.ReadFile(inPath)
	cmaps := parsePDFToUnicodeMaps(rawPDF)

	if err := api.ExtractContentFile(ctx, inPath, contentDir, nil, conf); err == nil {
		entries, readErr := os.ReadDir(contentDir)
		if readErr == nil && len(entries) > 0 {
			type pageFile struct {
				pageNum int
				path    string
			}
			var pfs []pageFile
			reNum := regexp.MustCompile(`_Content_page_(\d+)\.txt$`)
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				num := len(pfs) + 1
				if m := reNum.FindStringSubmatch(e.Name()); len(m) == 2 {
					if v, convErr := strconv.Atoi(m[1]); convErr == nil {
						num = v
					}
				}
				pfs = append(pfs, pageFile{pageNum: num, path: filepath.Join(contentDir, e.Name())})
			}
			sort.Slice(pfs, func(i, j int) bool { return pfs[i].pageNum < pfs[j].pageNum })
			pages := make([]string, 0, len(pfs))
			for _, pf := range pfs {
				streamBytes, err := os.ReadFile(pf.path)
				if err != nil {
					continue
				}
				pages = append(pages, parsePDFContentStreamText(string(streamBytes), cmaps))
			}
			return pages, nil
		}
	}

	// Fallback: scan compressed/uncompressed streams directly from rawPDF.
	if len(rawPDF) > 0 {
		text := fallbackScanStreams(rawPDF, cmaps)
		return []string{text}, nil
	}
	return nil, errors.New("unable to extract PDF text content")
}

func formatExtractedPDFPages(title string, pages []string, dstExt string) string {
	switch dstExt {
	case "md":
		var sb strings.Builder
		sb.WriteString("# ")
		sb.WriteString(title)
		sb.WriteString("\n\n")
		for i, pg := range pages {
			trimmed := strings.TrimSpace(pg)
			if len(pages) > 1 {
				sb.WriteString(fmt.Sprintf("## Page %d\n\n", i+1))
			}
			if trimmed == "" {
				sb.WriteString("_(Empty page)_\n\n")
			} else {
				sb.WriteString(trimmed)
				sb.WriteString("\n\n")
			}
		}
		return sb.String()

	case "html":
		var sb strings.Builder
		sb.WriteString("<!DOCTYPE html>\n<html lang=\"zh-CN\">\n<head>\n<meta charset=\"utf-8\">\n<title>")
		sb.WriteString(html.EscapeString(title))
		sb.WriteString("</title>\n<style>body{max-width:820px;margin:2rem auto;padding:0 1rem;font-family:system-ui,-apple-system,sans-serif;line-height:1.65;color:#1f2937}section{margin-bottom:2rem;padding-bottom:1.5rem;border-bottom:1px solid #e5e7eb}p{white-space:pre-wrap}</style>\n</head>\n<body>\n<h1>")
		sb.WriteString(html.EscapeString(title))
		sb.WriteString("</h1>\n")
		for i, pg := range pages {
			sb.WriteString("<section>")
			if len(pages) > 1 {
				sb.WriteString(fmt.Sprintf("<h2>Page %d</h2>", i+1))
			}
			sb.WriteString("<p>")
			sb.WriteString(html.EscapeString(strings.TrimSpace(pg)))
			sb.WriteString("</p></section>\n")
		}
		sb.WriteString("</body>\n</html>\n")
		return sb.String()

	default:
		var parts []string
		for i, pg := range pages {
			t := strings.TrimSpace(pg)
			if len(pages) > 1 {
				parts = append(parts, fmt.Sprintf("--- Page %d ---\n%s", i+1, t))
			} else {
				parts = append(parts, t)
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n\n")) + "\n"
	}
}

// parsePDFContentStreamText interprets BT..ET blocks, Tj, TJ, ', ", Td/TD/T* operators into clean human text.
func parsePDFContentStreamText(stream string, cmap map[uint16]string) string {
	var out strings.Builder
	i := 0
	n := len(stream)
	inTextObj := false

	appendLineBreak := func() {
		s := out.String()
		if len(s) > 0 && !strings.HasSuffix(s, "\n") {
			out.WriteByte('\n')
		}
	}

	var pendingStrings []string

	for i < n {
		ch := stream[i]
		if ch <= ' ' {
			i++
			continue
		}
		if ch == '%' {
			for i < n && stream[i] != '\n' && stream[i] != '\r' {
				i++
			}
			continue
		}
		if ch == '(' {
			lit, next := readPDFLiteralString(stream, i, cmap)
			if inTextObj {
				pendingStrings = append(pendingStrings, lit)
			}
			i = next
			continue
		}
		if ch == '<' && i+1 < n && stream[i+1] != '<' {
			hx, next := readPDFHexString(stream, i, cmap)
			if inTextObj {
				pendingStrings = append(pendingStrings, hx)
			}
			i = next
			continue
		}
		if ch == '[' {
			i++
			var arrBuf strings.Builder
			for i < n && stream[i] != ']' {
				c2 := stream[i]
				if c2 == '(' {
					lit, next := readPDFLiteralString(stream, i, cmap)
					arrBuf.WriteString(lit)
					i = next
				} else if c2 == '<' && i+1 < n && stream[i+1] != '<' {
					hx, next := readPDFHexString(stream, i, cmap)
					arrBuf.WriteString(hx)
					i = next
				} else if c2 == '-' || (c2 >= '0' && c2 <= '9') || c2 == '.' {
					startNum := i
					i++
					for i < n && ((stream[i] >= '0' && stream[i] <= '9') || stream[i] == '.') {
						i++
					}
					if val, err := strconv.ParseFloat(stream[startNum:i], 64); err == nil && val < -180 {
						arrBuf.WriteByte(' ')
					}
				} else {
					i++
				}
			}
			if i < n && stream[i] == ']' {
				i++
			}
			if inTextObj {
				pendingStrings = append(pendingStrings, arrBuf.String())
			}
			continue
		}

		// Read operator or token
		startTok := i
		for i < n && stream[i] > ' ' && !strings.ContainsRune("()<>[]{}/%", rune(stream[i])) {
			i++
		}
		if startTok == i {
			i++
			continue
		}
		tok := stream[startTok:i]
		switch tok {
		case "BT":
			inTextObj = true
			pendingStrings = pendingStrings[:0]
		case "ET":
			inTextObj = false
			pendingStrings = pendingStrings[:0]
			appendLineBreak()
		case "Tj", "TJ":
			for _, s := range pendingStrings {
				out.WriteString(s)
			}
			pendingStrings = pendingStrings[:0]
		case "'", "\"":
			appendLineBreak()
			for _, s := range pendingStrings {
				out.WriteString(s)
			}
			pendingStrings = pendingStrings[:0]
		case "T*", "TD", "Td":
			if inTextObj {
				appendLineBreak()
			}
			pendingStrings = pendingStrings[:0]
		default:
			// Non-text operator discards stray literal args
			if !isNumericToken(tok) && !strings.HasPrefix(tok, "/") {
				pendingStrings = pendingStrings[:0]
			}
		}
	}

	// Clean up excessive blank lines
	lines := strings.Split(out.String(), "\n")
	var cleaned []string
	blankRun := 0
	for _, l := range lines {
		t := strings.TrimRightFunc(l, unicode.IsSpace)
		if t == "" {
			blankRun++
			if blankRun <= 1 && len(cleaned) > 0 {
				cleaned = append(cleaned, "")
			}
			continue
		}
		blankRun = 0
		cleaned = append(cleaned, t)
	}
	return strings.TrimSpace(strings.Join(cleaned, "\n"))
}

func isNumericToken(s string) bool {
	if s == "" {
		return false
	}
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}

func readPDFLiteralString(s string, pos int, cmap map[uint16]string) (string, int) {
	pos++ // skip '('
	depth := 1
	var raw []byte
	for pos < len(s) && depth > 0 {
		ch := s[pos]
		if ch == '\\' && pos+1 < len(s) {
			pos++
			esc := s[pos]
			switch esc {
			case 'n':
				raw = append(raw, '\n')
			case 'r':
				raw = append(raw, '\r')
			case 't':
				raw = append(raw, '\t')
			case 'b':
				raw = append(raw, '\b')
			case 'f':
				raw = append(raw, '\f')
			case '(', ')', '\\':
				raw = append(raw, esc)
			case '\r':
				if pos+1 < len(s) && s[pos+1] == '\n' {
					pos++
				}
			case '\n':
				// line continuation
			default:
				if esc >= '0' && esc <= '7' {
					oct := string(esc)
					for k := 0; k < 2 && pos+1 < len(s) && s[pos+1] >= '0' && s[pos+1] <= '7'; k++ {
						pos++
						oct += string(s[pos])
					}
					if v, err := strconv.ParseUint(oct, 8, 8); err == nil {
						raw = append(raw, byte(v))
					}
				} else {
					raw = append(raw, esc)
				}
			}
			pos++
			continue
		}
		if ch == '(' {
			depth++
			raw = append(raw, ch)
		} else if ch == ')' {
			depth--
			if depth > 0 {
				raw = append(raw, ch)
			}
		} else {
			raw = append(raw, ch)
		}
		pos++
	}
	return decodePDFStringBytes(raw, cmap), pos
}

func readPDFHexString(s string, pos int, cmap map[uint16]string) (string, int) {
	pos++ // skip '<'
	var hexChars []byte
	for pos < len(s) && s[pos] != '>' {
		c := s[pos]
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
			hexChars = append(hexChars, c)
		}
		pos++
	}
	if pos < len(s) && s[pos] == '>' {
		pos++
	}
	if len(hexChars)%2 == 1 {
		hexChars = append(hexChars, '0')
	}
	raw := make([]byte, len(hexChars)/2)
	_, _ = hex.Decode(raw, hexChars)
	return decodePDFStringBytes(raw, cmap), pos
}

func decodePDFStringBytes(raw []byte, cmap map[uint16]string) string {
	if len(raw) == 0 {
		return ""
	}
	// UTF-16BE BOM
	if len(raw) >= 2 && raw[0] == 0xFE && raw[1] == 0xFF {
		return decodeUTF16BEBytes(raw[2:])
	}
	// Try 2-byte CMap lookup if CMap is populated and length is even
	if len(cmap) > 0 && len(raw)%2 == 0 {
		var sb strings.Builder
		hits := 0
		for i := 0; i < len(raw); i += 2 {
			cid := uint16(raw[i])<<8 | uint16(raw[i+1])
			if mapped, ok := cmap[cid]; ok {
				sb.WriteString(mapped)
				hits++
			} else if raw[i] == 0 && raw[i+1] >= 0x20 && raw[i+1] < 0x7F {
				sb.WriteByte(raw[i+1])
			}
		}
		if hits > 0 {
			return sb.String()
		}
	}
	// Plain ASCII / Latin-1 / UTF-8 fallback
	var sb strings.Builder
	for _, b := range raw {
		if b == '\n' || b == '\r' || b == '\t' || (b >= 0x20 && b <= 0x7E) {
			sb.WriteByte(b)
		} else if b >= 0x80 {
			sb.WriteRune(rune(b))
		}
	}
	return sb.String()
}

func decodeUTF16BEBytes(b []byte) string {
	if len(b) < 2 {
		return ""
	}
	u16 := make([]uint16, len(b)/2)
	for i := 0; i < len(u16); i++ {
		u16[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
	}
	return string(utf16.Decode(u16))
}

// parsePDFToUnicodeMaps scans compressed & uncompressed PDF streams for beginbfchar / beginbfrange CMap tables.
func parsePDFToUnicodeMaps(rawPDF []byte) map[uint16]string {
	cmap := make(map[uint16]string)
	streams := collectDecodedPDFStreams(rawPDF)
	reChar := regexp.MustCompile(`<([0-9A-Fa-f]{2,4})>\s*<([0-9A-Fa-f]+)>`)
	reRange := regexp.MustCompile(`<([0-9A-Fa-f]{2,4})>\s*<([0-9A-Fa-f]{2,4})>\s*<([0-9A-Fa-f]+)>`)

	for _, st := range streams {
		if !bytes.Contains(st, []byte("begincmap")) && !bytes.Contains(st, []byte("beginbf")) {
			continue
		}
		s := string(st)
		// Parse beginbfrange blocks
		for _, block := range extractBetween(s, "beginbfrange", "endbfrange") {
			for _, m := range reRange.FindAllStringSubmatch(block, -1) {
				lo, err1 := strconv.ParseUint(m[1], 16, 16)
				hi, err2 := strconv.ParseUint(m[2], 16, 16)
				dstStart, err3 := strconv.ParseUint(m[3], 16, 32)
				if err1 != nil || err2 != nil || err3 != nil || hi < lo || hi-lo > 4096 {
					continue
				}
				for code := lo; code <= hi; code++ {
					r := rune(dstStart + (code - lo))
					cmap[uint16(code)] = string(r)
				}
			}
		}
		// Parse beginbfchar blocks
		for _, block := range extractBetween(s, "beginbfchar", "endbfchar") {
			for _, m := range reChar.FindAllStringSubmatch(block, -1) {
				src, err := strconv.ParseUint(m[1], 16, 16)
				if err != nil {
					continue
				}
				dstBytes, err := hex.DecodeString(m[2])
				if err != nil || len(dstBytes) == 0 {
					continue
				}
				if len(dstBytes) >= 2 && len(dstBytes)%2 == 0 {
					cmap[uint16(src)] = decodeUTF16BEBytes(dstBytes)
				} else {
					cmap[uint16(src)] = string(dstBytes)
				}
			}
		}
	}
	return cmap
}

func extractBetween(s, startMarker, endMarker string) []string {
	var out []string
	rem := s
	for {
		idx := strings.Index(rem, startMarker)
		if idx < 0 {
			break
		}
		rem = rem[idx+len(startMarker):]
		end := strings.Index(rem, endMarker)
		if end < 0 {
			break
		}
		out = append(out, rem[:end])
		rem = rem[end+len(endMarker):]
	}
	return out
}

func collectDecodedPDFStreams(raw []byte) [][]byte {
	var out [][]byte
	pos := 0
	for pos < len(raw) {
		idx := bytes.Index(raw[pos:], []byte("stream"))
		if idx < 0 {
			break
		}
		streamPos := pos + idx + len("stream")
		// Ensure it wasn't "endstream"
		if pos+idx >= 3 && bytes.Equal(raw[pos+idx-3:pos+idx], []byte("end")) {
			pos = streamPos
			continue
		}
		if streamPos < len(raw) && raw[streamPos] == '\r' {
			streamPos++
		}
		if streamPos < len(raw) && raw[streamPos] == '\n' {
			streamPos++
		} else {
			pos = streamPos
			continue
		}
		endRel := bytes.Index(raw[streamPos:], []byte("endstream"))
		if endRel < 0 {
			break
		}
		body := bytes.TrimRight(raw[streamPos:streamPos+endRel], "\r\n")
		pos = streamPos + endRel + len("endstream")

		if zr, err := zlib.NewReader(bytes.NewReader(body)); err == nil {
			decoded, err := io.ReadAll(io.LimitReader(zr, 8*1024*1024))
			_ = zr.Close()
			if err == nil && len(decoded) > 0 {
				out = append(out, decoded)
				continue
			}
		}
		out = append(out, body)
	}
	return out
}

func fallbackScanStreams(rawPDF []byte, cmap map[uint16]string) string {
	var parts []string
	for _, st := range collectDecodedPDFStreams(rawPDF) {
		if bytes.Contains(st, []byte("BT")) && bytes.Contains(st, []byte("ET")) {
			t := parsePDFContentStreamText(string(st), cmap)
			if t != "" {
				parts = append(parts, t)
			}
		}
	}
	return strings.Join(parts, "\n\n")
}
