package converter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// SubtitleCue represents a single timed subtitle entry in milliseconds.
type SubtitleCue struct {
	Index   int    `json:"index"`
	StartMs int64  `json:"startMs"`
	EndMs   int64  `json:"endMs"`
	Text    string `json:"text"`
}

func convertSubtitleNative(ctx context.Context, inPath, srcExt, outPath, dstExt string, opts Options) error {
	if err := ctx.Err(); err != nil {
		return ErrCanceled
	}
	raw, _, err := ReadTextFile(inPath, opts.TextEncoding)
	if err != nil {
		return err
	}

	var cues []SubtitleCue
	switch srcExt {
	case "srt":
		cues, err = parseSRT(raw)
	case "vtt":
		cues, err = parseVTT(raw)
	case "ass", "ssa":
		cues, err = parseASS(raw)
	default:
		return fmt.Errorf("unsupported subtitle input: .%s", srcExt)
	}
	if err != nil {
		return err
	}
	if len(cues) == 0 {
		return errors.New("no valid subtitle cues found in file")
	}

	switch dstExt {
	case "srt":
		return os.WriteFile(outPath, []byte(renderSRT(cues)), 0o644)
	case "vtt":
		return os.WriteFile(outPath, []byte(renderVTT(cues)), 0o644)
	case "ass":
		return os.WriteFile(outPath, []byte(renderASS(safeStem(inPath), cues)), 0o644)
	case "txt":
		var lines []string
		for _, c := range cues {
			if strings.TrimSpace(c.Text) != "" {
				lines = append(lines, strings.TrimSpace(c.Text))
			}
		}
		return os.WriteFile(outPath, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	case "json":
		b, err := json.MarshalIndent(cues, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(outPath, append(b, '\n'), 0o644)
	default:
		return fmt.Errorf("unsupported subtitle output format: .%s", dstExt)
	}
}

var (
	reSRTTimestamp = regexp.MustCompile(`(\d{1,2}:\d{2}:\d{2}[,\.]\d{1,3})\s*-->\s*(\d{1,2}:\d{2}:\d{2}[,\.]\d{1,3})`)
	reVTTTimestamp = regexp.MustCompile(`((?:\d{1,2}:)?\d{2}:\d{2}\.\d{1,3})\s*-->\s*((?:\d{1,2}:)?\d{2}:\d{2}\.\d{1,3})`)
	reHTMLTags     = regexp.MustCompile(`<[^>]+>`)
	reASSTags      = regexp.MustCompile(`\{[^}]*\}`)
)

func parseSRT(text string) ([]SubtitleCue, error) {
	blocks := splitBlankBlocks(text)
	var cues []SubtitleCue
	for _, b := range blocks {
		lines := strings.Split(strings.TrimSpace(b), "\n")
		if len(lines) == 0 {
			continue
		}
		timeIdx := -1
		var startMs, endMs int64
		for i, l := range lines {
			if m := reSRTTimestamp.FindStringSubmatch(l); len(m) == 3 {
				s, err1 := parseTimecodeMs(m[1])
				e, err2 := parseTimecodeMs(m[2])
				if err1 == nil && err2 == nil {
					timeIdx = i
					startMs = s
					endMs = e
					break
				}
			}
		}
		if timeIdx < 0 || timeIdx+1 >= len(lines) {
			continue
		}
		body := cleanSubtitleText(strings.Join(lines[timeIdx+1:], "\n"))
		if body == "" {
			continue
		}
		cues = append(cues, SubtitleCue{
			Index:   len(cues) + 1,
			StartMs: startMs,
			EndMs:   endMs,
			Text:    body,
		})
	}
	return cues, nil
}

func parseVTT(text string) ([]SubtitleCue, error) {
	blocks := splitBlankBlocks(text)
	var cues []SubtitleCue
	for _, b := range blocks {
		trimmed := strings.TrimSpace(b)
		if strings.HasPrefix(trimmed, "WEBVTT") || strings.HasPrefix(trimmed, "NOTE") || strings.HasPrefix(trimmed, "STYLE") {
			continue
		}
		lines := strings.Split(trimmed, "\n")
		timeIdx := -1
		var startMs, endMs int64
		for i, l := range lines {
			if m := reVTTTimestamp.FindStringSubmatch(l); len(m) == 3 {
				s, err1 := parseTimecodeMs(m[1])
				e, err2 := parseTimecodeMs(m[2])
				if err1 == nil && err2 == nil {
					timeIdx = i
					startMs = s
					endMs = e
					break
				}
			}
		}
		if timeIdx < 0 || timeIdx+1 >= len(lines) {
			continue
		}
		body := cleanSubtitleText(strings.Join(lines[timeIdx+1:], "\n"))
		if body == "" {
			continue
		}
		cues = append(cues, SubtitleCue{
			Index:   len(cues) + 1,
			StartMs: startMs,
			EndMs:   endMs,
			Text:    body,
		})
	}
	return cues, nil
}

func parseASS(text string) ([]SubtitleCue, error) {
	lines := strings.Split(text, "\n")
	inEvents := false
	startCol, endCol, textCol := 1, 2, 9
	totalCols := 10
	var cues []SubtitleCue

	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inEvents = strings.EqualFold(line, "[Events]")
			continue
		}
		if !inEvents {
			continue
		}
		low := strings.ToLower(line)
		if strings.HasPrefix(low, "format:") {
			spec := strings.TrimSpace(line[len("format:"):])
			cols := strings.Split(spec, ",")
			totalCols = len(cols)
			for i, col := range cols {
				switch strings.ToLower(strings.TrimSpace(col)) {
				case "start":
					startCol = i
				case "end":
					endCol = i
				case "text":
					textCol = i
				}
			}
			continue
		}
		if strings.HasPrefix(low, "dialogue:") {
			payload := strings.TrimSpace(line[len("dialogue:"):])
			parts := strings.SplitN(payload, ",", totalCols)
			if len(parts) <= textCol || len(parts) <= endCol || len(parts) <= startCol {
				continue
			}
			s, err1 := parseTimecodeMs(strings.TrimSpace(parts[startCol]))
			e, err2 := parseTimecodeMs(strings.TrimSpace(parts[endCol]))
			if err1 != nil || err2 != nil {
				continue
			}
			txt := parts[textCol]
			txt = strings.ReplaceAll(txt, `\N`, "\n")
			txt = strings.ReplaceAll(txt, `\n`, "\n")
			txt = strings.ReplaceAll(txt, `\h`, " ")
			txt = reASSTags.ReplaceAllString(txt, "")
			txt = cleanSubtitleText(txt)
			if txt == "" {
				continue
			}
			cues = append(cues, SubtitleCue{
				Index:   len(cues) + 1,
				StartMs: s,
				EndMs:   e,
				Text:    txt,
			})
		}
	}
	return cues, nil
}

func splitBlankBlocks(s string) []string {
	var blocks []string
	var cur []string
	for _, line := range strings.Split(normalizeNewlines(s), "\n") {
		if strings.TrimSpace(line) == "" {
			if len(cur) > 0 {
				blocks = append(blocks, strings.Join(cur, "\n"))
				cur = nil
			}
		} else {
			cur = append(cur, line)
		}
	}
	if len(cur) > 0 {
		blocks = append(blocks, strings.Join(cur, "\n"))
	}
	return blocks
}

func cleanSubtitleText(s string) string {
	s = reHTMLTags.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

func parseTimecodeMs(tc string) (int64, error) {
	tc = strings.TrimSpace(strings.ReplaceAll(tc, ",", "."))
	parts := strings.Split(tc, ":")
	var h, m int64
	var secStr string
	switch len(parts) {
	case 3:
		var err error
		h, err = strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return 0, err
		}
		m, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return 0, err
		}
		secStr = parts[2]
	case 2:
		var err error
		m, err = strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return 0, err
		}
		secStr = parts[1]
	default:
		return 0, fmt.Errorf("invalid timestamp: %s", tc)
	}

	secParts := strings.SplitN(secStr, ".", 2)
	sec, err := strconv.ParseInt(secParts[0], 10, 64)
	if err != nil {
		return 0, err
	}
	var ms int64
	if len(secParts) == 2 {
		frac := secParts[1]
		for len(frac) < 3 {
			frac += "0"
		}
		if len(frac) > 3 {
			frac = frac[:3]
		}
		ms, err = strconv.ParseInt(frac, 10, 64)
		if err != nil {
			return 0, err
		}
	}
	return ((h*3600+m*60+sec)*1000 + ms), nil
}

func formatSRTTime(ms int64) string {
	if ms < 0 {
		ms = 0
	}
	h := ms / 3_600_000
	ms %= 3_600_000
	m := ms / 60_000
	ms %= 60_000
	s := ms / 1000
	rem := ms % 1000
	return fmt.Sprintf("%02d:%02d:%02d,%03d", h, m, s, rem)
}

func formatVTTTime(ms int64) string {
	return strings.ReplaceAll(formatSRTTime(ms), ",", ".")
}

func formatASSTime(ms int64) string {
	if ms < 0 {
		ms = 0
	}
	h := ms / 3_600_000
	ms %= 3_600_000
	m := ms / 60_000
	ms %= 60_000
	s := ms / 1000
	cs := (ms % 1000) / 10
	return fmt.Sprintf("%d:%02d:%02d.%02d", h, m, s, cs)
}

func renderSRT(cues []SubtitleCue) string {
	var sb strings.Builder
	for i, c := range cues {
		sb.WriteString(fmt.Sprintf("%d\n%s --> %s\n%s\n\n", i+1, formatSRTTime(c.StartMs), formatSRTTime(c.EndMs), c.Text))
	}
	return sb.String()
}

func renderVTT(cues []SubtitleCue) string {
	var sb strings.Builder
	sb.WriteString("WEBVTT\n\n")
	for i, c := range cues {
		sb.WriteString(fmt.Sprintf("%d\n%s --> %s\n%s\n\n", i+1, formatVTTTime(c.StartMs), formatVTTTime(c.EndMs), c.Text))
	}
	return sb.String()
}

func renderASS(title string, cues []SubtitleCue) string {
	var sb strings.Builder
	sb.WriteString("[Script Info]\nTitle: " + title + "\nScriptType: v4.00+\nWrapStyle: 0\nPlayResX: 1920\nPlayResY: 1080\n\n")
	sb.WriteString("[V4+ Styles]\nFormat: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\n")
	sb.WriteString("Style: Default,Arial,48,&H00FFFFFF,&H000000FF,&H00000000,&H64000000,0,0,0,0,100,100,0,0,1,2,1,2,20,20,36,1\n\n")
	sb.WriteString("[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n")
	for _, c := range cues {
		escaped := strings.ReplaceAll(c.Text, "\n", `\N`)
		sb.WriteString(fmt.Sprintf("Dialogue: 0,%s,%s,Default,,0,0,0,,%s\n", formatASSTime(c.StartMs), formatASSTime(c.EndMs), escaped))
	}
	return sb.String()
}
