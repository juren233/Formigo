package converter

import (
	"time"
)

// Category groups file formats by media/document domain.
type Category string

const (
	CategoryImage        Category = "image"
	CategoryPDF          Category = "pdf"
	CategoryDocument     Category = "document"
	CategorySpreadsheet  Category = "spreadsheet"
	CategoryPresentation Category = "presentation"
	CategoryEbook        Category = "ebook"
	CategoryData         Category = "data"
	CategorySubtitle     Category = "subtitle"
	CategoryAudio        Category = "audio"
	CategoryVideo        Category = "video"
	CategoryArchive      Category = "archive"
	CategoryOther        Category = "other"
)

// EngineID identifies the underlying processor performing a conversion.
type EngineID string

const (
	EngineBuiltin     EngineID = "builtin"
	EngineFFmpeg      EngineID = "ffmpeg"
	EngineLibreOffice EngineID = "libreoffice"
	EnginePandoc      EngineID = "pandoc"
	EnginePoppler     EngineID = "poppler"
	EngineTesseract   EngineID = "tesseract"
	EngineSips        EngineID = "sips"
)

// Stage describes the lifecycle state of a conversion job.
type Stage string

const (
	StagePending    Stage = "pending"
	StageQueued     Stage = "queued"
	StagePreparing  Stage = "preparing"
	StageConverting Stage = "converting"
	StageValidating Stage = "validating"
	StageDone       Stage = "done"
	StageFailed     Stage = "failed"
	StageCanceled   Stage = "canceled"
)

// PDFOperation specifies special PDF processing modes.
type PDFOperation string

const (
	PDFOpConvert       PDFOperation = "convert"
	PDFOpSplit         PDFOperation = "split"
	PDFOpEncrypt       PDFOperation = "encrypt"
	PDFOpDecrypt       PDFOperation = "decrypt"
	PDFOpExtractImages PDFOperation = "extract-images"
)

// MergeMode specifies multi-file merge workflows.
type MergeMode string

const (
	MergeImagesToPDF MergeMode = "images-to-pdf"
	MergePDFs        MergeMode = "merge-pdfs"
	MergeFilesToZip  MergeMode = "files-to-zip"
)

// Options carries format-specific conversion parameters.
type Options struct {
	ImageQuality   int          `json:"imageQuality,omitempty"`   // 1..100 (default 90)
	ImageMaxWidth  int          `json:"imageMaxWidth,omitempty"`  // 0 = keep original
	VideoCodec     string       `json:"videoCodec,omitempty"`     // h264, h265, av1, vp9, copy
	AudioBitrate   string       `json:"audioBitrate,omitempty"`   // 128k, 192k, 256k, 320k
	PDFOperation   PDFOperation `json:"pdfOperation,omitempty"`   // convert, split, encrypt, decrypt, extract-images
	PDFSplitSpan   int          `json:"pdfSplitSpan,omitempty"`   // pages per split PDF (default 1)
	Password       string       `json:"-"`                        // omitted from JSON serialization for safety
	TextEncoding   string       `json:"textEncoding,omitempty"`   // auto, utf-8, gb18030, utf-16le, utf-16be
	OCRLanguage    string       `json:"ocrLanguage,omitempty"`    // e.g. chi_sim+eng or eng
	Overwrite      bool         `json:"overwrite,omitempty"`      // overwrite existing output instead of auto-numbering
}

// DefaultOptions returns standard conversion defaults.
func DefaultOptions() Options {
	return Options{
		ImageQuality: 90,
		VideoCodec:   "h264",
		AudioBitrate: "192k",
		PDFOperation: PDFOpConvert,
		PDFSplitSpan: 1,
		TextEncoding: "auto",
		OCRLanguage:  "chi_sim+eng",
	}
}

// ProgressEvent reports stage and fractional completion [0..1] (negative for indeterminate).
type ProgressEvent struct {
	Stage    Stage
	Progress float64
	Detail   string
}

// ProgressFunc receives live conversion progress updates.
type ProgressFunc func(ProgressEvent)

// Request describes a single-file or multi-file conversion job.
type Request struct {
	InputPaths []string
	TargetExt  string
	OutputPath string // explicit full file path (optional)
	OutputDir  string // target directory if OutputPath is empty
	MergeMode  MergeMode
	Options    Options
}

// Result describes a completed conversion output.
type Result struct {
	InputPaths []string      `json:"inputPaths"`
	OutputPath string        `json:"outputPath"`
	SourceExt  string        `json:"sourceExt"`
	TargetExt  string        `json:"targetExt"`
	Engine     EngineID      `json:"engine"`
	OutputSize int64         `json:"outputSize"`
	Elapsed    time.Duration `json:"elapsed"`
	Warnings   []string      `json:"warnings,omitempty"`
}

// EngineStatus describes whether a conversion backend is ready on the current machine.
type EngineStatus struct {
	ID          EngineID `json:"id"`
	Name        string   `json:"name"`
	Available   bool     `json:"available"`
	Builtin     bool     `json:"builtin"`
	Path        string   `json:"path,omitempty"`
	Version     string   `json:"version,omitempty"`
	Description string   `json:"description"`
}

// TargetChoice describes a valid output target format and the engine that will execute it.
type TargetChoice struct {
	Ext       string   `json:"ext"`
	Engine    EngineID `json:"engine"`
	Available bool     `json:"available"`
}
