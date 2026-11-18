package converter

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/xuri/excelize/v2"
	"gopkg.in/yaml.v3"
)

func convertDataOrSheetNative(ctx context.Context, inPath, srcExt, outPath, dstExt string, opts Options) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, ErrCanceled
	}

	isTabularSrc := srcExt == "xlsx" || srcExt == "xlsm" || srcExt == "csv" || srcExt == "tsv"
	isTabularDst := dstExt == "xlsx" || dstExt == "csv" || dstExt == "tsv" || dstExt == "md" || dstExt == "html"

	if isTabularSrc {
		rows, err := readTabularRows(inPath, srcExt, opts.TextEncoding)
		if err != nil {
			return nil, err
		}
		switch dstExt {
		case "xlsx":
			return nil, writeRowsToXLSX(rows, safeStem(inPath), outPath)
		case "csv":
			return nil, writeRowsToDelimited(rows, ',', true, outPath)
		case "tsv":
			return nil, writeRowsToDelimited(rows, '\t', false, outPath)
		case "md":
			return nil, os.WriteFile(outPath, []byte(renderRowsMarkdown(rows)), 0o644)
		case "html":
			return nil, os.WriteFile(outPath, []byte(renderRowsHTML(safeStem(inPath), rows)), 0o644)
		case "json", "yaml", "toml", "xml":
			objs := rowsToObjects(rows)
			return nil, writeStructuredValue(objs, dstExt, outPath)
		default:
			return nil, fmt.Errorf("unsupported spreadsheet target: .%s", dstExt)
		}
	}

	// Structured Data source (JSON, YAML, TOML, XML)
	val, err := readStructuredValue(inPath, srcExt, opts.TextEncoding)
	if err != nil {
		return nil, err
	}

	if isTabularDst {
		rows := structuredValueToRows(val)
		if len(rows) == 0 {
			return nil, errors.New("structured data contains no convertible records")
		}
		switch dstExt {
		case "xlsx":
			return nil, writeRowsToXLSX(rows, safeStem(inPath), outPath)
		case "csv":
			return nil, writeRowsToDelimited(rows, ',', true, outPath)
		case "tsv":
			return nil, writeRowsToDelimited(rows, '\t', false, outPath)
		case "md":
			return nil, os.WriteFile(outPath, []byte(renderRowsMarkdown(rows)), 0o644)
		case "html":
			return nil, os.WriteFile(outPath, []byte(renderRowsHTML(safeStem(inPath), rows)), 0o644)
		}
	}

	return nil, writeStructuredValue(val, dstExt, outPath)
}

func readTabularRows(path, srcExt, encHint string) ([][]string, error) {
	if srcExt == "xlsx" || srcExt == "xlsm" {
		f, err := excelize.OpenFile(path)
		if err != nil {
			return nil, fmt.Errorf("cannot open Excel workbook: %w", err)
		}
		defer f.Close()
		sheets := f.GetSheetList()
		if len(sheets) == 0 {
			return nil, errors.New("workbook has no sheets")
		}
		for _, sh := range sheets {
			rows, err := f.GetRows(sh)
			if err == nil && len(rows) > 0 {
				return normalizeRowWidths(rows), nil
			}
		}
		return [][]string{}, nil
	}

	text, _, err := ReadTextFile(path, encHint)
	if err != nil {
		return nil, err
	}
	r := csv.NewReader(strings.NewReader(text))
	if srcExt == "tsv" {
		r.Comma = '\t'
	}
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	rows, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", strings.ToUpper(srcExt), err)
	}
	return normalizeRowWidths(rows), nil
}

func normalizeRowWidths(rows [][]string) [][]string {
	maxCols := 0
	for _, r := range rows {
		if len(r) > maxCols {
			maxCols = len(r)
		}
	}
	if maxCols == 0 {
		return nil
	}
	out := make([][]string, 0, len(rows))
	for _, r := range rows {
		nonEmpty := false
		padded := make([]string, maxCols)
		for i := 0; i < maxCols; i++ {
			if i < len(r) {
				padded[i] = r[i]
				if strings.TrimSpace(r[i]) != "" {
					nonEmpty = true
				}
			}
		}
		if nonEmpty {
			out = append(out, padded)
		}
	}
	return out
}

func writeRowsToXLSX(rows [][]string, sheetTitle, outPath string) error {
	f := excelize.NewFile()
	defer f.Close()

	sheet := "Sheet1"
	cleanName := sanitizeExcelSheetName(sheetTitle)
	if cleanName != "" && cleanName != sheet {
		_ = f.SetSheetName(sheet, cleanName)
		sheet = cleanName
	}

	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Color: "#111827", Size: 11},
		Fill: excelize.Fill{Type: "pattern", Color: []string{"#F3F4F6"}, Pattern: 1},
	})

	colWidths := make(map[int]float64)
	for rIdx, row := range rows {
		for cIdx, cellVal := range row {
			coord, err := excelize.CoordinatesToCellName(cIdx+1, rIdx+1)
			if err != nil {
				continue
			}
			// Preserve numeric values where unambiguous (no leading zeros like zip codes).
			if rIdx > 0 && looksCleanNumber(cellVal) {
				if num, err := strconv.ParseFloat(cellVal, 64); err == nil {
					_ = f.SetCellFloat(sheet, coord, num, -1, 64)
				} else {
					_ = f.SetCellStr(sheet, coord, cellVal)
				}
			} else {
				_ = f.SetCellStr(sheet, coord, cellVal)
			}
			if rIdx == 0 && headerStyle != 0 {
				_ = f.SetCellStyle(sheet, coord, coord, headerStyle)
			}
			w := displayWidth(cellVal) + 3
			if w > 52 {
				w = 52
			}
			if w > colWidths[cIdx] {
				colWidths[cIdx] = w
			}
		}
	}

	for cIdx, w := range colWidths {
		if w < 10 {
			w = 10
		}
		colName, err := excelize.ColumnNumberToName(cIdx + 1)
		if err == nil {
			_ = f.SetColWidth(sheet, colName, colName, w)
		}
	}

	return f.SaveAs(outPath)
}

func sanitizeExcelSheetName(s string) string {
	replacer := strings.NewReplacer(":", "_", "\\", "_", "/", "_", "?", "_", "*", "_", "[", "(", "]", ")")
	s = strings.TrimSpace(replacer.Replace(s))
	runes := []rune(s)
	if len(runes) > 31 {
		s = string(runes[:31])
	}
	return s
}

func looksCleanNumber(s string) bool {
	if s == "" || len(s) > 15 {
		return false
	}
	if len(s) > 1 && s[0] == '0' && s[1] != '.' {
		return false
	}
	if len(s) > 2 && s[0] == '-' && s[1] == '0' && s[2] != '.' {
		return false
	}
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}

func displayWidth(s string) float64 {
	w := 0.0
	for _, r := range s {
		if r == '\n' {
			break
		}
		if r > 0x7F {
			w += 1.8
		} else {
			w += 1.0
		}
	}
	return w
}

func writeRowsToDelimited(rows [][]string, sep rune, writeUTF8BOM bool, outPath string) error {
	var buf bytes.Buffer
	if writeUTF8BOM {
		// UTF-8 BOM lets Microsoft Excel open Chinese CSVs directly without encoding prompts.
		buf.Write([]byte{0xEF, 0xBB, 0xBF})
	}
	w := csv.NewWriter(&buf)
	w.Comma = sep
	if err := w.WriteAll(rows); err != nil {
		return err
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}
	return os.WriteFile(outPath, buf.Bytes(), 0o644)
}

func renderRowsMarkdown(rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}
	escapeCell := func(s string) string {
		s = strings.ReplaceAll(s, "\r\n", "<br>")
		s = strings.ReplaceAll(s, "\n", "<br>")
		s = strings.ReplaceAll(s, "|", `\|`)
		return strings.TrimSpace(s)
	}
	var sb strings.Builder
	cols := len(rows[0])
	sb.WriteString("| ")
	for c := 0; c < cols; c++ {
		if c > 0 {
			sb.WriteString(" | ")
		}
		sb.WriteString(escapeCell(rows[0][c]))
	}
	sb.WriteString(" |\n| ")
	for c := 0; c < cols; c++ {
		if c > 0 {
			sb.WriteString(" | ")
		}
		sb.WriteString("---")
	}
	sb.WriteString(" |\n")
	for r := 1; r < len(rows); r++ {
		sb.WriteString("| ")
		for c := 0; c < cols; c++ {
			if c > 0 {
				sb.WriteString(" | ")
			}
			val := ""
			if c < len(rows[r]) {
				val = rows[r][c]
			}
			sb.WriteString(escapeCell(val))
		}
		sb.WriteString(" |\n")
	}
	return sb.String()
}

func renderRowsHTML(title string, rows [][]string) string {
	var sb strings.Builder
	sb.WriteString("<!DOCTYPE html>\n<html lang=\"zh-CN\">\n<head>\n<meta charset=\"utf-8\">\n<title>")
	sb.WriteString(html.EscapeString(title))
	sb.WriteString("</title>\n<style>body{font-family:system-ui,-apple-system,sans-serif;margin:2rem;color:#111827}table{border-collapse:collapse;width:100%;font-size:14px}th,td{border:1px solid #d1d5db;padding:8px 12px;text-align:left;vertical-align:top}th{background:#f3f4f6;font-weight:600}tr:nth-child(even) td{background:#f9fafb}</style>\n</head>\n<body>\n<table>\n")
	for rIdx, row := range rows {
		if rIdx == 0 {
			sb.WriteString("<thead><tr>")
			for _, cell := range row {
				sb.WriteString("<th>" + html.EscapeString(cell) + "</th>")
			}
			sb.WriteString("</tr></thead>\n<tbody>\n")
		} else {
			sb.WriteString("<tr>")
			for _, cell := range row {
				sb.WriteString("<td>" + strings.ReplaceAll(html.EscapeString(cell), "\n", "<br>") + "</td>")
			}
			sb.WriteString("</tr>\n")
		}
	}
	if len(rows) > 0 {
		sb.WriteString("</tbody>\n")
	}
	sb.WriteString("</table>\n</body>\n</html>\n")
	return sb.String()
}

func rowsToObjects(rows [][]string) []map[string]any {
	if len(rows) == 0 {
		return []map[string]any{}
	}
	if len(rows) == 1 {
		rec := make(map[string]any, len(rows[0]))
		for i, v := range rows[0] {
			rec[fmt.Sprintf("col_%d", i+1)] = inferScalar(v)
		}
		return []map[string]any{rec}
	}
	headers := make([]string, len(rows[0]))
	seen := make(map[string]int)
	for i, h := range rows[0] {
		h = strings.TrimSpace(h)
		if h == "" {
			h = fmt.Sprintf("column_%d", i+1)
		}
		if cnt := seen[h]; cnt > 0 {
			seen[h] = cnt + 1
			h = fmt.Sprintf("%s_%d", h, cnt+1)
		} else {
			seen[h] = 1
		}
		headers[i] = h
	}
	out := make([]map[string]any, 0, len(rows)-1)
	for _, r := range rows[1:] {
		obj := make(map[string]any, len(headers))
		for i, h := range headers {
			val := ""
			if i < len(r) {
				val = r[i]
			}
			obj[h] = inferScalar(val)
		}
		out = append(out, obj)
	}
	return out
}

func inferScalar(s string) any {
	trimmed := strings.TrimSpace(s)
	if trimmed == "true" {
		return true
	}
	if trimmed == "false" {
		return false
	}
	if looksCleanNumber(trimmed) {
		if iv, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
			return iv
		}
		if fv, err := strconv.ParseFloat(trimmed, 64); err == nil {
			return fv
		}
	}
	return s
}

func readStructuredValue(path, srcExt, encHint string) (any, error) {
	text, _, err := ReadTextFile(path, encHint)
	if err != nil {
		return nil, err
	}
	switch srcExt {
	case "json":
		var v any
		dec := json.NewDecoder(strings.NewReader(text))
		dec.UseNumber()
		if err := dec.Decode(&v); err != nil {
			return nil, fmt.Errorf("invalid JSON: %w", err)
		}
		return normalizeJSONNumbers(v), nil

	case "yaml":
		var v any
		if err := yaml.Unmarshal([]byte(text), &v); err != nil {
			return nil, fmt.Errorf("invalid YAML: %w", err)
		}
		return normalizeYAMLValue(v), nil

	case "toml":
		var v any
		if _, err := toml.Decode(text, &v); err != nil {
			return nil, fmt.Errorf("invalid TOML: %w", err)
		}
		return normalizeGenericValue(v), nil

	case "xml":
		return parseGenericXML(text)

	default:
		return nil, fmt.Errorf("unsupported structured format: .%s", srcExt)
	}
}

func normalizeJSONNumbers(v any) any {
	switch x := v.(type) {
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return i
		}
		if f, err := x.Float64(); err == nil {
			return f
		}
		return x.String()
	case map[string]any:
		m := make(map[string]any, len(x))
		for k, val := range x {
			m[k] = normalizeJSONNumbers(val)
		}
		return m
	case []any:
		s := make([]any, len(x))
		for i, val := range x {
			s[i] = normalizeJSONNumbers(val)
		}
		return s
	default:
		return v
	}
}

func normalizeYAMLValue(v any) any {
	return normalizeGenericValue(v)
}

func normalizeGenericValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(x))
		for k, val := range x {
			m[k] = normalizeGenericValue(val)
		}
		return m
	case map[any]any:
		m := make(map[string]any, len(x))
		for k, val := range x {
			m[fmt.Sprint(k)] = normalizeGenericValue(val)
		}
		return m
	case []map[string]any:
		s := make([]any, len(x))
		for i, val := range x {
			s[i] = normalizeGenericValue(val)
		}
		return s
	case []any:
		s := make([]any, len(x))
		for i, val := range x {
			s[i] = normalizeGenericValue(val)
		}
		return s
	default:
		return v
	}
}

func writeStructuredValue(val any, dstExt, outPath string) error {
	switch dstExt {
	case "json":
		raw, err := json.MarshalIndent(val, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(outPath, append(raw, '\n'), 0o644)

	case "yaml":
		var buf bytes.Buffer
		enc := yaml.NewEncoder(&buf)
		enc.SetIndent(2)
		if err := enc.Encode(val); err != nil {
			return err
		}
		_ = enc.Close()
		return os.WriteFile(outPath, buf.Bytes(), 0o644)

	case "toml":
		// TOML root must be a table (map); wrap top-level slices under "items".
		root := val
		switch val.(type) {
		case map[string]any:
		default:
			root = map[string]any{"items": val}
		}
		var buf bytes.Buffer
		if err := toml.NewEncoder(&buf).Encode(root); err != nil {
			return err
		}
		return os.WriteFile(outPath, buf.Bytes(), 0o644)

	case "xml":
		var buf bytes.Buffer
		buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
		writeXMLNode(&buf, "root", val, 0)
		return os.WriteFile(outPath, buf.Bytes(), 0o644)

	default:
		return fmt.Errorf("unsupported structured output format: .%s", dstExt)
	}
}

func structuredValueToRows(val any) [][]string {
	// Recursively unwrap single-key wrapper maps (e.g. {"root": {"item": [...]}} or {"items": [...]})
	for {
		m, ok := val.(map[string]any)
		if !ok || len(m) != 1 {
			break
		}
		unwrapped := false
		for _, inner := range m {
			switch inner.(type) {
			case []any, []map[string]any, map[string]any:
				val = inner
				unwrapped = true
			}
		}
		if !unwrapped {
			break
		}
	}

	switch x := val.(type) {
	case []map[string]any:
		anySlice := make([]any, len(x))
		for i := range x {
			anySlice[i] = x[i]
		}
		return sliceToRows(anySlice)
	case []any:
		return sliceToRows(x)
	case map[string]any:
		keys := sortedMapKeys(x)
		rows := [][]string{{"key", "value"}}
		for _, k := range keys {
			rows = append(rows, []string{k, stringifyCell(x[k])})
		}
		return rows
	default:
		return [][]string{{"value"}, {stringifyCell(val)}}
	}
}

func sliceToRows(items []any) [][]string {
	if len(items) == 0 {
		return nil
	}
	var colOrder []string
	colSeen := make(map[string]bool)
	allMaps := true
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			allMaps = false
			break
		}
		for _, k := range sortedMapKeys(m) {
			if !colSeen[k] {
				colSeen[k] = true
				colOrder = append(colOrder, k)
			}
		}
	}
	if !allMaps {
		rows := [][]string{{"value"}}
		for _, it := range items {
			rows = append(rows, []string{stringifyCell(it)})
		}
		return rows
	}
	rows := make([][]string, 0, len(items)+1)
	rows = append(rows, colOrder)
	for _, it := range items {
		m := it.(map[string]any)
		row := make([]string, len(colOrder))
		for i, k := range colOrder {
			row[i] = stringifyCell(m[k])
		}
		rows = append(rows, row)
	}
	return rows
}

func stringifyCell(v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case map[string]any, []any:
		b, err := json.Marshal(x)
		if err == nil {
			return string(b)
		}
		return fmt.Sprint(x)
	default:
		return fmt.Sprint(x)
	}
}

func sortedMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

type xmlNode struct {
	name     string
	attrs    []xml.Attr
	children []*xmlNode
	text     strings.Builder
}

func parseGenericXML(input string) (any, error) {
	dec := xml.NewDecoder(strings.NewReader(input))
	var stack []*xmlNode
	var root *xmlNode

	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("invalid XML: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			node := &xmlNode{name: t.Name.Local, attrs: t.Attr}
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, node)
			} else {
				root = node
			}
			stack = append(stack, node)
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].text.Write(t)
			}
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	if root == nil {
		return map[string]any{}, nil
	}
	return map[string]any{root.name: xmlNodeToValue(root)}, nil
}

func xmlNodeToValue(n *xmlNode) any {
	txt := strings.TrimSpace(n.text.String())
	if len(n.children) == 0 && len(n.attrs) == 0 {
		return inferScalar(txt)
	}
	m := make(map[string]any)
	for _, a := range n.attrs {
		m["@"+a.Name.Local] = inferScalar(a.Value)
	}
	for _, ch := range n.children {
		val := xmlNodeToValue(ch)
		if existing, exists := m[ch.name]; exists {
			if sl, ok := existing.([]any); ok {
				m[ch.name] = append(sl, val)
			} else {
				m[ch.name] = []any{existing, val}
			}
		} else {
			m[ch.name] = val
		}
	}
	if txt != "" {
		m["#text"] = inferScalar(txt)
	}
	return m
}

func writeXMLNode(buf *bytes.Buffer, tag string, val any, depth int) {
	tag = sanitizeXMLTag(tag)
	indent := strings.Repeat("  ", depth)

	switch x := val.(type) {
	case []any:
		for _, item := range x {
			writeXMLNode(buf, tag, item, depth)
		}
	case []map[string]any:
		if tag == "root" {
			buf.WriteString(indent + "<root>\n")
			for _, item := range x {
				writeXMLNode(buf, "item", item, depth+1)
			}
			buf.WriteString(indent + "</root>\n")
			return
		}
		for _, item := range x {
			writeXMLNode(buf, tag, item, depth)
		}
	case map[string]any:
		// Avoid double <root><root> when converting round-trip XML
		if depth == 0 && tag == "root" && len(x) == 1 {
			for k, v := range x {
				if _, isSlice := v.([]any); !isSlice {
					writeXMLNode(buf, k, v, 0)
					return
				}
			}
		}
		buf.WriteString(indent + "<" + tag)
		keys := sortedMapKeys(x)
		var elemKeys []string
		var bodyText string
		for _, k := range keys {
			if strings.HasPrefix(k, "@") && len(k) > 1 {
				attrName := sanitizeXMLTag(k[1:])
				buf.WriteString(fmt.Sprintf(` %s="%s"`, attrName, html.EscapeString(fmt.Sprint(x[k]))))
			} else if k == "#text" {
				bodyText = fmt.Sprint(x[k])
			} else {
				elemKeys = append(elemKeys, k)
			}
		}
		if len(elemKeys) == 0 && bodyText == "" {
			buf.WriteString("/>\n")
			return
		}
		if len(elemKeys) == 0 {
			buf.WriteString(">" + html.EscapeString(bodyText) + "</" + tag + ">\n")
			return
		}
		buf.WriteString(">\n")
		for _, k := range elemKeys {
			childTag := k
			if childTag == "items" {
				if sl, ok := x[k].([]any); ok {
					for _, it := range sl {
						writeXMLNode(buf, "item", it, depth+1)
					}
					continue
				}
			}
			writeXMLNode(buf, childTag, x[k], depth+1)
		}
		if bodyText != "" {
			buf.WriteString(strings.Repeat("  ", depth+1) + html.EscapeString(bodyText) + "\n")
		}
		buf.WriteString(indent + "</" + tag + ">\n")

	default:
		s := ""
		if x != nil {
			s = fmt.Sprint(x)
		}
		buf.WriteString(fmt.Sprintf("%s<%s>%s</%s>\n", indent, tag, html.EscapeString(s), tag))
	}
}

func sanitizeXMLTag(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "item"
	}
	var out []rune
	for i, r := range s {
		valid := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || r >= 0xC0 ||
			(i > 0 && ((r >= '0' && r <= '9') || r == '-' || r == '.'))
		if valid {
			out = append(out, r)
		} else if i == 0 && r >= '0' && r <= '9' {
			out = append(out, '_', r)
		} else {
			out = append(out, '_')
		}
	}
	res := string(out)
	if res == "" || strings.HasPrefix(strings.ToLower(res), "xml") {
		return "n_" + res
	}
	return res
}
