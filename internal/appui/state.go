package appui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"formigo/internal/config"
	"formigo/internal/converter"
	"formigo/internal/i18n"
)

var uiItemSeq atomic.Uint64

// NavPage constants for the left sidebar.
const (
	PageConvert  = "convert"
	PageMerge    = "merge"
	PageEngines  = "engines"
	PageSettings = "settings"
)

// QueueItem represents one file row in the Batch Convert workspace.
type QueueItem struct {
	ID            string
	Path          string
	Name          string
	SourceExt     string
	Category      converter.Category
	SizeBytes     int64
	TargetExt     string
	TargetOptions []string
	Stage         converter.Stage
	Progress      float64
	Detail        string
	OutputPath    string
	OutputSize    int64
	Elapsed       time.Duration
	Warnings      []string
	ErrMsg        string
}

// MergeItem represents an ordered file entry in the Merge Studio workspace.
type MergeItem struct {
	ID        string
	Path      string
	Name      string
	Ext       string
	SizeBytes int64
}

// PlatformBridge decouples OS dialogs, file manager actions, and main-thread dispatch so views remain 100% unit-testable.
type PlatformBridge struct {
	DispatchMain func(fn func())
	PickFiles    func(defaultDir string, done func(paths []string))
	PickFolder   func(defaultDir string, done func(dir string))
	SaveFile     func(defaultPath string, ext string, done func(path string))
	SaveCopy     func(srcPath string, suggestedName string, done func(saved string, err error))
	OpenPath     func(path string)
	RevealPath   func(path string)
	SetProgress  func(value float64, active bool, hasError bool)
	SystemLocale string
	IsMobile     bool
}

// AppState holds the reactive model rendered by AppState.View.
type AppState struct {
	Store    *config.Store
	Pipeline *converter.Pipeline
	Bridge   PlatformBridge

	ActivePage         string
	MobileOptionsOpen  bool
	InspectorCollapsed bool
	QueueFilter        string // "all", "pending", "active", "done", "failed"

	// Batch Convert queue & controls
	Items        []*QueueItem
	BatchTarget  string
	ActiveBatch  *converter.BatchController
	BatchRunning bool

	// Live conversion options bound to inspector controls
	OptQuality      float64
	OptMaxWidth     float64
	OptVideoCodec   string
	OptAudioBitrate string
	OptPDFModeIdx   int
	OptPDFSplitSpan float64
	OptPDFPassword  string
	OptTextEncoding string
	OutputModeIdx   int // 0 = source dir, 1 = custom dir
	CustomOutputDir string

	// Merge Studio state
	MergeModeIdx    int // 0 = images->pdf, 1 = merge pdfs, 2 = files->zip
	MergeItems      []*MergeItem
	MergeOutputName string
	MergeRunning    bool
	MergeLastOutput string
	MergeLastErr    string

	// Engines cache for display
	EngineStatuses []converter.EngineStatus
	FormatMatrix   []converter.MatrixGroup

	// Preferences form fields
	PrefLangIdx        int
	PrefAppearanceIdx  int
	PrefConflictIdx    int
	PrefConcurrency    float64
	PathFFmpeg         string
	PathLibreOffice    string
	PathPandoc         string
	PathPoppler        string
	PathTesseract      string
	PendingToast       string
	PendingToastReveal string
}

// NewAppState initializes application state from Store and Pipeline.
func NewAppState(store *config.Store, pipe *converter.Pipeline, bridge PlatformBridge) *AppState {
	if store == nil {
		store = config.LoadStore(filepath.Join(os.TempDir(), "formigo-fallback-prefs.json"))
	}
	prefs := store.Get()
	if pipe == nil {
		rt := converter.NewRuntime(prefs.EnginePaths)
		pipe, _ = converter.NewPipeline(rt)
	}
	if bridge.DispatchMain == nil {
		bridge.DispatchMain = func(fn func()) { fn() }
	}

	s := &AppState{
		Store:           store,
		Pipeline:        pipe,
		Bridge:          bridge,
		ActivePage:      PageConvert,
		QueueFilter:     "all",
		OptQuality:      float64(prefs.ImageQuality),
		OptMaxWidth:     0,
		OptVideoCodec:   prefs.VideoCodec,
		OptAudioBitrate: prefs.AudioBitrate,
		OptPDFModeIdx:   0,
		OptPDFSplitSpan: 1,
		OptTextEncoding: "auto",
		CustomOutputDir: prefs.CustomOutputDir,
		MergeOutputName: "merged-output",
		PrefConcurrency: float64(prefs.Concurrency),
		PathFFmpeg:      prefs.EnginePaths["ffmpeg"],
		PathLibreOffice: prefs.EnginePaths["libreoffice"],
		PathPandoc:      prefs.EnginePaths["pandoc"],
		PathPoppler:     prefs.EnginePaths["poppler"],
		PathTesseract:   prefs.EnginePaths["tesseract"],
	}
	if prefs.OutputMode == config.OutputCustomDir {
		s.OutputModeIdx = 1
	}
	switch prefs.Language {
	case i18n.LangZH:
		s.PrefLangIdx = 1
	case i18n.LangEN:
		s.PrefLangIdx = 2
	default:
		s.PrefLangIdx = 0
	}
	switch prefs.Appearance {
	case config.AppearanceLight:
		s.PrefAppearanceIdx = 1
	case config.AppearanceDark:
		s.PrefAppearanceIdx = 2
	default:
		s.PrefAppearanceIdx = 0
	}
	if prefs.ConflictPolicy == config.ConflictOverwrite {
		s.PrefConflictIdx = 1
	}

	s.RefreshEngineSnapshot()
	return s
}

// ResolvedLang returns the active UI language.
func (s *AppState) ResolvedLang() i18n.Lang {
	switch s.PrefLangIdx {
	case 1:
		return i18n.LangZH
	case 2:
		return i18n.LangEN
	default:
		return i18n.Resolve(i18n.LangAuto, s.Bridge.SystemLocale)
	}
}

// RefreshEngineSnapshot updates cached engine statuses and active format matrix, refreshing queue targets too.
func (s *AppState) RefreshEngineSnapshot() {
	rt := s.Pipeline.Runtime()
	s.EngineStatuses = rt.Statuses()
	s.FormatMatrix = converter.ActiveMatrix(rt)
	for _, it := range s.Items {
		choices := converter.TargetsForExt(it.SourceExt, rt, true)
		opts := make([]string, len(choices))
		for i, ch := range choices {
			opts[i] = ch.Ext
		}
		it.TargetOptions = opts
		if len(opts) > 0 && !slices.Contains(opts, it.TargetExt) {
			it.TargetExt = converter.DefaultTargetFor(it.SourceExt, s.Store.PreferredTarget(it.SourceExt), rt)
		}
	}
}

// AddPaths expands files and directories and appends supported items into the active workspace (Convert or Merge).
func (s *AppState) AddPaths(paths []string) int {
	expanded := expandInputPaths(paths)
	if len(expanded) == 0 {
		return 0
	}
	if dir := filepath.Dir(expanded[0]); dir != "" {
		_ = s.Store.Update(func(p *config.Preferences) { p.LastOpenDir = dir })
	}
	if s.ActivePage == PageMerge {
		return s.AddMergeFiles(expanded)
	}
	return s.AddQueueFiles(expanded)
}

// AddQueueFiles appends files to the Batch Convert queue, deduplicating active paths.
func (s *AppState) AddQueueFiles(paths []string) int {
	rt := s.Pipeline.Runtime()
	existing := make(map[string]bool, len(s.Items))
	for _, it := range s.Items {
		if it.Stage != converter.StageDone && it.Stage != converter.StageFailed && it.Stage != converter.StageCanceled {
			existing[it.Path] = true
		}
	}

	added := 0
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil || existing[abs] {
			continue
		}
		info, err := os.Stat(abs)
		if err != nil || info.IsDir() || info.Size() == 0 {
			continue
		}
		srcExt := converter.NormalizeExt(abs)
		if srcExt == "" {
			continue
		}
		choices := converter.TargetsForExt(srcExt, rt, true)
		if len(choices) == 0 {
			continue
		}
		opts := make([]string, len(choices))
		for i, ch := range choices {
			opts[i] = ch.Ext
		}
		target := converter.DefaultTargetFor(srcExt, s.Store.PreferredTarget(srcExt), rt)
		item := &QueueItem{
			ID:            fmt.Sprintf("q-%d", uiItemSeq.Add(1)),
			Path:          abs,
			Name:          filepath.Base(abs),
			SourceExt:     srcExt,
			Category:      converter.CategoryForExt(srcExt),
			SizeBytes:     info.Size(),
			TargetExt:     target,
			TargetOptions: opts,
			Stage:         converter.StagePending,
		}
		s.Items = append(s.Items, item)
		existing[abs] = true
		added++
	}

	if added > 0 && s.BatchTarget == "" {
		all := s.AvailableBatchTargets()
		if len(all) > 0 {
			s.BatchTarget = all[0]
		}
	}
	return added
}

// AddMergeFiles appends files to the Merge Studio list, filtering appropriately by MergeModeIdx.
func (s *AppState) AddMergeFiles(paths []string) int {
	added := 0
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		info, err := os.Stat(abs)
		if err != nil || info.IsDir() || info.Size() == 0 {
			continue
		}
		ext := converter.NormalizeExt(abs)
		if s.MergeModeIdx == 0 && converter.CategoryForExt(ext) != converter.CategoryImage {
			continue
		}
		if s.MergeModeIdx == 1 && ext != "pdf" {
			continue
		}
		s.MergeItems = append(s.MergeItems, &MergeItem{
			ID:        fmt.Sprintf("m-%d", uiItemSeq.Add(1)),
			Path:      abs,
			Name:      filepath.Base(abs),
			Ext:       ext,
			SizeBytes: info.Size(),
		})
		added++
	}
	return added
}

// AvailableBatchTargets collects the union of target formats across all queue items.
func (s *AppState) AvailableBatchTargets() []string {
	seen := make(map[string]bool)
	var out []string
	for _, it := range s.Items {
		for _, opt := range it.TargetOptions {
			if !seen[opt] {
				seen[opt] = true
				out = append(out, opt)
			}
		}
	}
	return out
}

// ApplyBatchTarget sets TargetExt on every compatible pending/failed item in the queue.
func (s *AppState) ApplyBatchTarget(target string) int {
	target = converter.NormalizeExt(target)
	if target == "" {
		return 0
	}
	changed := 0
	for _, it := range s.Items {
		if it.Stage == converter.StageConverting || it.Stage == converter.StageQueued {
			continue
		}
		if slices.Contains(it.TargetOptions, target) {
			it.TargetExt = target
			if it.Stage == converter.StageDone || it.Stage == converter.StageFailed || it.Stage == converter.StageCanceled {
				it.Stage = converter.StagePending
				it.Progress = 0
				it.ErrMsg = ""
			}
			s.Store.RememberTarget(it.SourceExt, target)
			changed++
		}
	}
	return changed
}

// BuildOptions constructs converter.Options from current UI inspector state.
func (s *AppState) BuildOptions() converter.Options {
	prefs := s.Store.Get()
	pdfOps := []converter.PDFOperation{
		converter.PDFOpConvert,
		converter.PDFOpSplit,
		converter.PDFOpEncrypt,
		converter.PDFOpDecrypt,
		converter.PDFOpExtractImages,
	}
	pdfOp := converter.PDFOpConvert
	if s.OptPDFModeIdx >= 0 && s.OptPDFModeIdx < len(pdfOps) {
		pdfOp = pdfOps[s.OptPDFModeIdx]
	}
	return converter.Options{
		ImageQuality:  int(s.OptQuality),
		ImageMaxWidth: int(s.OptMaxWidth),
		VideoCodec:    s.OptVideoCodec,
		AudioBitrate:  s.OptAudioBitrate,
		PDFOperation:  pdfOp,
		PDFSplitSpan:  max(1, int(s.OptPDFSplitSpan)),
		Password:      s.OptPDFPassword,
		TextEncoding:  s.OptTextEncoding,
		Overwrite:     prefs.ConflictPolicy == config.ConflictOverwrite,
	}
}

func (s *AppState) effectiveOutputDir() string {
	if s.Bridge.IsMobile {
		return os.TempDir()
	}
	if s.OutputModeIdx == 1 && strings.TrimSpace(s.CustomOutputDir) != "" {
		return strings.TrimSpace(s.CustomOutputDir)
	}
	return ""
}

// StartAll launches batch conversion for all non-completed queue items.
func (s *AppState) StartAll() {
	if s.BatchRunning {
		return
	}
	var jobs []converter.BatchJob
	opts := s.BuildOptions()
	outDir := s.effectiveOutputDir()

	for _, it := range s.Items {
		if it.Stage == converter.StageDone {
			continue
		}
		it.Stage = converter.StageQueued
		it.Progress = 0
		it.ErrMsg = ""
		it.Detail = ""
		jobs = append(jobs, converter.BatchJob{
			ID: it.ID,
			Request: converter.Request{
				InputPaths: []string{it.Path},
				TargetExt:  it.TargetExt,
				OutputDir:  outDir,
				Options:    opts,
			},
		})
	}
	if len(jobs) == 0 {
		return
	}
	s.launchJobs(jobs)
}

// StartSingle runs conversion for a single item by ID.
func (s *AppState) StartSingle(id string) {
	if s.BatchRunning {
		return
	}
	var target *QueueItem
	for _, it := range s.Items {
		if it.ID == id {
			target = it
			break
		}
	}
	if target == nil {
		return
	}
	target.Stage = converter.StageQueued
	target.Progress = 0
	target.ErrMsg = ""
	target.Detail = ""
	job := converter.BatchJob{
		ID: target.ID,
		Request: converter.Request{
			InputPaths: []string{target.Path},
			TargetExt:  target.TargetExt,
			OutputDir:  s.effectiveOutputDir(),
			Options:    s.BuildOptions(),
		},
	}
	s.launchJobs([]converter.BatchJob{job})
}

func (s *AppState) launchJobs(jobs []converter.BatchJob) {
	s.BatchRunning = true
	if s.Bridge.SetProgress != nil {
		s.Bridge.SetProgress(0.01, true, false)
	}
	workers := max(1, int(s.PrefConcurrency))
	ctrl := s.Pipeline.StartBatch(context.Background(), jobs, workers, func(u converter.BatchUpdate) {
		s.Bridge.DispatchMain(func() {
			s.applyJobUpdate(u)
		})
	})
	s.ActiveBatch = ctrl
	go func() {
		ctrl.Wait()
		s.Bridge.DispatchMain(func() {
			if s.ActiveBatch == ctrl {
				s.BatchRunning = false
				s.ActiveBatch = nil
				if s.Bridge.SetProgress != nil {
					s.Bridge.SetProgress(0, false, false)
				}
				var lastOut string
				doneCnt := 0
				for _, it := range s.Items {
					if it.Stage == converter.StageDone && it.OutputPath != "" {
						doneCnt++
						lastOut = it.OutputPath
					}
				}
				if len(jobs) > 1 && doneCnt > 0 && !s.Bridge.IsMobile {
					s.PendingToast = fmt.Sprintf("%s (%d)", i18n.T(s.ResolvedLang()).BatchDoneToast, doneCnt)
					s.PendingToastReveal = lastOut
				}
			}
		})
	}()
}

func (s *AppState) applyJobUpdate(u converter.BatchUpdate) {
	for _, it := range s.Items {
		if it.ID != u.ID {
			continue
		}
		it.Stage = u.Stage
		it.Progress = u.Progress
		if u.Detail != "" {
			it.Detail = u.Detail
		}
		if u.Result != nil {
			it.OutputPath = u.Result.OutputPath
			it.OutputSize = u.Result.OutputSize
			it.Elapsed = u.Result.Elapsed
			it.Warnings = u.Result.Warnings
			s.Store.RememberTarget(it.SourceExt, it.TargetExt)
		}
		if u.Err != nil {
			msg := u.Err.Error()
			if s.OptPDFPassword != "" {
				msg = strings.ReplaceAll(msg, s.OptPDFPassword, "[redacted]")
			}
			it.ErrMsg = msg
		}
		break
	}
	if s.BatchRunning && s.Bridge.SetProgress != nil {
		_, _, _, failed, prog := s.QueueCounts()
		s.Bridge.SetProgress(prog, true, failed > 0)
	}
}

// QueueCounts summarizes item stages and overall 0..1 progress across the queue.
func (s *AppState) QueueCounts() (pending, active, done, failed int, overall float64) {
	if len(s.Items) == 0 {
		return 0, 0, 0, 0, 0
	}
	var sum float64
	for _, it := range s.Items {
		switch it.Stage {
		case converter.StageDone:
			done++
			sum += 1.0
		case converter.StageFailed, converter.StageCanceled:
			failed++
			sum += 1.0
		case converter.StageConverting, converter.StageQueued, converter.StagePreparing, converter.StageValidating:
			active++
			sum += max(0.05, min(0.99, it.Progress))
		default:
			pending++
		}
	}
	return pending, active, done, failed, sum / float64(len(s.Items))
}

// FilteredItems returns queue items matching QueueFilter (falling back to all if filter has 0 matches).
func (s *AppState) FilteredItems() []*QueueItem {
	f := s.QueueFilter
	if f == "" || f == "all" {
		return s.Items
	}
	var out []*QueueItem
	for _, it := range s.Items {
		match := false
		switch f {
		case "pending":
			match = it.Stage == converter.StagePending
		case "active":
			match = it.Stage == converter.StageConverting || it.Stage == converter.StageQueued || it.Stage == converter.StagePreparing || it.Stage == converter.StageValidating
		case "done":
			match = it.Stage == converter.StageDone
		case "failed":
			match = it.Stage == converter.StageFailed || it.Stage == converter.StageCanceled
		}
		if match {
			out = append(out, it)
		}
	}
	if len(out) == 0 {
		return s.Items
	}
	return out
}

// CancelAll aborts the active batch.
func (s *AppState) CancelAll() {
	if s.ActiveBatch != nil {
		s.ActiveBatch.CancelAll()
	}
}

// RemoveItem deletes one item from the queue (canceling it first if active).
func (s *AppState) RemoveItem(id string) {
	if s.ActiveBatch != nil {
		s.ActiveBatch.CancelJob(id)
	}
	s.Items = slices.DeleteFunc(s.Items, func(it *QueueItem) bool {
		return it.ID == id
	})
}

// ClearCompleted removes all finished items from the queue.
func (s *AppState) ClearCompleted() {
	s.Items = slices.DeleteFunc(s.Items, func(it *QueueItem) bool {
		return it.Stage == converter.StageDone
	})
}

// ClearAll cancels any running work and empties the queue.
func (s *AppState) ClearAll() {
	s.CancelAll()
	s.Items = nil
}

// MoveMergeItem reorders MergeItems by delta (-1 up, +1 down).
func (s *AppState) MoveMergeItem(idx, delta int) {
	dst := idx + delta
	if idx < 0 || idx >= len(s.MergeItems) || dst < 0 || dst >= len(s.MergeItems) {
		return
	}
	s.MergeItems[idx], s.MergeItems[dst] = s.MergeItems[dst], s.MergeItems[idx]
}

// RunMergeTo executes the Merge Studio operation to explicitOutputPath (or beside the first input if empty).
func (s *AppState) RunMergeTo(explicitOutputPath string) {
	if s.MergeRunning || len(s.MergeItems) == 0 {
		return
	}
	var mode converter.MergeMode
	ext := "pdf"
	switch s.MergeModeIdx {
	case 0:
		mode = converter.MergeImagesToPDF
	case 1:
		mode = converter.MergePDFs
	default:
		mode = converter.MergeFilesToZip
		ext = "zip"
	}

	inputs := make([]string, len(s.MergeItems))
	for i, it := range s.MergeItems {
		inputs[i] = it.Path
	}

	outPath := explicitOutputPath
	if outPath == "" {
		dir := s.effectiveOutputDir()
		if dir == "" {
			dir = filepath.Dir(inputs[0])
		}
		stem := strings.TrimSpace(s.MergeOutputName)
		if stem == "" {
			stem = "merged-output"
		}
		stem = strings.TrimSuffix(stem, "."+ext)
		outPath = filepath.Join(dir, stem+"."+ext)
	}

	opts := s.BuildOptions()
	s.MergeRunning = true
	s.MergeLastErr = ""
	s.MergeLastOutput = ""

	go func() {
		res, err := s.Pipeline.Convert(context.Background(), converter.Request{
			InputPaths: inputs,
			OutputPath: outPath,
			MergeMode:  mode,
			Options:    opts,
		}, nil)
		s.Bridge.DispatchMain(func() {
			s.MergeRunning = false
			if err != nil {
				s.MergeLastErr = err.Error()
			} else {
				s.MergeLastOutput = res.OutputPath
				if s.Bridge.IsMobile && s.Bridge.SaveCopy != nil {
					s.Bridge.SaveCopy(res.OutputPath, filepath.Base(res.OutputPath), func(saved string, saveErr error) {
						if saveErr == nil && saved != "" {
							s.PendingToast = i18n.T(s.ResolvedLang()).SavedToast
						}
					})
				}
			}
		})
	}()
}

// SavePreferences persists settings from the Preferences page and refreshes engines.
func (s *AppState) SavePreferences() {
	_ = s.Store.Update(func(p *config.Preferences) {
		switch s.PrefLangIdx {
		case 1:
			p.Language = i18n.LangZH
		case 2:
			p.Language = i18n.LangEN
		default:
			p.Language = i18n.LangAuto
		}
		switch s.PrefAppearanceIdx {
		case 1:
			p.Appearance = config.AppearanceLight
		case 2:
			p.Appearance = config.AppearanceDark
		default:
			p.Appearance = config.AppearanceSystem
		}
		if s.OutputModeIdx == 1 {
			p.OutputMode = config.OutputCustomDir
		} else {
			p.OutputMode = config.OutputSameAsSource
		}
		p.CustomOutputDir = strings.TrimSpace(s.CustomOutputDir)
		if s.PrefConflictIdx == 1 {
			p.ConflictPolicy = config.ConflictOverwrite
		} else {
			p.ConflictPolicy = config.ConflictRename
		}
		p.Concurrency = max(1, int(s.PrefConcurrency))
		p.ImageQuality = max(1, min(100, int(s.OptQuality)))
		p.VideoCodec = s.OptVideoCodec
		p.AudioBitrate = s.OptAudioBitrate
		p.EnginePaths["ffmpeg"] = strings.TrimSpace(s.PathFFmpeg)
		p.EnginePaths["libreoffice"] = strings.TrimSpace(s.PathLibreOffice)
		p.EnginePaths["pandoc"] = strings.TrimSpace(s.PathPandoc)
		p.EnginePaths["poppler"] = strings.TrimSpace(s.PathPoppler)
		p.EnginePaths["tesseract"] = strings.TrimSpace(s.PathTesseract)
	})
	s.Pipeline.Runtime().SetCustomPaths(s.Store.Get().EnginePaths)
	s.RefreshEngineSnapshot()
}

func expandInputPaths(paths []string) []string {
	var out []string
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if !info.IsDir() {
			out = append(out, p)
			continue
		}
		_ = filepath.WalkDir(p, func(cur string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			name := d.Name()
			if d.IsDir() {
				if cur != p && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "__MACOSX") {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasPrefix(name, ".") {
				out = append(out, cur)
			}
			return nil
		})
	}
	return out
}

// HumanBytes formats byte counts cleanly (e.g. "2.4 MB").
func HumanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
