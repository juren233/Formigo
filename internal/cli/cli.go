package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"formigo/internal/config"
	"formigo/internal/converter"
)

const usageText = `Formigo — Cross-Platform Offline File Format Converter (Go + MyGo)

Usage:
  formigo                                      Launch native GPU desktop UI
  formigo engines [--json]                     Inspect built-in & external conversion engines
  formigo capabilities [--json]                List active format capability matrix
  formigo targets <file-or-ext> [--json]       List available output formats for a source file/ext
  formigo convert <files...> --to <ext> [opts] Convert one or more files
  formigo images-to-pdf <imgs...> [opts]       Merge ordered images into a single PDF
  formigo merge-pdfs <pdfs...> [opts]          Merge multiple PDFs into a single PDF
  formigo pack-zip <files...> [opts]           Pack multiple files into a ZIP archive

Options:
  -t, --to <format>            Target format extension (e.g. webp, pdf, mp4, xlsx, docx, md)
  -o, --output <file>          Explicit output file path (single output or merge modes)
  -d, --output-dir <dir>       Output directory for batch conversions
      --quality <1-100>        Image compression quality (default: 90)
      --max-width <px>         Proportional max image width without upscaling
      --video-codec <codec>    Video codec: h264, h265, av1, vp9, copy (default: h264)
      --audio-bitrate <rate>   Audio bitrate: 128k, 192k, 256k, 320k (default: 192k)
      --pdf-op <mode>          PDF operation: convert, split, encrypt, decrypt, extract-images
      --split-span <pages>     Pages per file when splitting PDF (default: 1)
      --password <secret>      PDF password for encrypt/decrypt (redacted from logs/JSON)
      --encoding <enc>         Source text encoding: auto, utf-8, gb18030, utf-16le, utf-16be
      --workers <n>            Parallel batch workers (default: from preferences)
      --overwrite              Overwrite existing target files instead of auto-numbering
      --json                   Emit machine-readable JSON output
  -h, --help                   Show this help message
`

// IsCLIInvocation reports whether os.Args[1:] requests headless CLI execution rather than GUI launch.
func IsCLIInvocation(args []string) bool {
	if len(args) == 0 {
		return false
	}
	first := strings.TrimSpace(args[0])
	switch first {
	case "--cli", "-h", "--help", "help", "version", "--version", "-v",
		"engines", "capabilities", "targets", "convert", "images-to-pdf", "merge-pdfs", "pack-zip":
		return true
	default:
		return false
	}
}

type parsedCLI struct {
	cmd        string
	files      []string
	to         string
	output     string
	outputDir  string
	workers    int
	jsonOutput bool
	opts       converter.Options
}

// Run executes the headless CLI and returns an OS exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "--cli" {
		args = args[1:]
	}
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		_, _ = io.WriteString(stdout, usageText)
		return 0
	}
	if args[0] == "version" || args[0] == "--version" || args[0] == "-v" {
		_, _ = io.WriteString(stdout, "formigo 0.1.0\n")
		return 0
	}

	store := config.LoadStore("")
	prefs := store.Get()

	parsed, err := parseArgs(args, prefs)
	if err != nil {
		writeCLIError(stdout, stderr, parsed.jsonOutput, err, parsed.opts.Password)
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rt := converter.NewRuntime(prefs.EnginePaths)
	pipe, err := converter.NewPipeline(rt)
	if err != nil {
		writeCLIError(stdout, stderr, parsed.jsonOutput, err, parsed.opts.Password)
		return 1
	}
	defer pipe.Close()

	switch parsed.cmd {
	case "engines":
		statuses := rt.Statuses()
		if parsed.jsonOutput {
			writeJSON(stdout, map[string]any{"ok": true, "engines": statuses})
		} else {
			for _, st := range statuses {
				mark := "MISSING"
				if st.Available {
					mark = "READY  "
				}
				loc := st.Path
				if st.Builtin {
					loc = "(built-in)"
				}
				_, _ = fmt.Fprintf(stdout, "[%s] %-22s %s %s\n", mark, st.Name, st.Version, loc)
			}
		}
		return 0

	case "capabilities":
		matrix := converter.ActiveMatrix(rt)
		if parsed.jsonOutput {
			writeJSON(stdout, map[string]any{
				"ok":      true,
				"engines": rt.Statuses(),
				"matrix":  matrix,
			})
		} else {
			for _, g := range matrix {
				_, _ = fmt.Fprintf(stdout, "%-14s : %s  -->  %s\n",
					g.Category,
					strings.Join(g.Sources, ", "),
					strings.Join(g.Targets, ", "),
				)
			}
		}
		return 0

	case "targets":
		if len(parsed.files) == 0 {
			writeCLIError(stdout, stderr, parsed.jsonOutput, errors.New("usage: formigo targets <file-or-ext>"), "")
			return 2
		}
		srcExt := converter.NormalizeExt(parsed.files[0])
		choices := converter.TargetsForExt(srcExt, rt, true)
		if parsed.jsonOutput {
			writeJSON(stdout, map[string]any{
				"ok":        true,
				"sourceExt": srcExt,
				"category":  converter.CategoryForExt(srcExt),
				"targets":   choices,
			})
		} else {
			if len(choices) == 0 {
				_, _ = fmt.Fprintf(stdout, "No active targets for .%s\n", srcExt)
			} else {
				for _, ch := range choices {
					_, _ = fmt.Fprintf(stdout, ".%-8s (engine: %s)\n", ch.Ext, ch.Engine)
				}
			}
		}
		return 0

	case "images-to-pdf", "merge-pdfs", "pack-zip":
		if len(parsed.files) == 0 {
			writeCLIError(stdout, stderr, parsed.jsonOutput, fmt.Errorf("%s requires at least one input file", parsed.cmd), parsed.opts.Password)
			return 2
		}
		var mode converter.MergeMode
		switch parsed.cmd {
		case "images-to-pdf":
			mode = converter.MergeImagesToPDF
		case "merge-pdfs":
			mode = converter.MergePDFs
		case "pack-zip":
			mode = converter.MergeFilesToZip
		}
		res, err := pipe.Convert(ctx, converter.Request{
			InputPaths: parsed.files,
			OutputPath: parsed.output,
			OutputDir:  parsed.outputDir,
			MergeMode:  mode,
			Options:    parsed.opts,
		}, nil)
		if err != nil {
			writeCLIError(stdout, stderr, parsed.jsonOutput, err, parsed.opts.Password)
			return 1
		}
		if parsed.jsonOutput {
			writeJSON(stdout, map[string]any{"ok": true, "result": res})
		} else {
			_, _ = fmt.Fprintf(stdout, "OK -> %s (%s, %v)\n", res.OutputPath, formatBytes(res.OutputSize), res.Elapsed.Round(1e6))
		}
		return 0

	case "convert":
		if len(parsed.files) == 0 {
			writeCLIError(stdout, stderr, parsed.jsonOutput, errors.New("convert requires at least one input file"), parsed.opts.Password)
			return 2
		}
		if parsed.to == "" && parsed.opts.PDFOperation == converter.PDFOpConvert {
			writeCLIError(stdout, stderr, parsed.jsonOutput, errors.New("convert requires --to <format> (or --pdf-op)"), parsed.opts.Password)
			return 2
		}
		if parsed.output != "" && len(parsed.files) > 1 {
			writeCLIError(stdout, stderr, parsed.jsonOutput, errors.New("--output can only be used with a single input file; use --output-dir for batch conversion"), parsed.opts.Password)
			return 2
		}

		jobs := make([]converter.BatchJob, len(parsed.files))
		for i, f := range parsed.files {
			jobs[i] = converter.BatchJob{
				ID: strconv.Itoa(i),
				Request: converter.Request{
					InputPaths: []string{f},
					TargetExt:  parsed.to,
					OutputPath: parsed.output,
					OutputDir:  parsed.outputDir,
					Options:    parsed.opts,
				},
			}
		}

		type itemOutcome struct {
			Input  string            `json:"input"`
			Result *converter.Result `json:"result,omitempty"`
			Error  string            `json:"error,omitempty"`
		}
		outcomes := make([]itemOutcome, len(parsed.files))
		for i, f := range parsed.files {
			outcomes[i].Input = f
		}

		ctrl := pipe.StartBatch(ctx, jobs, parsed.workers, func(u converter.BatchUpdate) {
			idx, err := strconv.Atoi(u.ID)
			if err != nil || idx < 0 || idx >= len(outcomes) {
				return
			}
			if u.Stage == converter.StageDone && u.Result != nil {
				outcomes[idx].Result = u.Result
				store.RememberTarget(u.Result.SourceExt, u.Result.TargetExt)
			} else if u.Stage == converter.StageFailed || u.Stage == converter.StageCanceled {
				if u.Err != nil {
					outcomes[idx].Error = redactSecret(u.Err.Error(), parsed.opts.Password)
				} else {
					outcomes[idx].Error = string(u.Stage)
				}
			}
		})
		ctrl.Wait()

		failed := 0
		for _, o := range outcomes {
			if o.Error != "" || o.Result == nil {
				failed++
			}
		}

		if parsed.jsonOutput {
			writeJSON(stdout, map[string]any{
				"ok":      failed == 0,
				"total":   len(outcomes),
				"failed":  failed,
				"results": outcomes,
			})
		} else {
			for _, o := range outcomes {
				if o.Result != nil {
					_, _ = fmt.Fprintf(stdout, "[OK]   %s -> %s (%s, %s)\n",
						filepath.Base(o.Input), o.Result.OutputPath, o.Result.Engine, formatBytes(o.Result.OutputSize))
				} else {
					_, _ = fmt.Fprintf(stderr, "[FAIL] %s : %s\n", filepath.Base(o.Input), o.Error)
				}
			}
		}
		if failed > 0 {
			return 1
		}
		return 0

	default:
		writeCLIError(stdout, stderr, parsed.jsonOutput, fmt.Errorf("unknown command: %s", parsed.cmd), "")
		return 2
	}
}

func parseArgs(args []string, prefs config.Preferences) (parsedCLI, error) {
	p := parsedCLI{
		cmd:     args[0],
		workers: prefs.Concurrency,
		opts:    converter.DefaultOptions(),
	}
	p.opts.ImageQuality = prefs.ImageQuality
	p.opts.VideoCodec = prefs.VideoCodec
	p.opts.AudioBitrate = prefs.AudioBitrate
	p.opts.Overwrite = prefs.ConflictPolicy == config.ConflictOverwrite

	for i := 1; i < len(args); i++ {
		a := args[i]
		if a == "--json" {
			p.jsonOutput = true
			continue
		}
		if a == "--overwrite" {
			p.opts.Overwrite = true
			continue
		}
		if a == "-h" || a == "--help" {
			p.cmd = "help"
			return p, nil
		}
		nextVal := func() (string, error) {
			if i+1 >= len(args) {
				return "", fmt.Errorf("option %s requires a value", a)
			}
			i++
			return args[i], nil
		}
		switch a {
		case "-t", "--to":
			v, err := nextVal()
			if err != nil {
				return p, err
			}
			p.to = converter.NormalizeExt(v)
		case "-o", "--output":
			v, err := nextVal()
			if err != nil {
				return p, err
			}
			p.output = v
		case "-d", "--output-dir":
			v, err := nextVal()
			if err != nil {
				return p, err
			}
			p.outputDir = v
		case "--quality":
			v, err := nextVal()
			if err != nil {
				return p, err
			}
			q, err := strconv.Atoi(v)
			if err != nil || q < 1 || q > 100 {
				return p, errors.New("--quality must be an integer between 1 and 100")
			}
			p.opts.ImageQuality = q
		case "--max-width":
			v, err := nextVal()
			if err != nil {
				return p, err
			}
			mw, err := strconv.Atoi(v)
			if err != nil || mw < 0 {
				return p, errors.New("--max-width must be a non-negative integer")
			}
			p.opts.ImageMaxWidth = mw
		case "--video-codec":
			v, err := nextVal()
			if err != nil {
				return p, err
			}
			p.opts.VideoCodec = strings.ToLower(strings.TrimSpace(v))
		case "--audio-bitrate":
			v, err := nextVal()
			if err != nil {
				return p, err
			}
			p.opts.AudioBitrate = strings.TrimSpace(v)
		case "--pdf-op":
			v, err := nextVal()
			if err != nil {
				return p, err
			}
			p.opts.PDFOperation = converter.PDFOperation(strings.ToLower(strings.TrimSpace(v)))
		case "--split-span":
			v, err := nextVal()
			if err != nil {
				return p, err
			}
			sp, err := strconv.Atoi(v)
			if err != nil || sp < 1 {
				return p, errors.New("--split-span must be >= 1")
			}
			p.opts.PDFSplitSpan = sp
		case "--password":
			v, err := nextVal()
			if err != nil {
				return p, err
			}
			p.opts.Password = v
		case "--encoding":
			v, err := nextVal()
			if err != nil {
				return p, err
			}
			p.opts.TextEncoding = v
		case "--workers":
			v, err := nextVal()
			if err != nil {
				return p, err
			}
			w, err := strconv.Atoi(v)
			if err != nil || w < 1 || w > 16 {
				return p, errors.New("--workers must be between 1 and 16")
			}
			p.workers = w
		default:
			if strings.HasPrefix(a, "-") {
				return p, fmt.Errorf("unknown flag: %s", a)
			}
			p.files = append(p.files, a)
		}
	}
	return p, nil
}

func redactSecret(msg, secret string) string {
	if secret != "" {
		msg = strings.ReplaceAll(msg, secret, "[redacted]")
	}
	return msg
}

func writeCLIError(stdout, stderr io.Writer, asJSON bool, err error, secret string) {
	msg := redactSecret(err.Error(), secret)
	if asJSON {
		writeJSON(stdout, map[string]any{
			"ok":    false,
			"error": msg,
		})
		return
	}
	_, _ = fmt.Fprintf(stderr, "Error: %s\n", msg)
}

func writeJSON(w io.Writer, payload any) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(payload)
}

func formatBytes(b int64) string {
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
