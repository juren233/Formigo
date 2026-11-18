package converter

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

// ReadTextFile reads a text file and decodes it cleanly into UTF-8, honoring explicit encoding or auto-detecting BOM / UTF-8 / GB18030.
func ReadTextFile(path string, hint string) (string, string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	text, detected, err := DecodeTextBytes(raw, hint)
	if err != nil {
		return "", "", err
	}
	return normalizeNewlines(text), detected, nil
}

// DecodeTextBytes converts raw bytes into UTF-8 and reports the detected/used encoding name.
func DecodeTextBytes(raw []byte, hint string) (string, string, error) {
	if len(raw) == 0 {
		return "", "utf-8", nil
	}
	h := strings.ToLower(strings.TrimSpace(hint))

	// Check BOMs first when auto or matching.
	if bytes.HasPrefix(raw, []byte{0xEF, 0xBB, 0xBF}) && (h == "" || h == "auto" || h == "utf-8") {
		return string(raw[3:]), "utf-8-bom", nil
	}
	if bytes.HasPrefix(raw, []byte{0xFF, 0xFE}) && (h == "" || h == "auto" || h == "utf-16" || h == "utf-16le") {
		s, err := decodeWith(raw[2:], unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM))
		return s, "utf-16le", err
	}
	if bytes.HasPrefix(raw, []byte{0xFE, 0xFF}) && (h == "" || h == "auto" || h == "utf-16" || h == "utf-16be") {
		s, err := decodeWith(raw[2:], unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM))
		return s, "utf-16be", err
	}

	switch h {
	case "utf-8", "utf8":
		return string(raw), "utf-8", nil
	case "gb18030", "gbk", "gb2312":
		s, err := decodeWith(raw, simplifiedchinese.GB18030)
		return s, "gb18030", err
	case "utf-16le":
		s, err := decodeWith(raw, unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM))
		return s, "utf-16le", err
	case "utf-16be":
		s, err := decodeWith(raw, unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM))
		return s, "utf-16be", err
	}

	// Auto-detection: valid UTF-8 without NUL bytes wins.
	if !bytes.ContainsRune(raw, 0) && utf8.Valid(raw) {
		return string(raw), "utf-8", nil
	}

	// Heuristic UTF-16 without BOM if many NULs on even/odd positions.
	if looksLikeUTF16LE(raw) {
		if s, err := decodeWith(raw, unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM)); err == nil {
			return s, "utf-16le", nil
		}
	}
	if looksLikeUTF16BE(raw) {
		if s, err := decodeWith(raw, unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM)); err == nil {
			return s, "utf-16be", nil
		}
	}

	// Fallback to GB18030 (superset of GBK/GB2312 common on Windows CN).
	if s, err := decodeWith(raw, simplifiedchinese.GB18030); err == nil && utf8.ValidString(s) {
		return s, "gb18030", nil
	}

	return "", "", fmt.Errorf("unable to decode text encoding")
}

func decodeWith(raw []byte, enc encoding.Encoding) (string, error) {
	r := transform.NewReader(bytes.NewReader(raw), enc.NewDecoder())
	out, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func looksLikeUTF16LE(b []byte) bool {
	if len(b) < 4 || len(b)%2 != 0 {
		return false
	}
	limit := len(b)
	if limit > 512 {
		limit = 512
	}
	oddZero, evenZero := 0, 0
	for i := 0; i < limit; i += 2 {
		if b[i] == 0 {
			evenZero++
		}
		if b[i+1] == 0 {
			oddZero++
		}
	}
	return oddZero > limit/6 && evenZero < oddZero/4
}

func looksLikeUTF16BE(b []byte) bool {
	if len(b) < 4 || len(b)%2 != 0 {
		return false
	}
	limit := len(b)
	if limit > 512 {
		limit = 512
	}
	oddZero, evenZero := 0, 0
	for i := 0; i < limit; i += 2 {
		if b[i] == 0 {
			evenZero++
		}
		if b[i+1] == 0 {
			oddZero++
		}
	}
	return evenZero > limit/6 && oddZero < evenZero/4
}

func normalizeNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}
