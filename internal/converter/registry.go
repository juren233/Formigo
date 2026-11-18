package converter

import (
	"path/filepath"
	"slices"
	"strings"
)

// NormalizeExt canonicalizes a filename or extension string (e.g. "foo.JPEG" -> "jpg", ".tar.gz" -> "tar.gz").
func NormalizeExt(nameOrExt string) string {
	s := strings.ToLower(strings.TrimSpace(nameOrExt))
	if s == "" {
		return ""
	}
	if s == "tar.gz" || s == ".tar.gz" || strings.HasSuffix(s, ".tar.gz") {
		return "tar.gz"
	}
	if strings.ContainsAny(s, `/\`) || (strings.Contains(s, ".") && !strings.HasPrefix(s, ".")) {
		ext := strings.TrimPrefix(filepath.Ext(s), ".")
		if ext != "" {
			s = ext
		}
	} else {
		s = strings.TrimPrefix(s, ".")
	}
	switch s {
	case "jpeg", "jpe", "jfif":
		return "jpg"
	case "tif":
		return "tiff"
	case "htm":
		return "html"
	case "markdown", "mdown":
		return "md"
	case "yml":
		return "yaml"
	case "tgz":
		return "tar.gz"
	}
	return s
}

// CategoryForExt classifies a canonical source extension into a Category.
func CategoryForExt(ext string) Category {
	ext = NormalizeExt(ext)
	switch ext {
	case "png", "jpg", "webp", "gif", "bmp", "tiff", "ico", "svg", "heic", "heif", "avif", "tga":
		return CategoryImage
	case "pdf":
		return CategoryPDF
	case "docx", "doc", "odt", "rtf", "wps", "txt", "md", "html", "log":
		return CategoryDocument
	case "xlsx", "xls", "xlsm", "ods", "et", "csv", "tsv":
		return CategorySpreadsheet
	case "pptx", "ppt", "odp", "dps":
		return CategoryPresentation
	case "epub":
		return CategoryEbook
	case "json", "yaml", "toml", "xml":
		return CategoryData
	case "srt", "vtt", "ass", "ssa":
		return CategorySubtitle
	case "mp3", "wav", "flac", "m4a", "aac", "ogg", "opus", "wma", "aiff":
		return CategoryAudio
	case "mp4", "mkv", "mov", "webm", "avi", "flv", "wmv", "m4v", "ts":
		return CategoryVideo
	case "zip", "tar", "tar.gz":
		return CategoryArchive
	default:
		return CategoryOther
	}
}

type routeRule struct {
	sources []string
	targets []string
	engines []EngineID // ordered by preference
}

var conversionRoutes = []routeRule{
	// Pure-Go native raster & vector images
	{
		sources: []string{"png", "jpg", "webp", "gif", "bmp", "tiff", "ico", "svg"},
		targets: []string{"png", "jpg", "webp", "bmp", "tiff", "gif", "ico", "pdf"},
		engines: []EngineID{EngineBuiltin},
	},
	// HEIC/HEIF via sips (macOS) or FFmpeg
	{
		sources: []string{"heic", "heif"},
		targets: []string{"jpg", "png", "webp", "tiff", "bmp", "ico", "pdf"},
		engines: []EngineID{EngineSips, EngineFFmpeg},
	},
	// AVIF / TGA via FFmpeg
	{
		sources: []string{"avif", "tga"},
		targets: []string{"png", "jpg", "webp", "bmp", "tiff", "ico", "pdf"},
		engines: []EngineID{EngineFFmpeg},
	},
	// Image -> Video via FFmpeg
	{
		sources: []string{"png", "jpg", "webp", "gif", "bmp"},
		targets: []string{"mp4", "webm"},
		engines: []EngineID{EngineFFmpeg},
	},
	// Image -> OCR Text via Tesseract
	{
		sources: []string{"png", "jpg", "webp", "bmp", "tiff"},
		targets: []string{"txt", "md"},
		engines: []EngineID{EngineTesseract},
	},

	// PDF native conversions (split/encrypt/decrypt/extract-images + pure-Go text extraction)
	{
		sources: []string{"pdf"},
		targets: []string{"txt", "md", "html", "pdf", "zip"},
		engines: []EngineID{EngineBuiltin},
	},
	// PDF -> PNG/JPG via Poppler or macOS sips
	{
		sources: []string{"pdf"},
		targets: []string{"png", "jpg"},
		engines: []EngineID{EnginePoppler, EngineSips},
	},

	// Text / Markdown / HTML / DOCX pure-Go document matrix (upgraded by Pandoc/LibreOffice for PDF)
	{
		sources: []string{"txt", "log", "md", "html"},
		targets: []string{"md", "html", "txt", "docx", "epub", "pdf"},
		engines: []EngineID{EngineBuiltin},
	},
	{
		sources: []string{"docx"},
		targets: []string{"md", "txt", "html", "epub"},
		engines: []EngineID{EngineBuiltin},
	},
	{
		sources: []string{"docx"},
		targets: []string{"pdf", "odt", "rtf"},
		engines: []EngineID{EngineLibreOffice, EnginePandoc},
	},
	{
		sources: []string{"doc", "odt", "rtf", "wps"},
		targets: []string{"pdf", "docx", "txt", "html", "odt", "rtf"},
		engines: []EngineID{EngineLibreOffice},
	},
	{
		sources: []string{"odt", "rtf"},
		targets: []string{"md", "html", "docx", "txt"},
		engines: []EngineID{EnginePandoc},
	},

	// Presentations via LibreOffice
	{
		sources: []string{"pptx", "ppt", "odp", "dps"},
		targets: []string{"pdf", "pptx", "odp", "html"},
		engines: []EngineID{EngineLibreOffice},
	},

	// eBooks (Pure Go EPUB reader/builder + LibreOffice/Pandoc optional PDF/DOCX)
	{
		sources: []string{"epub"},
		targets: []string{"md", "txt", "html", "docx"},
		engines: []EngineID{EngineBuiltin},
	},
	{
		sources: []string{"epub"},
		targets: []string{"pdf"},
		engines: []EngineID{EngineLibreOffice, EngineBuiltin},
	},

	// Spreadsheets & Tabular Data (Pure Go Excelize + CSV/TSV/JSON/YAML/MD/HTML)
	{
		sources: []string{"xlsx", "xlsm", "csv", "tsv"},
		targets: []string{"xlsx", "csv", "tsv", "json", "yaml", "md", "html"},
		engines: []EngineID{EngineBuiltin},
	},
	{
		sources: []string{"xlsx", "xls", "ods", "et", "csv"},
		targets: []string{"pdf", "ods", "xlsx"},
		engines: []EngineID{EngineLibreOffice},
	},

	// Structured Data interchange (Pure Go)
	{
		sources: []string{"json", "yaml", "toml", "xml"},
		targets: []string{"json", "yaml", "toml", "xml", "csv", "xlsx", "md", "html"},
		engines: []EngineID{EngineBuiltin},
	},

	// Subtitles (Pure Go)
	{
		sources: []string{"srt", "vtt", "ass", "ssa"},
		targets: []string{"srt", "vtt", "ass", "txt", "json"},
		engines: []EngineID{EngineBuiltin},
	},

	// Archives (Pure Go)
	{
		sources: []string{"zip", "tar", "tar.gz"},
		targets: []string{"zip", "tar", "tar.gz", "pdf"},
		engines: []EngineID{EngineBuiltin},
	},

	// Audio via FFmpeg
	{
		sources: []string{"mp3", "wav", "flac", "m4a", "aac", "ogg", "opus", "wma", "aiff"},
		targets: []string{"mp3", "wav", "flac", "m4a", "aac", "ogg", "opus", "wma"},
		engines: []EngineID{EngineFFmpeg},
	},

	// Video via FFmpeg (Video -> Video, Video -> GIF, Video -> Audio extraction)
	{
		sources: []string{"mp4", "mkv", "mov", "webm", "avi", "flv", "wmv", "m4v", "ts"},
		targets: []string{"mp4", "mkv", "mov", "webm", "avi", "gif", "mp3", "wav", "flac", "m4a", "aac", "ogg", "opus"},
		engines: []EngineID{EngineFFmpeg},
	},
}

// ResolveEngine finds the highest-priority available engine for (sourceExt -> targetExt).
func ResolveEngine(sourceExt, targetExt string, rt *Runtime) (EngineID, bool) {
	src := NormalizeExt(sourceExt)
	dst := NormalizeExt(targetExt)
	if src == "" || dst == "" {
		return "", false
	}

	// Any file can be packed into a .zip archive via builtin engine.
	if dst == "zip" && src != "zip" && src != "tar" && src != "tar.gz" && src != "pdf" {
		return EngineBuiltin, true
	}

	// Prefer LibreOffice over builtin text-PDF renderer when converting Office/Markdown/HTML -> PDF if installed.
	if dst == "pdf" && slices.Contains([]string{"md", "html", "txt", "docx", "xlsx", "csv"}, src) {
		if rt != nil && rt.Available(EngineLibreOffice) && slices.Contains([]string{"docx", "xlsx", "csv", "html", "txt"}, src) {
			return EngineLibreOffice, true
		}
	}

	for _, r := range conversionRoutes {
		if !slices.Contains(r.sources, src) || !slices.Contains(r.targets, dst) {
			continue
		}
		for _, eng := range r.engines {
			if rt == nil {
				if eng == EngineBuiltin {
					return eng, true
				}
				continue
			}
			if rt.Available(eng) {
				return eng, true
			}
		}
	}
	return "", false
}

// TargetsForExt returns supported target formats for sourceExt.
// When onlyAvailable is true, only targets backed by an active engine on rt are returned.
func TargetsForExt(sourceExt string, rt *Runtime, onlyAvailable bool) []TargetChoice {
	src := NormalizeExt(sourceExt)
	if src == "" {
		return nil
	}

	seen := make(map[string]int)
	var out []TargetChoice

	addChoice := func(dst string, engines []EngineID) {
		// Skip identity unless PDF (which supports encrypt/decrypt/re-save) or Image (quality/resize).
		if dst == src && src != "pdf" && CategoryForExt(src) != CategoryImage {
			return
		}
		var activeEngine EngineID
		avail := false
		for _, eng := range engines {
			if rt == nil {
				if eng == EngineBuiltin {
					activeEngine = eng
					avail = true
					break
				}
			} else if rt.Available(eng) {
				activeEngine = eng
				avail = true
				break
			}
		}
		if !avail && len(engines) > 0 {
			activeEngine = engines[0]
		}
		if idx, exists := seen[dst]; exists {
			// Upgrade existing entry if this route is available while the earlier one wasn't.
			if avail && !out[idx].Available {
				out[idx].Engine = activeEngine
				out[idx].Available = true
			}
			return
		}
		if onlyAvailable && !avail {
			return
		}
		seen[dst] = len(out)
		out = append(out, TargetChoice{
			Ext:       dst,
			Engine:    activeEngine,
			Available: avail,
		})
	}

	for _, r := range conversionRoutes {
		if !slices.Contains(r.sources, src) {
			continue
		}
		for _, dst := range r.targets {
			addChoice(dst, r.engines)
		}
	}

	// Universal fallback: any file can be packaged into ZIP.
	if src != "zip" {
		addChoice("zip", []EngineID{EngineBuiltin})
	}

	return out
}

// DefaultTargetFor chooses the best initial target format for a source extension,
// honoring remembered user preference if valid.
func DefaultTargetFor(sourceExt, remembered string, rt *Runtime) string {
	choices := TargetsForExt(sourceExt, rt, true)
	if len(choices) == 0 {
		return ""
	}
	rem := NormalizeExt(remembered)
	if rem != "" {
		for _, ch := range choices {
			if ch.Ext == rem {
				return ch.Ext
			}
		}
	}
	src := NormalizeExt(sourceExt)
	// Sensible ergonomic first-time defaults per format
	preferred := map[string]string{
		"png":    "webp",
		"jpg":    "webp",
		"webp":   "png",
		"heic":   "jpg",
		"heif":   "jpg",
		"svg":    "png",
		"ico":    "png",
		"bmp":    "png",
		"tiff":   "png",
		"pdf":    "md",
		"md":     "docx",
		"docx":   "md",
		"html":   "md",
		"txt":    "pdf",
		"epub":   "md",
		"xlsx":   "csv",
		"csv":    "xlsx",
		"tsv":    "xlsx",
		"json":   "yaml",
		"yaml":   "json",
		"toml":   "json",
		"xml":    "json",
		"srt":    "vtt",
		"vtt":    "srt",
		"ass":    "srt",
		"mp4":    "mp3",
		"mkv":    "mp4",
		"mov":    "mp4",
		"avi":    "mp4",
		"webm":   "mp4",
		"flac":   "mp3",
		"wav":    "mp3",
		"m4a":    "mp3",
		"mp3":    "wav",
		"zip":    "tar.gz",
		"tar.gz": "zip",
		"tar":    "zip",
	}[src]
	if preferred != "" {
		for _, ch := range choices {
			if ch.Ext == preferred {
				return ch.Ext
			}
		}
	}
	for _, ch := range choices {
		if ch.Ext != src {
			return ch.Ext
		}
	}
	return choices[0].Ext
}

// MatrixSummary groups active source->target capabilities by Category for UI/CLI inspection.
type MatrixGroup struct {
	Category Category `json:"category"`
	Sources  []string `json:"sources"`
	Targets  []string `json:"targets"`
}

// ActiveMatrix summarizes all currently active format conversions on rt.
func ActiveMatrix(rt *Runtime) []MatrixGroup {
	categories := []Category{
		CategoryImage,
		CategoryPDF,
		CategoryDocument,
		CategorySpreadsheet,
		CategoryPresentation,
		CategoryEbook,
		CategoryData,
		CategorySubtitle,
		CategoryAudio,
		CategoryVideo,
		CategoryArchive,
	}
	allExts := []string{
		"png", "jpg", "webp", "gif", "bmp", "tiff", "ico", "svg", "heic", "avif", "tga",
		"pdf",
		"md", "txt", "html", "docx", "doc", "odt", "rtf", "wps",
		"xlsx", "xls", "ods", "csv", "tsv",
		"pptx", "ppt", "odp",
		"epub",
		"json", "yaml", "toml", "xml",
		"srt", "vtt", "ass",
		"mp3", "wav", "flac", "m4a", "aac", "ogg", "opus", "wma",
		"mp4", "mkv", "mov", "webm", "avi", "flv", "wmv",
		"zip", "tar", "tar.gz",
	}

	var groups []MatrixGroup
	for _, cat := range categories {
		var srcs []string
		targetSet := make(map[string]bool)
		var tgts []string
		for _, ext := range allExts {
			if CategoryForExt(ext) != cat {
				continue
			}
			choices := TargetsForExt(ext, rt, true)
			// Ignore if only universal fallback zip is available for external-only formats (e.g. audio without ffmpeg).
			nonZip := 0
			for _, ch := range choices {
				if ch.Ext != "zip" || cat == CategoryArchive {
					nonZip++
				}
			}
			if nonZip == 0 {
				continue
			}
			srcs = append(srcs, ext)
			for _, ch := range choices {
				if ch.Ext == "zip" && cat != CategoryArchive && cat != CategoryPDF {
					continue
				}
				if !targetSet[ch.Ext] {
					targetSet[ch.Ext] = true
					tgts = append(tgts, ch.Ext)
				}
			}
		}
		if len(srcs) > 0 && len(tgts) > 0 {
			groups = append(groups, MatrixGroup{
				Category: cat,
				Sources:  srcs,
				Targets:  tgts,
			})
		}
	}
	return groups
}
