package converter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// ErrCanceled indicates the user or caller canceled a conversion job.
var ErrCanceled = errors.New("conversion canceled")

// Runtime manages external conversion engine discovery and bounded subprocess execution.
type Runtime struct {
	mu          sync.RWMutex
	customPaths map[string]string
	statuses    map[EngineID]EngineStatus
	ffprobePath string
	tessLangs   map[string]bool
}

// NewRuntime creates a Runtime and probes available engines.
func NewRuntime(customPaths map[string]string) *Runtime {
	rt := &Runtime{
		customPaths: make(map[string]string),
		statuses:    make(map[EngineID]EngineStatus),
		tessLangs:   make(map[string]bool),
	}
	for k, v := range customPaths {
		rt.customPaths[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
	}
	rt.Refresh()
	return rt
}

// SetCustomPaths updates custom tool overrides and refreshes engine availability.
func (rt *Runtime) SetCustomPaths(customPaths map[string]string) {
	rt.mu.Lock()
	rt.customPaths = make(map[string]string)
	for k, v := range customPaths {
		rt.customPaths[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
	}
	rt.mu.Unlock()
	rt.Refresh()
}

// Refresh probes all engines on the host machine.
func (rt *Runtime) Refresh() {
	statuses := make(map[EngineID]EngineStatus)
	statuses[EngineBuiltin] = EngineStatus{
		ID:          EngineBuiltin,
		Name:        "Formigo Native Core",
		Available:   true,
		Builtin:     true,
		Version:     "Pure Go",
		Description: "Image/ICO/SVG/WebP, PDF Merge/Split/AES/Text, XLSX/CSV/JSON/YAML/TOML/XML, DOCX/EPUB/MD/HTML, Subtitles, Archives",
	}

	rt.mu.RLock()
	custom := make(map[string]string, len(rt.customPaths))
	for k, v := range rt.customPaths {
		custom[k] = v
	}
	rt.mu.RUnlock()

	ffmpeg := discoverBinary("ffmpeg", custom["ffmpeg"], formigoEnv("FORMIGO_FFMPEG_PATH", "FOMIGO_FFMPEG_PATH"), extraFFmpegCandidates())
	statuses[EngineFFmpeg] = probeTool(EngineFFmpeg, "FFmpeg", ffmpeg, []string{"-version"},
		"Audio/Video transcoding (MP3/FLAC/WAV/AAC/OPUS/MP4/MKV/MOV/WEBM/GIF) & HEIC/AVIF/TGA decoding")

	ffprobe := ""
	if ffmpeg != "" {
		sibling := filepath.Join(filepath.Dir(ffmpeg), exeName("ffprobe"))
		if fileExecutable(sibling) {
			ffprobe = sibling
		}
	}
	if ffprobe == "" {
		ffprobe = discoverBinary("ffprobe", custom["ffprobe"], formigoEnv("FORMIGO_FFPROBE_PATH", "FOMIGO_FFPROBE_PATH"), extraFFprobeCandidates())
	}

	soffice := discoverBinary("soffice", custom["libreoffice"], formigoEnv("FORMIGO_LIBREOFFICE_PATH", "FOMIGO_LIBREOFFICE_PATH"), extraLibreOfficeCandidates())
	statuses[EngineLibreOffice] = probeTool(EngineLibreOffice, "LibreOffice", soffice, []string{"--version"},
		"High-fidelity Office/WPS (DOC/DOCX/XLS/XLSX/PPT/PPTX/ODT/RTF) <-> PDF & Office formats")

	pandoc := discoverBinary("pandoc", custom["pandoc"], formigoEnv("FORMIGO_PANDOC_PATH", "FOMIGO_PANDOC_PATH"), extraPandocCandidates())
	statuses[EnginePandoc] = probeTool(EnginePandoc, "Pandoc", pandoc, []string{"--version"},
		"Rich document conversion (Markdown/DOCX/EPUB/RST/LaTeX/HTML)")

	pdftoppm := discoverBinary("pdftoppm", custom["poppler"], formigoEnv("FORMIGO_PDFTOPPM_PATH", "FOMIGO_PDFTOPPM_PATH"), extraPopplerCandidates())
	statuses[EnginePoppler] = probeTool(EnginePoppler, "Poppler (pdftoppm)", pdftoppm, []string{"-v"},
		"Multi-page PDF rasterization to high-DPI PNG/JPG images")

	tesseract := discoverBinary("tesseract", custom["tesseract"], formigoEnv("FORMIGO_TESSERACT_PATH", "FOMIGO_TESSERACT_PATH"), extraTesseractCandidates())
	tessStatus := probeTool(EngineTesseract, "Tesseract OCR", tesseract, []string{"--version"},
		"Offline optical character recognition (Image / Scanned PDF -> TXT/MD)")
	statuses[EngineTesseract] = tessStatus

	tessLangs := make(map[string]bool)
	if tessStatus.Available {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		out, _, err := runSubprocess(ctx, tesseract, []string{"--list-langs"}, nil)
		cancel()
		if err == nil {
			for _, line := range strings.Split(out, "\n") {
				l := strings.TrimSpace(line)
				if l != "" && !strings.HasPrefix(strings.ToLower(l), "list of") {
					tessLangs[l] = true
				}
			}
		}
	}

	if runtime.GOOS == "darwin" {
		sips := discoverBinary("sips", "", "", []string{"/usr/bin/sips"})
		statuses[EngineSips] = EngineStatus{
			ID:          EngineSips,
			Name:        "macOS ImageIO (sips)",
			Available:   sips != "",
			Path:        sips,
			Version:     "System Built-in",
			Description: "Native macOS HEIC/HEIF/TIFF/PDF first-page rasterization",
		}
	}

	rt.mu.Lock()
	rt.statuses = statuses
	rt.ffprobePath = ffprobe
	rt.tessLangs = tessLangs
	rt.mu.Unlock()
}

// Available reports whether the specified engine is ready.
func (rt *Runtime) Available(id EngineID) bool {
	if rt == nil {
		return id == EngineBuiltin
	}
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	st, ok := rt.statuses[id]
	return ok && st.Available
}

// Path returns the resolved binary path for an external engine.
func (rt *Runtime) Path(id EngineID) string {
	if rt == nil {
		return ""
	}
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return rt.statuses[id].Path
}

// FFprobePath returns the resolved ffprobe path if found.
func (rt *Runtime) FFprobePath() string {
	if rt == nil {
		return ""
	}
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	return rt.ffprobePath
}

// BestOCRLang picks a valid Tesseract language spec based on installed traineddata.
func (rt *Runtime) BestOCRLang(preferred string) string {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	if len(rt.tessLangs) == 0 {
		if preferred != "" {
			return preferred
		}
		return "eng"
	}
	if preferred != "" {
		parts := strings.Split(preferred, "+")
		var valid []string
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if rt.tessLangs[p] {
				valid = append(valid, p)
			}
		}
		if len(valid) > 0 {
			return strings.Join(valid, "+")
		}
	}
	if rt.tessLangs["chi_sim"] && rt.tessLangs["eng"] {
		return "chi_sim+eng"
	}
	if rt.tessLangs["eng"] {
		return "eng"
	}
	for k := range rt.tessLangs {
		if k != "osd" {
			return k
		}
	}
	return "eng"
}

// Statuses returns ordered engine status records for UI and CLI diagnostics.
func (rt *Runtime) Statuses() []EngineStatus {
	order := []EngineID{
		EngineBuiltin,
		EngineFFmpeg,
		EngineLibreOffice,
		EnginePandoc,
		EnginePoppler,
		EngineTesseract,
	}
	if runtime.GOOS == "darwin" {
		order = append(order, EngineSips)
	}
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	out := make([]EngineStatus, 0, len(order))
	for _, id := range order {
		if st, ok := rt.statuses[id]; ok {
			out = append(out, st)
		}
	}
	return out
}

func probeTool(id EngineID, name, binPath string, versionArgs []string, desc string) EngineStatus {
	st := EngineStatus{
		ID:          id,
		Name:        name,
		Description: desc,
	}
	if binPath == "" {
		return st
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	stdout, stderr, err := runSubprocess(ctx, binPath, versionArgs, nil)
	if err != nil {
		return st
	}
	st.Available = true
	st.Path = binPath
	combined := strings.TrimSpace(stdout)
	if combined == "" {
		combined = strings.TrimSpace(stderr)
	}
	if idx := strings.IndexByte(combined, '\n'); idx >= 0 {
		combined = strings.TrimSpace(combined[:idx])
	}
	if len(combined) > 72 {
		combined = combined[:72] + "..."
	}
	st.Version = combined
	return st
}

func discoverBinary(baseName, customPath, envPath string, extra []string) string {
	var candidates []string
	if customPath != "" {
		candidates = append(candidates, customPath)
	}
	if envPath != "" {
		candidates = append(candidates, envPath)
	}
	// Check portable bundled bin directories next to executable and cwd.
	binFile := exeName(baseName)
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(exeDir, "bin", binFile),
			filepath.Join(exeDir, "bin", baseName, binFile),
			filepath.Join(exeDir, "..", "Resources", "bin", binFile),
			filepath.Join(exeDir, "..", "Resources", baseName, binFile),
		)
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(cwd, "bin", binFile),
			filepath.Join(cwd, "bin", baseName, binFile),
		)
	}
	candidates = append(candidates, extra...)
	for _, c := range candidates {
		if c != "" && fileExecutable(c) {
			return c
		}
	}
	if found, err := exec.LookPath(baseName); err == nil && found != "" {
		return found
	}
	return ""
}

func exeName(base string) string {
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(base), ".exe") {
		return base + ".exe"
	}
	return base
}

func fileExecutable(p string) bool {
	info, err := os.Stat(p)
	if err != nil || info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode()&0o111 != 0
}

func homeBinCandidates(name string) []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	bin := exeName(name)
	return []string{
		filepath.Join(home, ".local", "bin", bin),
		filepath.Join(home, "bin", bin),
	}
}

func commonUnixCandidates(name string) []string {
	out := homeBinCandidates(name)
	if runtime.GOOS != "windows" {
		out = append(out,
			filepath.Join("/opt/homebrew/bin", name),
			filepath.Join("/usr/local/bin", name),
			filepath.Join("/usr/bin", name),
			filepath.Join("/snap/bin", name),
		)
	}
	return out
}

func extraFFmpegCandidates() []string {
	out := commonUnixCandidates("ffmpeg")
	if runtime.GOOS == "windows" {
		out = append(out,
			`C:\ffmpeg\bin\ffmpeg.exe`,
			`C:\Program Files\ffmpeg\bin\ffmpeg.exe`,
		)
	}
	return out
}

func extraFFprobeCandidates() []string {
	out := commonUnixCandidates("ffprobe")
	if runtime.GOOS == "windows" {
		out = append(out,
			`C:\ffmpeg\bin\ffprobe.exe`,
			`C:\Program Files\ffmpeg\bin\ffprobe.exe`,
		)
	}
	return out
}

func extraLibreOfficeCandidates() []string {
	out := commonUnixCandidates("soffice")
	switch runtime.GOOS {
	case "darwin":
		out = append(out, "/Applications/LibreOffice.app/Contents/MacOS/soffice")
	case "windows":
		out = append(out,
			`C:\Program Files\LibreOffice\program\soffice.com`,
			`C:\Program Files\LibreOffice\program\soffice.exe`,
			`C:\Program Files (x86)\LibreOffice\program\soffice.exe`,
		)
	}
	return out
}

func extraPandocCandidates() []string {
	out := commonUnixCandidates("pandoc")
	if runtime.GOOS == "windows" {
		if localApp := os.Getenv("LOCALAPPDATA"); localApp != "" {
			out = append(out, filepath.Join(localApp, "Pandoc", "pandoc.exe"))
		}
		out = append(out, `C:\Program Files\Pandoc\pandoc.exe`)
	}
	return out
}

func extraPopplerCandidates() []string {
	out := commonUnixCandidates("pdftoppm")
	if runtime.GOOS == "windows" {
		out = append(out, `C:\Program Files\poppler\Library\bin\pdftoppm.exe`)
	}
	return out
}

func extraTesseractCandidates() []string {
	out := commonUnixCandidates("tesseract")
	if runtime.GOOS == "windows" {
		out = append(out,
			`C:\Program Files\Tesseract-OCR\tesseract.exe`,
			`C:\Program Files (x86)\Tesseract-OCR\tesseract.exe`,
		)
	}
	return out
}

// SubprocessHooks allows real-time streaming inspection of stdout/stderr lines.
type SubprocessHooks struct {
	OnStdoutLine func(line string)
	OnStderrLine func(line string)
	RedactSecret string
}

// RunCommand executes a subprocess safely with context cancellation, closed stdin, and bounded output buffers.
func RunCommand(ctx context.Context, bin string, args []string, hooks *SubprocessHooks) (string, string, error) {
	return runSubprocess(ctx, bin, args, hooks)
}

func runSubprocess(ctx context.Context, bin string, args []string, hooks *SubprocessHooks) (string, string, error) {
	if err := ctx.Err(); err != nil {
		if errors.Is(err, context.Canceled) {
			return "", "", ErrCanceled
		}
		return "", "", err
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	configurePlatformCmd(cmd)
	cmd.Stdin = bytes.NewReader(nil)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", "", err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", "", err
	}
	if err := cmd.Start(); err != nil {
		return "", "", err
	}

	const maxCapture = 4 * 1024 * 1024
	var stdoutBuf, stderrBuf bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		var lineFn func(string)
		if hooks != nil {
			lineFn = hooks.OnStdoutLine
		}
		streamReader(stdoutPipe, &stdoutBuf, maxCapture, lineFn)
	}()
	go func() {
		defer wg.Done()
		var lineFn func(string)
		if hooks != nil {
			lineFn = hooks.OnStderrLine
		}
		streamReader(stderrPipe, &stderrBuf, maxCapture, lineFn)
	}()

	wg.Wait()
	waitErr := cmd.Wait()
	outStr := stdoutBuf.String()
	errStr := stderrBuf.String()

	if hooks != nil && hooks.RedactSecret != "" {
		outStr = strings.ReplaceAll(outStr, hooks.RedactSecret, "[redacted]")
		errStr = strings.ReplaceAll(errStr, hooks.RedactSecret, "[redacted]")
	}

	if waitErr != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return outStr, errStr, ErrCanceled
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return outStr, errStr, fmt.Errorf("%s timed out", filepath.Base(bin))
		}
		msg := strings.TrimSpace(errStr)
		if msg == "" {
			msg = strings.TrimSpace(outStr)
		}
		if len(msg) > 400 {
			msg = msg[len(msg)-400:]
		}
		if msg != "" {
			return outStr, errStr, fmt.Errorf("%s failed: %s", filepath.Base(bin), msg)
		}
		return outStr, errStr, fmt.Errorf("%s failed: %w", filepath.Base(bin), waitErr)
	}
	return outStr, errStr, nil
}

func streamReader(r io.Reader, dst *bytes.Buffer, limit int, onLine func(string)) {
	buf := make([]byte, 4096)
	var pending string
	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			if dst.Len() < limit {
				remain := limit - dst.Len()
				if remain > len(chunk) {
					remain = len(chunk)
				}
				_, _ = dst.Write(chunk[:remain])
			}
			if onLine != nil {
				pending += string(chunk)
				for {
					idx := strings.IndexAny(pending, "\r\n")
					if idx < 0 {
						break
					}
					line := pending[:idx]
					pending = pending[idx+1:]
					if line != "" {
						onLine(line)
					}
				}
			}
		}
		if err != nil {
			break
		}
	}
	if onLine != nil && strings.TrimSpace(pending) != "" {
		onLine(pending)
	}
}

func formigoEnv(name, legacyName string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return strings.TrimSpace(os.Getenv(legacyName))
}
