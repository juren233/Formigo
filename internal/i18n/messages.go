package i18n

import (
	"os"
	"strings"
)

// Lang identifies a supported UI language.
type Lang string

const (
	LangAuto Lang = "auto"
	LangZH   Lang = "zh"
	LangEN   Lang = "en"
)

// DetectSystemLang inspects standard locale environment variables and defaults to Chinese if zh is matched.
func DetectSystemLang(localeHint string) Lang {
	candidates := []string{
		localeHint,
		os.Getenv("LC_ALL"),
		os.Getenv("LC_MESSAGES"),
		os.Getenv("LANG"),
	}
	for _, c := range candidates {
		lower := strings.ToLower(strings.TrimSpace(c))
		if lower == "" {
			continue
		}
		if strings.HasPrefix(lower, "zh") || strings.Contains(lower, "hans") || strings.Contains(lower, "hant") {
			return LangZH
		}
		if strings.HasPrefix(lower, "en") {
			return LangEN
		}
	}
	return LangZH
}

// Resolve returns the concrete language for rendering.
func Resolve(pref Lang, localeHint string) Lang {
	if pref == LangZH || pref == LangEN {
		return pref
	}
	return DetectSystemLang(localeHint)
}

// Catalog holds localized UI text.
type Catalog struct {
	AppName        string
	AppSubtitle    string
	NavGroupWork   string
	NavGroupSystem string
	NavConvert     string
	NavMerge       string
	NavEngines     string
	NavSettings    string
	AddFiles       string
	AddFolder      string
	ClearCompleted string
	ClearAll       string
	StartAll       string
	CancelAll      string
	DropTitle       string
	DropHint        string
	DropTitleMobile string
	DropHintMobile  string
	DropActive      string
	DropAppendHint  string
	EmptyQueueHint  string

	FilterAll     string
	FilterPending string
	FilterActive  string
	FilterDone    string
	FilterFailed  string

	BatchTargetLabel string
	ApplyToAll       string
	MobileOptionsBtn string
	OutputDirLabel   string
	SameAsSource     string
	CustomDir        string
	ChooseDir        string
	OpenDir          string

	MergeModeLabel     string
	MergeImagesToPDF   string
	MergePDFs          string
	MergeFilesToZip    string
	MergeImgTitle      string
	MergeImgDesc       string
	MergePDFTitle      string
	MergePDFDesc       string
	MergeZipTitle      string
	MergeZipDesc       string
	MergeShortImgPDF   string
	MergeShortPDF      string
	MergeShortZip      string
	MergeOutputName    string
	StartMerge         string
	MoveUp             string
	MoveDown           string
	MergeEmptyHint     string
	MergeSuccessPrefix string

	InspectorTitle     string
	ImageQuality       string
	ImageMaxWidth      string
	ImageMaxWidthHint  string
	VideoCodec         string
	AudioBitrate       string
	PDFOperation       string
	PDFOpConvert       string
	PDFOpSplit         string
	PDFOpEncrypt       string
	PDFOpDecrypt       string
	PDFOpExtractImages string
	PDFSplitSpan       string
	PDFPassword        string
	PDFPasswordHint    string
	TextEncoding       string
	OCRHint            string

	StagePending    string
	StageQueued     string
	StagePreparing  string
	StageConverting string
	StageValidating string
	StageDone       string
	StageFailed     string
	StageCanceled   string

	ActionConvert  string
	ActionRetry    string
	ActionCancel   string
	ActionOpen     string
	ActionReveal   string
	ActionSaveCopy string
	SavedToast     string
	ActionRemove   string
	ActionCopyPath string
	BatchDoneToast string

	EnginesTitle       string
	EnginesSubtitle    string
	RescanEngines      string
	EngineBuiltinTitle string
	EngineBuiltinDesc  string
	EngineAvailable    string
	EngineMissing      string
	FormatsMatrixTitle string

	SettingsTitle       string
	SettingLanguage     string
	DescLanguage        string
	LangAutoLabel       string
	LangZHLabel         string
	LangENLabel         string
	SettingAppearance   string
	DescAppearance      string
	ThemeSystem         string
	ThemeLight          string
	ThemeDark           string
	SettingOutputMode   string
	DescOutputMode      string
	SettingConflict     string
	DescConflict        string
	ConflictRename      string
	ConflictOverwrite   string
	SettingConcurrency  string
	DescConcurrency     string
	CustomEnginePaths   string
	BrowseBinary        string
	SaveSettings        string
	SettingsSavedToast  string
	ResetFormatMemory   string
	FormatMemoryCleared string
}

var zhCatalog = Catalog{
	AppName:        "Formigo",
	AppSubtitle:    "全平台本地离线格式转换引擎",
	NavGroupWork:   "工作台",
	NavGroupSystem: "引擎与系统",
	NavConvert:     "批量转换",
	NavMerge:       "合并工坊",
	NavEngines:     "引擎与格式",
	NavSettings:    "偏好设置",
	AddFiles:       "添加文件",
	AddFolder:      "导入文件夹",
	ClearCompleted: "清理已完成",
	ClearAll:       "清空列表",
	StartAll:       "开始转换",
	CancelAll:      "全部停止",
	DropTitle:       "拖拽文件或文件夹到这里",
	DropHint:        "支持图片、PDF、Office/电子书、数据表、字幕、压缩包及音视频批量离线互转",
	DropTitleMobile: "点击此处选择要转换的文件",
	DropHintMobile:  "支持图片、PDF、DOCX/EPUB、表格、数据、字幕与压缩包本地离线互转",
	DropActive:      "松开鼠标即可加入转换队列",
	DropAppendHint:  "继续拖入文件或点击此处追加到队列",
	EmptyQueueHint:  "队列为空 — 拖入任意文件后将自动识别源格式并匹配上次使用的目标格式",

	FilterAll:     "全部",
	FilterPending: "待处理",
	FilterActive:  "进行中",
	FilterDone:    "已完成",
	FilterFailed:  "失败",

	BatchTargetLabel: "统一目标格式",
	ApplyToAll:       "应用到同类型",
	MobileOptionsBtn: "转换参数",
	OutputDirLabel:   "输出位置",
	SameAsSource:     "原文件同目录",
	CustomDir:        "指定目录",
	ChooseDir:        "选择目录",
	OpenDir:          "打开目录",

	MergeModeLabel:     "合并模式",
	MergeImagesToPDF:   "多图合并为 PDF（按列表顺序分页）",
	MergePDFs:          "多个 PDF 合并为单个 PDF",
	MergeFilesToZip:    "多文件打包为 ZIP 归档",
	MergeImgTitle:      "多图合并为 PDF",
	MergeImgDesc:       "按列表顺序将多图合成为分页 PDF",
	MergePDFTitle:      "多个 PDF 合并",
	MergePDFDesc:       "无损拼接多份 PDF 并保留矢量内容",
	MergeZipTitle:      "打包为 ZIP 归档",
	MergeZipDesc:       "将任意文件离线打包为标准 ZIP",
	MergeShortImgPDF:   "多图 → PDF",
	MergeShortPDF:      "合并 PDF",
	MergeShortZip:      "打包 ZIP",
	MergeOutputName:    "输出文件名",
	StartMerge:         "执行合并并保存",
	MoveUp:             "上移",
	MoveDown:           "下移",
	MergeEmptyHint:     "添加多张图片、多个 PDF 或任意文件，可自由调整顺序后一键合并输出",
	MergeSuccessPrefix: "合并完成：",

	InspectorTitle:     "转换参数",
	ImageQuality:       "图片压缩质量 (1-100)",
	ImageMaxWidth:      "最大宽度限制 (px, 0=保持原图)",
	ImageMaxWidthHint:  "等比缩放，不放大原图",
	VideoCodec:         "视频编码器",
	AudioBitrate:       "音频码率",
	PDFOperation:       "PDF 处理模式",
	PDFOpConvert:       "常规格式转换",
	PDFOpSplit:         "按页拆分 (打包 ZIP)",
	PDFOpEncrypt:       "AES-256 密码加密",
	PDFOpDecrypt:       "密码解密输出",
	PDFOpExtractImages: "提取内嵌原图 (ZIP)",
	PDFSplitSpan:       "每几页拆分为一份",
	PDFPassword:        "PDF 密码",
	PDFPasswordHint:    "加密/解密时必填，不在日志中记录",
	TextEncoding:       "源文本编码",
	OCRHint:            "检测到 Tesseract 时支持图片与扫描件 OCR 文字提取",

	StagePending:    "待开始",
	StageQueued:     "排队中",
	StagePreparing:  "准备中",
	StageConverting: "转换中",
	StageValidating: "校验产物",
	StageDone:       "已完成",
	StageFailed:     "失败",
	StageCanceled:   "已取消",

	ActionConvert:  "转换",
	ActionRetry:    "重试",
	ActionCancel:   "取消",
	ActionOpen:     "打开",
	ActionReveal:   "定位",
	ActionSaveCopy: "导出保存",
	SavedToast:     "文件已导出保存",
	ActionRemove:   "移除",
	ActionCopyPath: "复制文件路径",
	BatchDoneToast: "批量转换已完成",

	EnginesTitle:       "转换引擎状态与能力矩阵",
	EnginesSubtitle:    "Formigo 采用「纯 Go 内置引擎 + 可选外部专业工具」双层架构，开箱即用且支持按需增强",
	RescanEngines:      "重新检测引擎",
	EngineBuiltinTitle: "Formigo Native Core (纯 Go 内置引擎)",
	EngineBuiltinDesc:  "零外部依赖：图片/ICO/SVG/WebP、PDF 合并拆分加解密与文本提取、XLSX/CSV/JSON/YAML/XML/TOML、DOCX/EPUB/Markdown/HTML、字幕与归档",
	EngineAvailable:    "已就绪",
	EngineMissing:      "未检测到",
	FormatsMatrixTitle: "当前平台已激活格式矩阵",

	SettingsTitle:       "偏好设置",
	SettingLanguage:     "界面语言 / Language",
	DescLanguage:        "切换界面显示语言，修改后立即生效",
	LangAutoLabel:       "跟随系统",
	LangZHLabel:         "简体中文",
	LangENLabel:         "English",
	SettingAppearance:   "界面外观",
	DescAppearance:      "跟随系统日夜切换或固定使用浅色 / 深色主题",
	ThemeSystem:         "跟随系统",
	ThemeLight:          "浅色模式",
	ThemeDark:           "深色模式",
	SettingOutputMode:   "默认输出目录",
	DescOutputMode:      "转换结果默认保存在源文件旁或输出到统一目录",
	SettingConflict:     "同名文件策略",
	DescConflict:        "目标路径已存在同名文件时的安全处理方式",
	ConflictRename:      "自动递增编号",
	ConflictOverwrite:   "覆盖同名文件",
	SettingConcurrency:  "最大并行任务数",
	DescConcurrency:     "同时运行的转换工作线程数量（建议匹配 CPU 核心）",
	CustomEnginePaths:   "自定义外部工具路径（留空则自动从系统 PATH 与内置目录搜索）",
	BrowseBinary:        "浏览",
	SaveSettings:        "保存设置",
	SettingsSavedToast:  "设置已保存并立即生效",
	ResetFormatMemory:   "清空各格式默认目标记忆",
	FormatMemoryCleared: "已重置源格式目标偏好记忆",
}

var enCatalog = Catalog{
	AppName:        "Formigo",
	AppSubtitle:    "Cross-Platform Offline File Format Converter",
	NavGroupWork:   "Workspace",
	NavGroupSystem: "System",
	NavConvert:     "Batch Convert",
	NavMerge:       "Merge Studio",
	NavEngines:     "Engines & Matrix",
	NavSettings:    "Settings",
	AddFiles:       "Add Files",
	AddFolder:      "Add Folder",
	ClearCompleted: "Clear Done",
	ClearAll:       "Clear All",
	StartAll:       "Convert All",
	CancelAll:      "Stop All",
	DropTitle:       "Drop files or folders here",
	DropHint:        "Offline conversion for images, PDF, Office/eBooks, spreadsheets, data, subtitles, archives & media",
	DropTitleMobile: "Tap here to pick files to convert",
	DropHintMobile:  "Offline conversion for Images, PDF, DOCX/EPUB, Spreadsheets, Data, Subtitles & Archives",
	DropActive:      "Release to add files to queue",
	DropAppendHint:  "Drop more files here or click to append",
	EmptyQueueHint:  "Queue is empty — drop files to auto-detect formats and restore your preferred target format",

	FilterAll:     "All",
	FilterPending: "Pending",
	FilterActive:  "Active",
	FilterDone:    "Done",
	FilterFailed:  "Failed",

	BatchTargetLabel: "Batch Target",
	ApplyToAll:       "Apply Matching",
	MobileOptionsBtn: "Options",
	OutputDirLabel:   "Output Folder",
	SameAsSource:     "Same as Source",
	CustomDir:        "Custom Folder",
	ChooseDir:        "Browse",
	OpenDir:          "Open",

	MergeModeLabel:     "Merge Mode",
	MergeImagesToPDF:   "Images -> Single PDF (ordered pages)",
	MergePDFs:          "Multiple PDFs -> Merged PDF",
	MergeFilesToZip:    "Multiple Files -> ZIP Archive",
	MergeImgTitle:      "Images to PDF",
	MergeImgDesc:       "Combine multiple images into a multi-page PDF in list order",
	MergePDFTitle:      "Merge PDFs",
	MergePDFDesc:       "Concatenate multiple PDF documents losslessly into one file",
	MergeZipTitle:      "Pack ZIP Archive",
	MergeZipDesc:       "Bundle any combination of files into a standard ZIP archive",
	MergeShortImgPDF:   "Imgs → PDF",
	MergeShortPDF:      "PDFs",
	MergeShortZip:      "ZIP",
	MergeOutputName:    "Output Filename",
	StartMerge:         "Merge & Save",
	MoveUp:             "Up",
	MoveDown:           "Down",
	MergeEmptyHint:     "Add multiple images, PDFs, or files, reorder freely, and merge into one file",
	MergeSuccessPrefix: "Merged to: ",

	InspectorTitle:     "Conversion Options",
	ImageQuality:       "Image Quality (1-100)",
	ImageMaxWidth:      "Max Width Limit (px, 0=Original)",
	ImageMaxWidthHint:  "Preserves aspect ratio without upscaling",
	VideoCodec:         "Video Codec",
	AudioBitrate:       "Audio Bitrate",
	PDFOperation:       "PDF Mode",
	PDFOpConvert:       "Standard Format Convert",
	PDFOpSplit:         "Split Pages (ZIP)",
	PDFOpEncrypt:       "AES-256 Encrypt",
	PDFOpDecrypt:       "Decrypt PDF",
	PDFOpExtractImages: "Extract Embedded Images (ZIP)",
	PDFSplitSpan:       "Pages per Split File",
	PDFPassword:        "PDF Password",
	PDFPasswordHint:    "Required for encrypt/decrypt; never logged",
	TextEncoding:       "Source Text Encoding",
	OCRHint:            "OCR text extraction activates when Tesseract is available",

	StagePending:    "Ready",
	StageQueued:     "Queued",
	StagePreparing:  "Preparing",
	StageConverting: "Converting",
	StageValidating: "Validating",
	StageDone:       "Done",
	StageFailed:     "Failed",
	StageCanceled:   "Canceled",

	ActionConvert:  "Convert",
	ActionRetry:    "Retry",
	ActionCancel:   "Cancel",
	ActionOpen:     "Open",
	ActionReveal:   "Reveal",
	ActionSaveCopy: "Save As",
	SavedToast:     "File saved",
	ActionRemove:   "Remove",
	ActionCopyPath: "Copy File Path",
	BatchDoneToast: "Batch conversion finished",

	EnginesTitle:       "Conversion Engines & Capability Matrix",
	EnginesSubtitle:    "Dual-layer architecture: Pure-Go Native Core out of the box + auto-detected external engines",
	RescanEngines:      "Rescan Engines",
	EngineBuiltinTitle: "Formigo Native Core (Pure Go)",
	EngineBuiltinDesc:  "Zero external dependencies: Image/ICO/SVG/WebP, PDF merge/split/AES/text, XLSX/CSV/JSON/YAML/XML/TOML, DOCX/EPUB/MD/HTML, Subtitles & Archives",
	EngineAvailable:    "Ready",
	EngineMissing:      "Not Found",
	FormatsMatrixTitle: "Active Format Matrix on This Machine",

	SettingsTitle:       "Preferences",
	SettingLanguage:     "Language / 界面语言",
	DescLanguage:        "Choose display language; takes effect immediately",
	LangAutoLabel:       "System Default",
	LangZHLabel:         "简体中文",
	LangENLabel:         "English",
	SettingAppearance:   "Appearance",
	DescAppearance:      "Follow OS appearance or lock Light / Dark mode",
	ThemeSystem:         "System",
	ThemeLight:          "Light",
	ThemeDark:           "Dark",
	SettingOutputMode:   "Default Output Location",
	DescOutputMode:      "Write converted files next to source or into a custom folder",
	SettingConflict:     "Filename Collision",
	DescConflict:        "How to handle existing files with the same output name",
	ConflictRename:      "Auto-number",
	ConflictOverwrite:   "Overwrite Existing",
	SettingConcurrency:  "Max Parallel Workers",
	DescConcurrency:     "Concurrent conversion threads (match CPU cores)",
	CustomEnginePaths:   "Custom External Binary Paths (leave blank for auto-discovery)",
	BrowseBinary:        "Browse",
	SaveSettings:        "Save Preferences",
	SettingsSavedToast:  "Preferences saved",
	ResetFormatMemory:   "Reset Remembered Target Formats",
	FormatMemoryCleared: "Remembered target preferences cleared",
}

// T returns the catalog for a resolved language.
func T(lang Lang) Catalog {
	if lang == LangEN {
		return enCatalog
	}
	return zhCatalog
}

// CategoryLabel returns localized category names.
func CategoryLabel(cat string, lang Lang) string {
	if lang == LangEN {
		switch cat {
		case "image":
			return "Images"
		case "pdf":
			return "PDF"
		case "document":
			return "Documents"
		case "spreadsheet":
			return "Spreadsheets"
		case "presentation":
			return "Presentations"
		case "ebook":
			return "eBooks"
		case "data":
			return "Structured Data"
		case "subtitle":
			return "Subtitles"
		case "audio":
			return "Audio"
		case "video":
			return "Video"
		case "archive":
			return "Archives"
		default:
			return "General"
		}
	}
	switch cat {
	case "image":
		return "图片与图标"
	case "pdf":
		return "PDF 文档"
	case "document":
		return "文字与办公文档"
	case "spreadsheet":
		return "电子表格"
	case "presentation":
		return "演示文稿"
	case "ebook":
		return "电子书"
	case "data":
		return "结构化数据"
	case "subtitle":
		return "字幕文件"
	case "audio":
		return "音频媒体"
	case "video":
		return "视频媒体"
	case "archive":
		return "压缩归档"
	default:
		return "通用文件"
	}
}
