package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIConvertAndJSONRedaction(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FORMIGO_CONFIG_PATH", filepath.Join(dir, "prefs.json"))

	// 1. capabilities --json
	var out, errBuf bytes.Buffer
	code := Run([]string{"capabilities", "--json"}, &out, &errBuf)
	if code != 0 {
		t.Fatalf("capabilities exit=%d stderr=%s", code, errBuf.String())
	}
	var capPayload map[string]any
	if err := json.Unmarshal(out.Bytes(), &capPayload); err != nil || capPayload["ok"] != true {
		t.Fatalf("invalid capabilities JSON: %s", out.String())
	}

	// 2. convert JSON -> YAML
	jsonFile := filepath.Join(dir, "config.json")
	_ = os.WriteFile(jsonFile, []byte(`{"app":"formigo","offline":true}`), 0o644)

	out.Reset()
	errBuf.Reset()
	code = Run([]string{"convert", jsonFile, "--to", "yaml", "--output-dir", dir, "--json"}, &out, &errBuf)
	if code != 0 {
		t.Fatalf("convert exit=%d out=%s err=%s", code, out.String(), errBuf.String())
	}
	yamlPath := filepath.Join(dir, "config.yaml")
	data, err := os.ReadFile(yamlPath)
	if err != nil || !strings.Contains(string(data), "formigo") {
		t.Fatalf("expected converted YAML at %s, err=%v", yamlPath, err)
	}

	// 3. Password redaction in error JSON
	badPDF := filepath.Join(dir, "corrupt.pdf")
	_ = os.WriteFile(badPDF, []byte("not-a-pdf"), 0o644)
	out.Reset()
	errBuf.Reset()
	secret := "MyTopSecretPass123"
	code = Run([]string{"convert", badPDF, "--pdf-op", "decrypt", "--password", secret, "--json"}, &out, &errBuf)
	if code == 0 {
		t.Fatal("expected non-zero exit code on corrupt PDF")
	}
	if strings.Contains(out.String(), secret) || strings.Contains(errBuf.String(), secret) {
		t.Fatal("password leaked into CLI output!")
	}
}
