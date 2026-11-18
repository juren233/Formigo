package converter

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	gmhtml "github.com/yuin/goldmark/renderer/html"
	nethtml "golang.org/x/net/html"
)

func convertDocumentOrEbookNative(ctx context.Context, inPath, srcExt, outPath, dstExt, _ string, opts Options) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, ErrCanceled
	}

	title := safeStem(inPath)
	var mdBody string
	var warnings []string

	// Normalize any document/ebook input into rich CommonMark + GFM Markdown AST representation first.
	switch srcExt {
	case "md":
		txt, _, err := ReadTextFile(inPath, opts.TextEncoding)
		if err != nil {
			return nil, err
		}
		mdBody = txt

	case "txt", "log":
		txt, _, err := ReadTextFile(inPath, opts.TextEncoding)
		if err != nil {
			return nil, err
		}
		if dstExt == "txt" {
			return nil, os.WriteFile(outPath, []byte(txt), 0o644)
		}
		mdBody = plainTextToMarkdown(title, txt)

	case "html":
		rawHTML, _, err := ReadTextFile(inPath, opts.TextEncoding)
		if err != nil {
			return nil, err
		}
		extractedTitle, convMD, err := convertHTMLToMarkdown(rawHTML)
		if err != nil {
			return nil, err
		}
		if extractedTitle != "" {
			title = extractedTitle
		}
		mdBody = convMD

	case "docx":
		convMD, err := extractDOCXToMarkdown(inPath)
		if err != nil {
			return nil, err
		}
		mdBody = convMD

	case "epub":
		bookTitle, convMD, w, err := extractEPUBToMarkdown(inPath)
		if err != nil {
			return nil, err
		}
		if bookTitle != "" {
			title = bookTitle
		}
		warnings = append(warnings, w...)
		mdBody = convMD

	default:
		return nil, fmt.Errorf("unsupported native document input: .%s", srcExt)
	}

	// Render normalized Markdown to target format.
	switch dstExt {
	case "md":
		return warnings, os.WriteFile(outPath, []byte(strings.TrimSpace(mdBody)+"\n"), 0o644)

	case "txt":
		plain := markdownToPlainText(mdBody)
		return warnings, os.WriteFile(outPath, []byte(plain), 0o644)

	case "html":
		docHTML, err := renderMarkdownDocumentHTML(title, mdBody)
		if err != nil {
			return warnings, err
		}
		return warnings, os.WriteFile(outPath, []byte(docHTML), 0o644)

	case "docx":
		return warnings, writeMarkdownToDOCX(title, mdBody, outPath)

	case "epub":
		return warnings, writeMarkdownToEPUB(title, mdBody, outPath)

	case "pdf":
		plain := markdownToPlainText(mdBody)
		return warnings, writeUnicodeTextPDF(title, plain, outPath)

	default:
		return warnings, fmt.Errorf("unsupported document target format: .%s", dstExt)
	}
}

func plainTextToMarkdown(title, txt string) string {
	var sb strings.Builder
	sb.WriteString("# ")
	sb.WriteString(title)
	sb.WriteString("\n\n")
	lines := strings.Split(txt, "\n")
	for _, l := range lines {
		sb.WriteString(l)
		sb.WriteByte('\n')
	}
	return sb.String()
}

func renderMarkdownFragmentHTML(md string) (string, error) {
	gm := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithRendererOptions(gmhtml.WithUnsafe()),
	)
	var buf bytes.Buffer
	if err := gm.Convert([]byte(md), &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func renderMarkdownDocumentHTML(title, md string) (string, error) {
	body, err := renderMarkdownFragmentHTML(md)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>%s</title>
<style>
  body { max-width: 820px; margin: 2.5rem auto; padding: 0 1.25rem; font-family: system-ui, -apple-system, "PingFang SC", "Microsoft YaHei", sans-serif; line-height: 1.7; color: #1f2937; }
  h1, h2, h3, h4 { color: #111827; margin-top: 1.6em; line-height: 1.3; }
  pre { background: #f3f4f6; padding: 14px 16px; border-radius: 8px; overflow-x: auto; font-size: 0.9em; }
  code { background: #f3f4f6; padding: 2px 5px; border-radius: 4px; font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
  pre code { background: none; padding: 0; }
  blockquote { margin: 1em 0; padding: 0.25em 1em; border-left: 4px solid #6366f1; color: #4b5563; background: #f9fafb; }
  table { border-collapse: collapse; width: 100%%; margin: 1.2em 0; }
  th, td { border: 1px solid #d1d5db; padding: 8px 12px; text-align: left; }
  th { background: #f3f4f6; }
  img { max-width: 100%%; height: auto; }
</style>
</head>
<body>
%s
</body>
</html>
`, html.EscapeString(title), body), nil
}

// convertHTMLToMarkdown walks an HTML5 parse tree and emits clean GFM Markdown.
func convertHTMLToMarkdown(rawHTML string) (string, string, error) {
	doc, err := nethtml.Parse(strings.NewReader(rawHTML))
	if err != nil {
		return "", "", err
	}
	var title string
	var sb strings.Builder
	var walk func(*nethtml.Node, int)
	walk = func(n *nethtml.Node, listDepth int) {
		if n.Type == nethtml.TextNode {
			txt := n.Data
			if strings.TrimSpace(txt) == "" {
				if strings.ContainsAny(txt, " \t") && sb.Len() > 0 {
					s := sb.String()
					last := s[len(s)-1]
					if last != ' ' && last != '\n' {
						sb.WriteByte(' ')
					}
				}
				return
			}
			txt = strings.Join(strings.Fields(txt), " ")
			sb.WriteString(txt)
			return
		}
		if n.Type != nethtml.ElementNode {
			for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
				walk(ch, listDepth)
			}
			return
		}

		tag := strings.ToLower(n.Data)
		switch tag {
		case "script", "style", "noscript", "svg", "template":
			return
		case "title":
			if title == "" {
				title = strings.TrimSpace(extractHTMLNodeText(n))
			}
			return
		case "head":
			for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
				if ch.Type == nethtml.ElementNode && strings.EqualFold(ch.Data, "title") {
					walk(ch, listDepth)
				}
			}
			return
		case "h1", "h2", "h3", "h4", "h5", "h6":
			lvl := int(tag[1] - '0')
			ensureBlankLine(&sb)
			sb.WriteString(strings.Repeat("#", lvl) + " " + strings.TrimSpace(extractHTMLNodeText(n)) + "\n\n")
			return
		case "p", "div", "section", "article", "header", "footer", "main":
			isBlock := tag == "p"
			if isBlock {
				ensureBlankLine(&sb)
			}
			for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
				walk(ch, listDepth)
			}
			if isBlock {
				ensureBlankLine(&sb)
			}
			return
		case "br":
			sb.WriteString("  \n")
			return
		case "hr":
			ensureBlankLine(&sb)
			sb.WriteString("---\n\n")
			return
		case "strong", "b":
			t := strings.TrimSpace(extractHTMLNodeText(n))
			if t != "" {
				sb.WriteString("**" + t + "**")
			}
			return
		case "em", "i":
			t := strings.TrimSpace(extractHTMLNodeText(n))
			if t != "" {
				sb.WriteString("*" + t + "*")
			}
			return
		case "code":
			if n.Parent != nil && strings.EqualFold(n.Parent.Data, "pre") {
				sb.WriteString(extractHTMLNodeRawText(n))
				return
			}
			sb.WriteString("`" + strings.TrimSpace(extractHTMLNodeRawText(n)) + "`")
			return
		case "pre":
			ensureBlankLine(&sb)
			code := strings.TrimRight(extractHTMLNodeRawText(n), "\r\n")
			sb.WriteString("```\n" + code + "\n```\n\n")
			return
		case "blockquote":
			ensureBlankLine(&sb)
			inner := strings.TrimSpace(extractHTMLNodeText(n))
			for _, line := range strings.Split(inner, "\n") {
				sb.WriteString("> " + strings.TrimSpace(line) + "\n")
			}
			sb.WriteByte('\n')
			return
		case "a":
			href := getHTMLAttr(n, "href")
			label := strings.TrimSpace(extractHTMLNodeText(n))
			if label == "" {
				label = href
			}
			if href != "" && !strings.HasPrefix(strings.ToLower(href), "javascript:") {
				sb.WriteString(fmt.Sprintf("[%s](%s)", label, href))
			} else {
				sb.WriteString(label)
			}
			return
		case "ul", "ol":
			if listDepth == 0 {
				ensureBlankLine(&sb)
			} else if !strings.HasSuffix(sb.String(), "\n") {
				sb.WriteByte('\n')
			}
			idx := 1
			for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
				if ch.Type == nethtml.ElementNode && strings.EqualFold(ch.Data, "li") {
					indent := strings.Repeat("  ", listDepth)
					bullet := "- "
					if tag == "ol" {
						bullet = fmt.Sprintf("%d. ", idx)
						idx++
					}
					sb.WriteString(indent + bullet)
					for liCh := ch.FirstChild; liCh != nil; liCh = liCh.NextSibling {
						walk(liCh, listDepth+1)
					}
					if !strings.HasSuffix(sb.String(), "\n") {
						sb.WriteByte('\n')
					}
				}
			}
			if listDepth == 0 {
				sb.WriteByte('\n')
			}
			return
		case "table":
			rows := extractHTMLTableRows(n)
			if len(rows) > 0 {
				ensureBlankLine(&sb)
				sb.WriteString(renderRowsMarkdown(rows))
				sb.WriteByte('\n')
			}
			return
		}

		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch, listDepth)
		}
	}
	walk(doc, 0)
	return title, strings.TrimSpace(sb.String()), nil
}

func ensureBlankLine(sb *strings.Builder) {
	s := sb.String()
	if len(s) == 0 {
		return
	}
	if strings.HasSuffix(s, "\n\n") {
		return
	}
	if strings.HasSuffix(s, "\n") {
		sb.WriteByte('\n')
		return
	}
	sb.WriteString("\n\n")
}

func extractHTMLNodeText(n *nethtml.Node) string {
	var sb strings.Builder
	var rec func(*nethtml.Node)
	rec = func(cur *nethtml.Node) {
		if cur.Type == nethtml.TextNode {
			sb.WriteString(cur.Data)
			return
		}
		if cur.Type == nethtml.ElementNode && strings.EqualFold(cur.Data, "br") {
			sb.WriteByte('\n')
			return
		}
		for ch := cur.FirstChild; ch != nil; ch = ch.NextSibling {
			rec(ch)
		}
	}
	rec(n)
	return sb.String()
}

func extractHTMLNodeRawText(n *nethtml.Node) string {
	var sb strings.Builder
	var rec func(*nethtml.Node)
	rec = func(cur *nethtml.Node) {
		if cur.Type == nethtml.TextNode {
			sb.WriteString(cur.Data)
			return
		}
		for ch := cur.FirstChild; ch != nil; ch = ch.NextSibling {
			rec(ch)
		}
	}
	rec(n)
	return sb.String()
}

func getHTMLAttr(n *nethtml.Node, name string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, name) {
			return a.Val
		}
	}
	return ""
}

func extractHTMLTableRows(tableNode *nethtml.Node) [][]string {
	var rows [][]string
	var scan func(*nethtml.Node)
	scan = func(n *nethtml.Node) {
		if n.Type == nethtml.ElementNode && strings.EqualFold(n.Data, "tr") {
			var row []string
			for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
				if ch.Type == nethtml.ElementNode && (strings.EqualFold(ch.Data, "td") || strings.EqualFold(ch.Data, "th")) {
					row = append(row, strings.TrimSpace(extractHTMLNodeText(ch)))
				}
			}
			if len(row) > 0 {
				rows = append(rows, row)
			}
			return
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			scan(ch)
		}
	}
	scan(tableNode)
	return normalizeRowWidths(rows)
}

func markdownToPlainText(md string) string {
	renderedHTML, err := renderMarkdownFragmentHTML(md)
	if err != nil {
		return strings.TrimSpace(md) + "\n"
	}
	doc, err := nethtml.Parse(strings.NewReader(renderedHTML))
	if err != nil {
		return strings.TrimSpace(md) + "\n"
	}
	var sb strings.Builder
	var walk func(*nethtml.Node)
	walk = func(n *nethtml.Node) {
		if n.Type == nethtml.TextNode {
			sb.WriteString(n.Data)
			return
		}
		if n.Type == nethtml.ElementNode {
			tag := strings.ToLower(n.Data)
			if tag == "li" {
				sb.WriteString("• ")
			}
			for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
				walk(ch)
			}
			switch tag {
			case "p", "h1", "h2", "h3", "h4", "h5", "h6", "pre", "blockquote", "tr", "li", "div":
				if !strings.HasSuffix(sb.String(), "\n") {
					sb.WriteByte('\n')
				}
				if tag != "li" && tag != "tr" {
					sb.WriteByte('\n')
				}
			case "td", "th":
				sb.WriteByte('\t')
			case "br":
				sb.WriteByte('\n')
			}
			return
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(doc)
	lines := strings.Split(sb.String(), "\n")
	var out []string
	blanks := 0
	for _, l := range lines {
		t := strings.TrimRight(l, " \t")
		if t == "" {
			blanks++
			if blanks <= 1 && len(out) > 0 {
				out = append(out, "")
			}
			continue
		}
		blanks = 0
		out = append(out, t)
	}
	return strings.TrimSpace(strings.Join(out, "\n")) + "\n"
}

// --- Pure-Go DOCX Reader & Writer ---

func extractDOCXToMarkdown(path string) (string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return "", fmt.Errorf("cannot open DOCX archive: %w", err)
	}
	defer zr.Close()

	rels := make(map[string]string)
	var docXML []byte
	for _, f := range zr.File {
		switch f.Name {
		case "word/_rels/document.xml.rels":
			if b, err := readZipMember(f, 8*1024*1024); err == nil {
				rels = parseOOXMLRels(b)
			}
		case "word/document.xml":
			b, err := readZipMember(f, 64*1024*1024)
			if err != nil {
				return "", err
			}
			docXML = b
		}
	}
	if len(docXML) == 0 {
		return "", errors.New("word/document.xml not found in DOCX")
	}

	return parseWordDocumentXML(docXML, rels)
}

func readZipMember(f *zip.File, maxBytes int64) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, maxBytes))
}

func parseOOXMLRels(raw []byte) map[string]string {
	type relItem struct {
		ID     string `xml:"Id,attr"`
		Target string `xml:"Target,attr"`
	}
	type relRoot struct {
		Items []relItem `xml:"Relationship"`
	}
	var root relRoot
	_ = xml.Unmarshal(raw, &root)
	m := make(map[string]string, len(root.Items))
	for _, it := range root.Items {
		if it.ID != "" && it.Target != "" {
			m[it.ID] = it.Target
		}
	}
	return m
}

func parseWordDocumentXML(raw []byte, rels map[string]string) (string, error) {
	dec := xml.NewDecoder(bytes.NewReader(raw))
	var md strings.Builder

	var walkBody func() error
	walkBody = func() error {
		for {
			tok, err := dec.Token()
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return err
			}
			se, ok := tok.(xml.StartElement)
			if !ok {
				continue
			}
			switch se.Name.Local {
			case "p":
				para, err := readWordParagraph(dec, rels)
				if err != nil {
					return err
				}
				if strings.TrimSpace(para) != "" {
					md.WriteString(para)
					md.WriteString("\n\n")
				}
			case "tbl":
				rows, err := readWordTable(dec, rels)
				if err != nil {
					return err
				}
				if len(rows) > 0 {
					md.WriteString(renderRowsMarkdown(normalizeRowWidths(rows)))
					md.WriteByte('\n')
				}
			}
		}
	}
	if err := walkBody(); err != nil {
		return "", err
	}
	return strings.TrimSpace(md.String()), nil
}

func readWordParagraph(dec *xml.Decoder, rels map[string]string) (string, error) {
	headingLvl := 0
	isList := false
	var buf strings.Builder
	inRun := false
	runBold := false
	runItalic := false
	var runText strings.Builder
	var linkHref string

	flushRun := func() {
		txt := runText.String()
		runText.Reset()
		if txt == "" {
			return
		}
		if runBold && strings.TrimSpace(txt) != "" {
			txt = "**" + txt + "**"
		}
		if runItalic && strings.TrimSpace(txt) != "" {
			txt = "*" + txt + "*"
		}
		if linkHref != "" && strings.TrimSpace(txt) != "" {
			buf.WriteString(fmt.Sprintf("[%s](%s)", txt, linkHref))
		} else {
			buf.WriteString(txt)
		}
	}

	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			return buf.String(), err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			switch t.Name.Local {
			case "pStyle":
				val := xmlAttrVal(t.Attr, "val")
				low := strings.ToLower(val)
				if strings.HasPrefix(low, "heading") && len(low) >= 8 {
					if c := low[7]; c >= '1' && c <= '6' {
						headingLvl = int(c - '0')
					}
				}
			case "numPr":
				isList = true
			case "hyperlink":
				rid := xmlAttrVal(t.Attr, "id")
				linkHref = rels[rid]
			case "r":
				inRun = true
				runBold = false
				runItalic = false
			case "b":
				if inRun && xmlAttrVal(t.Attr, "val") != "0" && xmlAttrVal(t.Attr, "val") != "false" {
					runBold = true
				}
			case "i":
				if inRun && xmlAttrVal(t.Attr, "val") != "0" && xmlAttrVal(t.Attr, "val") != "false" {
					runItalic = true
				}
			case "br":
				runText.WriteByte('\n')
			case "tab":
				runText.WriteByte('\t')
			case "t":
				var content string
				if err := dec.DecodeElement(&content, &t); err == nil {
					runText.WriteString(content)
				}
				depth-- // DecodeElement consumed matching EndElement
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "r":
				flushRun()
				inRun = false
			case "hyperlink":
				linkHref = ""
			}
			depth--
		}
	}

	res := strings.TrimSpace(buf.String())
	if res == "" {
		return "", nil
	}
	if headingLvl > 0 {
		return strings.Repeat("#", headingLvl) + " " + res, nil
	}
	if isList {
		return "- " + res, nil
	}
	return res, nil
}

func readWordTable(dec *xml.Decoder, rels map[string]string) ([][]string, error) {
	var rows [][]string
	var curRow []string
	var curCell []string
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			return rows, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			switch t.Name.Local {
			case "tr":
				curRow = nil
			case "tc":
				curCell = nil
			case "p":
				pText, err := readWordParagraph(dec, rels)
				if err != nil {
					return rows, err
				}
				depth-- // readWordParagraph consumed </w:p>
				if pText != "" {
					curCell = append(curCell, pText)
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "tc":
				curRow = append(curRow, strings.Join(curCell, " "))
			case "tr":
				if len(curRow) > 0 {
					rows = append(rows, curRow)
				}
			}
			depth--
		}
	}
	return rows, nil
}

func xmlAttrVal(attrs []xml.Attr, local string) string {
	for _, a := range attrs {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

// writeMarkdownToDOCX builds a clean, standards-compliant Word OOXML (.docx) package from Markdown.
func writeMarkdownToDOCX(title, md, outPath string) error {
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	addEntry := func(name, content string) error {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = io.WriteString(w, content)
		return err
	}

	if err := addEntry("[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
  <Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>
  <Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/>
</Types>`); err != nil {
		return err
	}

	if err := addEntry("_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
  <Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/>
</Relationships>`); err != nil {
		return err
	}

	if err := addEntry("docProps/core.xml", fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
  <dc:title>%s</dc:title>
  <dc:creator>Fomigo</dc:creator>
  <dcterms:created xsi:type="dcterms:W3CDTF">%s</dcterms:created>
</cp:coreProperties>`, escapeXML(title), time.Now().UTC().Format(time.RFC3339))); err != nil {
		return err
	}

	if err := addEntry("word/styles.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:style w:type="paragraph" w:default="1" w:styleId="Normal">
    <w:name w:val="Normal"/>
    <w:pPr><w:spacing w:after="160" w:line="276" w:lineRule="auto"/></w:pPr>
    <w:rPr><w:sz w:val="22"/><w:szCs w:val="22"/></w:rPr>
  </w:style>
  <w:style w:type="paragraph" w:styleId="Heading1">
    <w:name w:val="heading 1"/><w:basedOn w:val="Normal"/>
    <w:pPr><w:spacing w:before="280" w:after="140"/><w:outlineLvl w:val="0"/></w:pPr>
    <w:rPr><w:b/><w:sz w:val="36"/><w:szCs w:val="36"/></w:rPr>
  </w:style>
  <w:style w:type="paragraph" w:styleId="Heading2">
    <w:name w:val="heading 2"/><w:basedOn w:val="Normal"/>
    <w:pPr><w:spacing w:before="240" w:after="120"/><w:outlineLvl w:val="1"/></w:pPr>
    <w:rPr><w:b/><w:sz w:val="30"/><w:szCs w:val="30"/></w:rPr>
  </w:style>
  <w:style w:type="paragraph" w:styleId="Heading3">
    <w:name w:val="heading 3"/><w:basedOn w:val="Normal"/>
    <w:pPr><w:spacing w:before="200" w:after="100"/><w:outlineLvl w:val="2"/></w:pPr>
    <w:rPr><w:b/><w:sz w:val="26"/><w:szCs w:val="26"/></w:rPr>
  </w:style>
</w:styles>`); err != nil {
		return err
	}

	if err := addEntry("word/_rels/document.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rIdStyles" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
</Relationships>`); err != nil {
		return err
	}

	bodyXML := buildDOCXBodyFromMarkdown(md)
	docXML := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>` + bodyXML + `<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440"/></w:sectPr></w:body>
</w:document>`

	if err := addEntry("word/document.xml", docXML); err != nil {
		return err
	}
	return zw.Close()
}

func buildDOCXBodyFromMarkdown(md string) string {
	var body strings.Builder
	lines := strings.Split(normalizeNewlines(md), "\n")
	inCode := false
	var codeLines []string
	var tableRows [][]string

	flushTable := func() {
		if len(tableRows) == 0 {
			return
		}
		norm := normalizeRowWidths(tableRows)
		tableRows = nil
		if len(norm) == 0 {
			return
		}
		body.WriteString(`<w:tbl><w:tblPr><w:tblBorders><w:top w:val="single" w:sz="4" w:color="D1D5DB"/><w:left w:val="single" w:sz="4" w:color="D1D5DB"/><w:bottom w:val="single" w:sz="4" w:color="D1D5DB"/><w:right w:val="single" w:sz="4" w:color="D1D5DB"/><w:insideH w:val="single" w:sz="4" w:color="D1D5DB"/><w:insideV w:val="single" w:sz="4" w:color="D1D5DB"/></w:tblBorders></w:tblPr>`)
		for rIdx, row := range norm {
			body.WriteString(`<w:tr>`)
			for _, cell := range row {
				body.WriteString(`<w:tc><w:p>`)
				if rIdx == 0 {
					body.WriteString(`<w:r><w:rPr><w:b/></w:rPr><w:t xml:space="preserve">` + escapeXML(cell) + `</w:t></w:r>`)
				} else {
					body.WriteString(renderDOCXInlineRuns(cell))
				}
				body.WriteString(`</w:p></w:tc>`)
			}
			body.WriteString(`</w:tr>`)
		}
		body.WriteString(`</w:tbl>`)
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			flushTable()
			if inCode {
				for _, cl := range codeLines {
					body.WriteString(`<w:p><w:pPr><w:shd w:val="clear" w:fill="F3F4F6"/><w:spacing w:after="40"/></w:pPr><w:r><w:rPr><w:rFonts w:ascii="Courier New" w:hAnsi="Courier New"/><w:sz w:val="19"/></w:rPr><w:t xml:space="preserve">` + escapeXML(cl) + `</w:t></w:r></w:p>`)
				}
				codeLines = nil
				inCode = false
			} else {
				inCode = true
			}
			continue
		}
		if inCode {
			codeLines = append(codeLines, line)
			continue
		}

		// Markdown pipe table detection
		if strings.HasPrefix(trimmed, "|") && strings.HasSuffix(trimmed, "|") && len(trimmed) > 2 {
			cells := splitMarkdownTableLine(trimmed)
			if !isMarkdownSeparatorRow(cells) {
				tableRows = append(tableRows, cells)
			}
			continue
		}
		flushTable()

		if trimmed == "" {
			continue
		}

		if strings.HasPrefix(trimmed, "#") {
			lvl := 0
			for lvl < len(trimmed) && trimmed[lvl] == '#' {
				lvl++
			}
			if lvl >= 1 && lvl <= 6 && lvl < len(trimmed) && trimmed[lvl] == ' ' {
				style := "Heading3"
				if lvl == 1 {
					style = "Heading1"
				} else if lvl == 2 {
					style = "Heading2"
				}
				body.WriteString(fmt.Sprintf(`<w:p><w:pPr><w:pStyle w:val="%s"/></w:pPr>%s</w:p>`, style, renderDOCXInlineRuns(strings.TrimSpace(trimmed[lvl+1:]))))
				continue
			}
		}

		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			body.WriteString(`<w:p><w:pPr><w:ind w:left="360"/></w:pPr><w:r><w:t xml:space="preserve">• </w:t></w:r>` + renderDOCXInlineRuns(strings.TrimSpace(trimmed[2:])) + `</w:p>`)
			continue
		}

		if strings.HasPrefix(trimmed, "> ") {
			body.WriteString(`<w:p><w:pPr><w:ind w:left="420"/></w:pPr>` + renderDOCXInlineRuns(strings.TrimSpace(trimmed[2:])) + `</w:p>`)
			continue
		}

		body.WriteString(`<w:p>` + renderDOCXInlineRuns(trimmed) + `</w:p>`)
	}
	flushTable()
	return body.String()
}

func splitMarkdownTableLine(line string) []string {
	inner := strings.TrimPrefix(strings.TrimSuffix(line, "|"), "|")
	parts := strings.Split(inner, "|")
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = strings.TrimSpace(strings.ReplaceAll(p, `\|`, "|"))
	}
	return out
}

func isMarkdownSeparatorRow(cells []string) bool {
	if len(cells) == 0 {
		return false
	}
	for _, c := range cells {
		clean := strings.Trim(c, ":- ")
		if clean != "" || !strings.Contains(c, "-") {
			return false
		}
	}
	return true
}

func renderDOCXInlineRuns(s string) string {
	var out strings.Builder
	i := 0
	for i < len(s) {
		if strings.HasPrefix(s[i:], "**") {
			if end := strings.Index(s[i+2:], "**"); end > 0 {
				out.WriteString(`<w:r><w:rPr><w:b/></w:rPr><w:t xml:space="preserve">` + escapeXML(s[i+2:i+2+end]) + `</w:t></w:r>`)
				i += 4 + end
				continue
			}
		}
		if s[i] == '`' {
			if end := strings.IndexByte(s[i+1:], '`'); end > 0 {
				out.WriteString(`<w:r><w:rPr><w:rFonts w:ascii="Courier New" w:hAnsi="Courier New"/><w:shd w:val="clear" w:fill="F3F4F6"/></w:rPr><w:t xml:space="preserve">` + escapeXML(s[i+1:i+1+end]) + `</w:t></w:r>`)
				i += 2 + end
				continue
			}
		}
		if s[i] == '*' {
			if end := strings.IndexByte(s[i+1:], '*'); end > 0 {
				out.WriteString(`<w:r><w:rPr><w:i/></w:rPr><w:t xml:space="preserve">` + escapeXML(s[i+1:i+1+end]) + `</w:t></w:r>`)
				i += 2 + end
				continue
			}
		}
		nextSpecial := len(s)
		for k := i + 1; k < len(s); k++ {
			if s[k] == '*' || s[k] == '`' {
				nextSpecial = k
				break
			}
		}
		out.WriteString(`<w:r><w:t xml:space="preserve">` + escapeXML(s[i:nextSpecial]) + `</w:t></w:r>`)
		i = nextSpecial
	}
	return out.String()
}

func escapeXML(s string) string {
	var buf bytes.Buffer
	for _, r := range s {
		if r == 0x9 || r == 0xA || r == 0xD || (r >= 0x20 && r <= 0xD7FF) || (r >= 0xE000 && r <= 0xFFFD) || r >= 0x10000 {
			buf.WriteRune(r)
		}
	}
	var esc bytes.Buffer
	_ = xml.EscapeText(&esc, buf.Bytes())
	return esc.String()
}

// --- Pure-Go EPUB Reader & Writer ---

func extractEPUBToMarkdown(epubPath string) (string, string, []string, error) {
	zr, err := zip.OpenReader(epubPath)
	if err != nil {
		return "", "", nil, fmt.Errorf("cannot open EPUB: %w", err)
	}
	defer zr.Close()

	files := make(map[string]*zip.File, len(zr.File))
	for _, f := range zr.File {
		files[f.Name] = f
	}

	// Check for DRM encryption
	if _, hasEnc := files["META-INF/encryption.xml"]; hasEnc {
		return "", "", nil, errors.New("encrypted DRM EPUB files are not supported")
	}

	containerFile, ok := files["META-INF/container.xml"]
	if !ok {
		return "", "", nil, errors.New("invalid EPUB: missing META-INF/container.xml")
	}
	containerXML, err := readZipMember(containerFile, 1024*1024)
	if err != nil {
		return "", "", nil, err
	}

	type rootFile struct {
		FullPath string `xml:"full-path,attr"`
	}
	type containerDoc struct {
		RootFiles []rootFile `xml:"rootfiles>rootfile"`
	}
	var cdoc containerDoc
	if err := xml.Unmarshal(containerXML, &cdoc); err != nil || len(cdoc.RootFiles) == 0 {
		return "", "", nil, errors.New("invalid EPUB container.xml")
	}

	opfPath := cdoc.RootFiles[0].FullPath
	opfFile, ok := files[opfPath]
	if !ok {
		return "", "", nil, fmt.Errorf("missing EPUB package file: %s", opfPath)
	}
	opfXML, err := readZipMember(opfFile, 8*1024*1024)
	if err != nil {
		return "", "", nil, err
	}

	type manifestItem struct {
		ID        string `xml:"id,attr"`
		Href      string `xml:"href,attr"`
		MediaType string `xml:"media-type,attr"`
	}
	type itemRef struct {
		IDRef string `xml:"idref,attr"`
	}
	type opfPackage struct {
		Titles   []string       `xml:"metadata>title"`
		Manifest []manifestItem `xml:"manifest>item"`
		Spine    []itemRef      `xml:"spine>itemref"`
	}
	var pkg opfPackage
	if err := xml.Unmarshal(opfXML, &pkg); err != nil {
		return "", "", nil, fmt.Errorf("invalid EPUB OPF metadata: %w", err)
	}

	bookTitle := ""
	if len(pkg.Titles) > 0 {
		bookTitle = strings.TrimSpace(pkg.Titles[0])
	}

	opfDir := path.Dir(opfPath)
	if opfDir == "." {
		opfDir = ""
	}

	manifest := make(map[string]manifestItem, len(pkg.Manifest))
	for _, it := range pkg.Manifest {
		manifest[it.ID] = it
	}

	var chapters []string
	var warnings []string
	for _, ref := range pkg.Spine {
		item, exists := manifest[ref.IDRef]
		if !exists {
			continue
		}
		if !strings.Contains(item.MediaType, "html") && !strings.Contains(item.MediaType, "xml") {
			continue
		}
		decodedHref, _ := url.PathUnescape(item.Href)
		if decodedHref == "" {
			decodedHref = item.Href
		}
		if idx := strings.IndexByte(decodedHref, '#'); idx >= 0 {
			decodedHref = decodedHref[:idx]
		}
		memberPath := decodedHref
		if opfDir != "" {
			memberPath = path.Clean(path.Join(opfDir, decodedHref))
		}
		zf, exists := files[memberPath]
		if !exists {
			warnings = append(warnings, "Missing spine chapter: "+memberPath)
			continue
		}
		rawChap, err := readZipMember(zf, 16*1024*1024)
		if err != nil {
			continue
		}
		_, chapMD, err := convertHTMLToMarkdown(string(rawChap))
		if err == nil && strings.TrimSpace(chapMD) != "" {
			chapters = append(chapters, chapMD)
		}
	}

	// Fallback if spine was empty: read all HTML/XHTML files in sorted order.
	if len(chapters) == 0 {
		var names []string
		for name := range files {
			low := strings.ToLower(name)
			if (strings.HasSuffix(low, ".xhtml") || strings.HasSuffix(low, ".html") || strings.HasSuffix(low, ".htm")) &&
				!strings.HasSuffix(low, "nav.xhtml") {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		for _, name := range names {
			rawChap, err := readZipMember(files[name], 16*1024*1024)
			if err == nil {
				if _, chapMD, err := convertHTMLToMarkdown(string(rawChap)); err == nil && strings.TrimSpace(chapMD) != "" {
					chapters = append(chapters, chapMD)
				}
			}
		}
	}

	if len(chapters) == 0 {
		return bookTitle, "", warnings, errors.New("EPUB contains no readable text chapters")
	}

	fullMD := strings.Join(chapters, "\n\n---\n\n")
	if bookTitle != "" && !strings.HasPrefix(strings.TrimSpace(fullMD), "# ") {
		fullMD = "# " + bookTitle + "\n\n" + fullMD
	}
	return bookTitle, fullMD, warnings, nil
}

// writeMarkdownToEPUB generates an EPUB 3.0 compliant eBook from Markdown.
func writeMarkdownToEPUB(title, md, outPath string) error {
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()

	zw := zip.NewWriter(f)

	// EPUB spec requires "mimetype" as the very first entry, stored uncompressed!
	mimeHdr := &zip.FileHeader{
		Name:   "mimetype",
		Method: zip.Store,
	}
	mw, err := zw.CreateHeader(mimeHdr)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(mw, "application/epub+zip"); err != nil {
		return err
	}

	addDeflated := func(name, content string) error {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = io.WriteString(w, content)
		return err
	}

	if err := addDeflated("META-INF/container.xml", `<?xml version="1.0" encoding="UTF-8"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`); err != nil {
		return err
	}

	if err := addDeflated("OEBPS/style.css", `body { font-family: serif; line-height: 1.7; margin: 1em; color: #1f2937; }
h1, h2, h3 { font-family: sans-serif; line-height: 1.3; margin-top: 1.4em; }
pre, code { font-family: monospace; background: #f3f4f6; }
pre { padding: 0.75em; overflow-x: auto; }
table { border-collapse: collapse; width: 100%; margin: 1em 0; }
th, td { border: 1px solid #ccc; padding: 6px 10px; }`); err != nil {
		return err
	}

	bodyHTML, err := renderMarkdownFragmentHTML(md)
	if err != nil {
		return err
	}
	xhtmlBody := htmlFragmentToXHTML(bodyHTML)

	chapterXHTML := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml" xml:lang="zh-CN">
<head>
  <meta charset="utf-8"/>
  <title>%s</title>
  <link rel="stylesheet" type="text/css" href="style.css"/>
</head>
<body>
%s
</body>
</html>`, escapeXML(title), xhtmlBody)

	if err := addDeflated("OEBPS/chapter1.xhtml", chapterXHTML); err != nil {
		return err
	}

	navXHTML := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops" xml:lang="zh-CN">
<head><meta charset="utf-8"/><title>%s</title></head>
<body>
  <nav epub:type="toc" id="toc">
    <h1>%s</h1>
    <ol><li><a href="chapter1.xhtml">%s</a></li></ol>
  </nav>
</body>
</html>`, escapeXML(title), escapeXML(title), escapeXML(title))

	if err := addDeflated("OEBPS/nav.xhtml", navXHTML); err != nil {
		return err
	}

	uid := fmt.Sprintf("urn:formigo:book:%d", time.Now().UnixNano())
	modified := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	opf := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="bookid">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:identifier id="bookid">%s</dc:identifier>
    <dc:title>%s</dc:title>
    <dc:language>zh-CN</dc:language>
    <meta property="dcterms:modified">%s</meta>
  </metadata>
  <manifest>
    <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
    <item id="css" href="style.css" media-type="text/css"/>
    <item id="chap1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine>
    <itemref idref="chap1"/>
  </spine>
</package>`, escapeXML(uid), escapeXML(title), modified)

	if err := addDeflated("OEBPS/content.opf", opf); err != nil {
		return err
	}
	return zw.Close()
}

func htmlFragmentToXHTML(fragment string) string {
	doc, err := nethtml.Parse(strings.NewReader("<html><body>" + fragment + "</body></html>"))
	if err != nil {
		return "<p>" + escapeXML(fragment) + "</p>"
	}
	var body *nethtml.Node
	var findBody func(*nethtml.Node)
	findBody = func(n *nethtml.Node) {
		if n.Type == nethtml.ElementNode && n.Data == "body" {
			body = n
			return
		}
		for ch := n.FirstChild; ch != nil && body == nil; ch = ch.NextSibling {
			findBody(ch)
		}
	}
	findBody(doc)
	if body == nil {
		return "<p>" + escapeXML(fragment) + "</p>"
	}
	var out bytes.Buffer
	for ch := body.FirstChild; ch != nil; ch = ch.NextSibling {
		renderXHTMLNode(&out, ch)
	}
	return out.String()
}

var voidHTMLTags = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true,
	"hr": true, "img": true, "input": true, "link": true, "meta": true,
	"param": true, "source": true, "track": true, "wbr": true,
}

func renderXHTMLNode(buf *bytes.Buffer, n *nethtml.Node) {
	switch n.Type {
	case nethtml.TextNode:
		buf.WriteString(escapeXML(n.Data))
	case nethtml.ElementNode:
		tag := strings.ToLower(n.Data)
		buf.WriteByte('<')
		buf.WriteString(tag)
		for _, a := range n.Attr {
			buf.WriteByte(' ')
			buf.WriteString(a.Key)
			buf.WriteString(`="`)
			buf.WriteString(escapeXML(a.Val))
			buf.WriteByte('"')
		}
		if voidHTMLTags[tag] {
			buf.WriteString("/>")
			return
		}
		buf.WriteByte('>')
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			renderXHTMLNode(buf, ch)
		}
		buf.WriteString("</")
		buf.WriteString(tag)
		buf.WriteByte('>')
	}
}

// --- Pure-Go Vector Unicode/CJK PDF Writer (Type0 CIDFont UniGB-UTF16-H) ---

func writeUnicodeTextPDF(title, text, outPath string) error {
	wrappedLines := wrapDocumentLines(text, 76)
	if len(wrappedLines) == 0 {
		wrappedLines = []string{title}
	}

	const linesPerPage = 44
	var pages [][]string
	for i := 0; i < len(wrappedLines); i += linesPerPage {
		end := i + linesPerPage
		if end > len(wrappedLines) {
			end = len(wrappedLines)
		}
		pages = append(pages, wrappedLines[i:end])
	}

	// Object layout:
	// 1: Catalog
	// 2: Pages
	// 3: Type0 Font (STSong-Light / UniGB-UTF16-H — supports full UTF-16BE BMP including Chinese & ASCII!)
	// 4: CIDFontType0 descendant
	// 5: FontDescriptor
	// 6 + 2*p: Page p
	// 7 + 2*p: Contents stream for Page p
	totalObjs := 5 + 2*len(pages)
	offsets := make([]int, totalObjs+1)
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.5\n%\xE2\xE3\xCF\xD3\n")

	writeObj := func(num int, body string) {
		offsets[num] = pdf.Len()
		pdf.WriteString(fmt.Sprintf("%d 0 obj\n%s\nendobj\n", num, body))
	}

	var kids []string
	for p := range pages {
		kids = append(kids, fmt.Sprintf("%d 0 R", 6+2*p))
	}

	writeObj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	writeObj(2, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(pages)))
	writeObj(3, "<< /Type /Font /Subtype /Type0 /BaseFont /STSong-Light /Encoding /UniGB-UTF16-H /DescendantFonts [4 0 R] >>")
	writeObj(4, "<< /Type /Font /Subtype /CIDFontType0 /BaseFont /STSong-Light /CIDSystemInfo << /Registry (Adobe) /Ordering (GB1) /Supplement 4 >> /FontDescriptor 5 0 R /DW 1000 >>")
	writeObj(5, "<< /Type /FontDescriptor /FontName /STSong-Light /Flags 4 /FontBBox [-250 -143 600 857] /ItalicAngle 0 /Ascent 857 /Descent -143 /CapHeight 857 /StemV 91 >>")

	for p, pLines := range pages {
		pageObj := 6 + 2*p
		streamObj := 7 + 2*p

		var content bytes.Buffer
		content.WriteString("BT\n/F1 11 Tf\n16 TL\n52 786 Td\n")
		for idx, line := range pLines {
			if idx > 0 {
				content.WriteString("T*\n")
			}
			hexUTF16 := encodePDFUTF16Hex(line)
			content.WriteString("<" + hexUTF16 + "> Tj\n")
		}
		content.WriteString("ET\n")
		streamBytes := content.Bytes()

		writeObj(pageObj, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>", streamObj))
		writeObj(streamObj, fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(streamBytes), string(streamBytes)))
	}

	xrefPos := pdf.Len()
	pdf.WriteString(fmt.Sprintf("xref\n0 %d\n0000000000 65535 f \n", totalObjs+1))
	for i := 1; i <= totalObjs; i++ {
		pdf.WriteString(fmt.Sprintf("%010d 00000 n \n", offsets[i]))
	}
	pdf.WriteString(fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", totalObjs+1, xrefPos))

	return os.WriteFile(outPath, pdf.Bytes(), 0o644)
}

func encodePDFUTF16Hex(s string) string {
	u16 := utf16.Encode([]rune(s))
	raw := make([]byte, 2+len(u16)*2)
	raw[0] = 0xFE
	raw[1] = 0xFF
	for i, v := range u16 {
		raw[2+i*2] = byte(v >> 8)
		raw[2+i*2+1] = byte(v)
	}
	return strings.ToUpper(hex.EncodeToString(raw))
}

func wrapDocumentLines(text string, maxCols int) []string {
	var out []string
	for _, rawLine := range strings.Split(normalizeNewlines(text), "\n") {
		if rawLine == "" {
			out = append(out, "")
			continue
		}
		col := 0
		var cur strings.Builder
		for _, r := range rawLine {
			w := 1
			if r > 0x7F {
				w = 2
			}
			if col+w > maxCols && cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
				col = 0
			}
			cur.WriteRune(r)
			col += w
		}
		if cur.Len() > 0 {
			out = append(out, cur.String())
		}
	}
	return out
}

// --- External LibreOffice & Pandoc Document Adapters ---

func (p *Pipeline) convertViaLibreOffice(ctx context.Context, inPath, _, outPath, dstExt, workDir string) ([]string, error) {
	soffice := p.rt.Path(EngineLibreOffice)
	if soffice == "" {
		return nil, errors.New("LibreOffice (soffice) is not installed")
	}

	profileDir := filepath.Join(workDir, "lo-profile")
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		return nil, err
	}
	loOutDir := filepath.Join(workDir, "lo-out")
	if err := os.MkdirAll(loOutDir, 0o755); err != nil {
		return nil, err
	}

	profileURI := "file:///" + strings.TrimPrefix(filepath.ToSlash(profileDir), "/")
	args := []string{
		"-env:UserInstallation=" + profileURI,
		"--headless",
		"--nologo",
		"--nofirststartwizard",
		"--norestore",
		"--convert-to", dstExt,
		"--outdir", loOutDir,
		inPath,
	}
	_, _, err := RunCommand(ctx, soffice, args, nil)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(loOutDir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(NormalizeExt(e.Name()), dstExt) {
			return nil, deliverAtomically(filepath.Join(loOutDir, e.Name()), outPath)
		}
	}
	return nil, fmt.Errorf("LibreOffice finished without producing a .%s file", dstExt)
}

func (p *Pipeline) convertViaPandoc(ctx context.Context, inPath, _, outPath, _ string) ([]string, error) {
	pandoc := p.rt.Path(EnginePandoc)
	if pandoc == "" {
		return nil, errors.New("Pandoc is not installed")
	}
	args := []string{inPath, "-o", outPath, "--standalone"}
	_, _, err := RunCommand(ctx, pandoc, args, nil)
	return nil, err
}
