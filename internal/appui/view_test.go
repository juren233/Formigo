package appui

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"formigo/internal/config"
	"formigo/internal/converter"

	"github.com/egoist/mygo/ui"
)

func TestAppViewNavigationAndConversionFlow(t *testing.T) {
	dir := t.TempDir()
	store := config.LoadStore(filepath.Join(dir, "prefs.json"))
	rt := converter.NewRuntime(nil)
	pipe, err := converter.NewPipeline(rt)
	if err != nil {
		t.Fatal(err)
	}
	defer pipe.Close()

	state := NewAppState(store, pipe, PlatformBridge{
		SystemLocale: "zh_CN",
	})

	tt := ui.NewTester(state.View, 1120, 740)

	// 1. Verify initial Chinese UI renders cleanly without duplicate key panics
	if !tt.HasText("Fomigo") || !tt.HasText("批量转换") {
		t.Fatalf("expected Fomigo brand and Chinese sidebar, texts: %v", tt.Texts())
	}

	// 2. Add two real files to queue and verify queue cards appear
	svgFile := filepath.Join(dir, "sample.svg")
	csvFile := filepath.Join(dir, "data.csv")
	_ = os.WriteFile(svgFile, []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><circle cx="32" cy="32" r="28" fill="#6366F1"/></svg>`), 0o644)
	_ = os.WriteFile(csvFile, []byte("name,score\nAlice,99\nBob,92\n"), 0o644)

	added := state.AddQueueFiles([]string{svgFile, csvFile})
	if added != 2 {
		t.Fatalf("expected 2 added files, got %d", added)
	}
	tt.Frame()
	if !tt.HasText("sample.svg") || !tt.HasText("data.csv") {
		t.Fatalf("expected queued filenames in view, got: %v", tt.Texts())
	}

	// 3. Click "开始转换" and wait briefly for worker completion
	if err := tt.Click("开始转换"); err != nil {
		t.Fatalf("clicking 开始转换 failed: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for state.BatchRunning && time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
	}
	tt.Frame()

	for _, it := range state.Items {
		if it.Stage != converter.StageDone {
			t.Fatalf("item %s finished in stage %s (err=%s)", it.Name, it.Stage, it.ErrMsg)
		}
		if _, statErr := os.Stat(it.OutputPath); statErr != nil {
			t.Fatalf("missing converted output %s: %v", it.OutputPath, statErr)
		}
	}
	if !tt.HasText("已完成") {
		t.Fatalf("expected 已完成 badge in view after conversion")
	}

	// 4. Navigate across Merge Studio, Engines & Matrix, and Settings
	if err := tt.Click("合并工坊"); err != nil {
		t.Fatalf("clicking 合并工坊 failed: %v", err)
	}
	if state.ActivePage != PageMerge {
		t.Fatalf("expected PageMerge, got %s", state.ActivePage)
	}

	if err := tt.Click("引擎与格式"); err != nil {
		t.Fatalf("clicking 引擎与格式 failed: %v", err)
	}
	if !tt.HasText("Fomigo Native Core (纯 Go 内置引擎)") {
		t.Fatalf("expected built-in engine card on Engines page")
	}

	if err := tt.Click("偏好设置"); err != nil {
		t.Fatalf("clicking 偏好设置 failed: %v", err)
	}

	// Switch language to English and Dark Theme via UI clicks
	if err := tt.Click("English"); err != nil {
		t.Fatalf("clicking English segment failed: %v", err)
	}
	if !tt.HasText("Preferences") || !tt.HasText("Batch Convert") {
		t.Fatalf("expected live English UI translation, got: %v", tt.Texts())
	}
	if err := tt.Click("Dark"); err != nil {
		t.Fatalf("clicking Dark theme segment failed: %v", err)
	}

	// 5. Verify GPU/software frame rasterizer produces valid non-empty RGBA buffer
	img := ui.Render(state.View, 1120, 740, 1.0)
	if img == nil || img.Bounds().Dx() != 1120 || img.Bounds().Dy() != 740 {
		t.Fatalf("unexpected rendered image bounds")
	}
}

func TestMobileCompactViewAndTouchFlow(t *testing.T) {
	dir := t.TempDir()
	store := config.LoadStore(filepath.Join(dir, "mobile-prefs.json"))
	rt := converter.NewRuntime(nil)
	pipe, err := converter.NewPipeline(rt)
	if err != nil {
		t.Fatal(err)
	}
	defer pipe.Close()

	var exportedFile string
	state := NewAppState(store, pipe, PlatformBridge{
		SystemLocale: "zh_CN",
		IsMobile:     true,
		SaveCopy: func(srcPath string, suggestedName string, done func(saved string, err error)) {
			exportedFile = suggestedName
			if done != nil {
				done("content://downloads/"+suggestedName, nil)
			}
		},
	})

	// Phone portrait dimensions (393x852 DIPs)
	tt := ui.NewTester(state.View, 393, 852)

	if !tt.HasText("点击此处选择要转换的文件") {
		t.Fatalf("expected mobile tap-to-pick prompt, got: %v", tt.Texts())
	}

	// Open & close mobile conversion options bottom sheet
	if err := tt.Click("转换参数"); err != nil {
		t.Fatalf("clicking 转换参数 failed: %v", err)
	}
	tt.Frame()
	if !state.MobileOptionsOpen {
		t.Fatal("expected MobileOptionsOpen=true")
	}
	state.MobileOptionsOpen = false
	tt.Frame()

	// Add an SVG file, convert, and trigger mobile SaveCopy
	svgFile := filepath.Join(dir, "icon.svg")
	_ = os.WriteFile(svgFile, []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect width="64" height="64" rx="16" fill="#4F46E5"/></svg>`), 0o644)
	state.AddQueueFiles([]string{svgFile})
	tt.Frame()

	if err := tt.Click("开始转换"); err != nil {
		t.Fatalf("clicking 开始转换 on mobile failed: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for state.BatchRunning && time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
	}
	tt.Frame()

	if err := tt.Click("导出保存"); err != nil {
		t.Fatalf("clicking 导出保存 failed: %v", err)
	}
	if exportedFile == "" {
		t.Fatal("expected SaveCopy callback to receive exported filename")
	}

	// Test switching across all 4 bottom tabs on mobile without duplicate keys
	for _, tab := range []string{"合并工坊", "引擎与格式", "偏好设置", "批量转换"} {
		if err := tt.Click(tab); err != nil {
			t.Fatalf("clicking mobile bottom tab %q failed: %v", tab, err)
		}
		tt.Frame()
	}
}

func TestRenderSnapshots(t *testing.T) {
	outDir := os.Getenv("FOMIGO_SNAP_DIR")
	if outDir == "" {
		t.Skip("set FOMIGO_SNAP_DIR to export UI snapshots")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	store := config.LoadStore(filepath.Join(dir, "prefs.json"))
	rt := converter.NewRuntime(nil)
	pipe, err := converter.NewPipeline(rt)
	if err != nil {
		t.Fatal(err)
	}
	defer pipe.Close()

	state := NewAppState(store, pipe, PlatformBridge{
		SystemLocale: "zh_CN",
		PickFiles:    func(string, func([]string)) {},
		PickFolder:   func(string, func(string)) {},
		OpenPath:     func(string) {},
		RevealPath:   func(string) {},
	})
	save := func(name string, w, h int) {
		img := ui.Render(state.View, w, h, 2.0)
		f, err := os.Create(filepath.Join(outDir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
	}

	state.PrefAppearanceIdx = 1
	state.ActivePage = PageConvert
	save("01_convert_empty_light", 1180, 760)

	state.PrefAppearanceIdx = 2
	save("02_convert_empty_dark", 1180, 760)

	state.Items = []*QueueItem{
		{
			ID: "1", Path: "/Users/demo/hero-banner.svg", Name: "hero-banner.svg",
			SourceExt: "svg", Category: converter.CategoryImage, SizeBytes: 184320,
			TargetExt: "webp", TargetOptions: []string{"webp", "png", "jpg", "ico"},
			Stage: converter.StageDone, Progress: 1, OutputPath: "/Users/demo/hero-banner.webp",
			OutputSize: 42100, Elapsed: 84 * time.Millisecond,
		},
		{
			ID: "2", Path: "/Users/demo/quarterly-report-2026.xlsx", Name: "quarterly-report-2026.xlsx",
			SourceExt: "xlsx", Category: converter.CategorySpreadsheet, SizeBytes: 2480120,
			TargetExt: "json", TargetOptions: []string{"csv", "json", "md", "html"},
			Stage: converter.StageConverting, Progress: 0.64, Detail: "正在解析工作表 (Sheet 2/3)…",
		},
		{
			ID: "3", Path: "/Users/demo/product-manual-v3.pdf", Name: "product-manual-v3.pdf",
			SourceExt: "pdf", Category: converter.CategoryPDF, SizeBytes: 8912000,
			TargetExt: "png", TargetOptions: []string{"png", "txt", "pdf"},
			Stage: converter.StagePending,
		},
		{
			ID: "4", Path: "/Users/demo/corrupted-clip.mp4", Name: "corrupted-clip.mp4",
			SourceExt: "mp4", Category: converter.CategoryVideo, SizeBytes: 45120000,
			TargetExt: "gif", TargetOptions: []string{"mp3", "gif", "webm", "wav"},
			Stage: converter.StageFailed, ErrMsg: "未检测到 FFmpeg 引擎，请在偏好设置中配置或安装 ffmpeg",
		},
	}
	state.PrefAppearanceIdx = 1
	save("03_convert_queue_light", 1180, 760)
	state.PrefAppearanceIdx = 2
	save("04_convert_queue_dark", 1180, 760)

	state.PrefAppearanceIdx = 1
	state.ActivePage = PageMerge
	save("05_merge_light", 1180, 760)
	state.ActivePage = PageEngines
	save("06_engines_light", 1180, 760)
	state.ActivePage = PageSettings
	save("07_settings_light", 1180, 760)

	state.Bridge.IsMobile = true
	state.ActivePage = PageConvert
	save("08_mobile_light", 393, 852)
}

