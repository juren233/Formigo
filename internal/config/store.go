package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"formigo/internal/i18n"
)

// OutputMode controls where converted files are written by default.
type OutputMode string

const (
	OutputSameAsSource OutputMode = "source"
	OutputCustomDir    OutputMode = "custom"
)

// ConflictPolicy controls how existing destination files are handled.
type ConflictPolicy string

const (
	ConflictRename    ConflictPolicy = "rename"
	ConflictOverwrite ConflictPolicy = "overwrite"
)

// Appearance controls Light/Dark/System theme mode.
type Appearance string

const (
	AppearanceSystem Appearance = "system"
	AppearanceLight  Appearance = "light"
	AppearanceDark   Appearance = "dark"
)

// Preferences represents persisted user configuration.
type Preferences struct {
	Language        i18n.Lang         `json:"language"`
	Appearance      Appearance        `json:"appearance"`
	OutputMode      OutputMode        `json:"outputMode"`
	CustomOutputDir string            `json:"customOutputDir"`
	LastOpenDir     string            `json:"lastOpenDir"`
	LastSaveDir     string            `json:"lastSaveDir"`
	ConflictPolicy  ConflictPolicy    `json:"conflictPolicy"`
	Concurrency     int               `json:"concurrency"`
	ImageQuality    int               `json:"imageQuality"`
	VideoCodec      string            `json:"videoCodec"`
	AudioBitrate    string            `json:"audioBitrate"`
	FormatMemory    map[string]string `json:"formatMemory"`
	EnginePaths     map[string]string `json:"enginePaths"`
}

// DefaultPreferences returns sensible defaults for the current OS.
func DefaultPreferences() Preferences {
	workers := runtime.NumCPU()
	if workers > 4 {
		workers = 4
	}
	if workers < 1 {
		workers = 2
	}
	home, _ := os.UserHomeDir()
	defaultOut := ""
	if home != "" {
		dl := filepath.Join(home, "Downloads")
		if info, err := os.Stat(dl); err == nil && info.IsDir() {
			defaultOut = dl
		} else {
			defaultOut = home
		}
	}
	return Preferences{
		Language:        i18n.LangAuto,
		Appearance:      AppearanceSystem,
		OutputMode:      OutputSameAsSource,
		CustomOutputDir: defaultOut,
		ConflictPolicy:  ConflictRename,
		Concurrency:     workers,
		ImageQuality:    90,
		VideoCodec:      "h264",
		AudioBitrate:    "192k",
		FormatMemory:    make(map[string]string),
		EnginePaths:     make(map[string]string),
	}
}

// Store manages thread-safe reading and atomic saving of Preferences.
type Store struct {
	mu   sync.RWMutex
	path string
	data Preferences
}

// DefaultConfigPath resolves the OS-appropriate config file location.
func DefaultConfigPath() string {
	if env := formigoEnv("FORMIGO_CONFIG_PATH", "FOMIGO_CONFIG_PATH"); env != "" {
		return env
	}
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "formigo", "preferences.json")
}

func formigoEnv(name, legacyName string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return strings.TrimSpace(os.Getenv(legacyName))
}

// LoadStore loads preferences from path (or DefaultConfigPath if empty), falling back to defaults.
func LoadStore(path string) *Store {
	if path == "" {
		path = DefaultConfigPath()
	}
	s := &Store{
		path: path,
		data: DefaultPreferences(),
	}
	raw, err := os.ReadFile(path)
	if err == nil && len(raw) > 0 {
		var loaded Preferences
		if json.Unmarshal(raw, &loaded) == nil {
			s.mergeLoaded(loaded)
		}
	}
	return s
}

func (s *Store) mergeLoaded(loaded Preferences) {
	if loaded.Language == i18n.LangAuto || loaded.Language == i18n.LangZH || loaded.Language == i18n.LangEN {
		s.data.Language = loaded.Language
	}
	if loaded.Appearance == AppearanceSystem || loaded.Appearance == AppearanceLight || loaded.Appearance == AppearanceDark {
		s.data.Appearance = loaded.Appearance
	}
	if loaded.OutputMode == OutputSameAsSource || loaded.OutputMode == OutputCustomDir {
		s.data.OutputMode = loaded.OutputMode
	}
	if loaded.CustomOutputDir != "" {
		s.data.CustomOutputDir = loaded.CustomOutputDir
	}
	if loaded.LastOpenDir != "" {
		s.data.LastOpenDir = loaded.LastOpenDir
	}
	if loaded.LastSaveDir != "" {
		s.data.LastSaveDir = loaded.LastSaveDir
	}
	if loaded.ConflictPolicy == ConflictRename || loaded.ConflictPolicy == ConflictOverwrite {
		s.data.ConflictPolicy = loaded.ConflictPolicy
	}
	if loaded.Concurrency >= 1 && loaded.Concurrency <= 16 {
		s.data.Concurrency = loaded.Concurrency
	}
	if loaded.ImageQuality >= 1 && loaded.ImageQuality <= 100 {
		s.data.ImageQuality = loaded.ImageQuality
	}
	if loaded.VideoCodec != "" {
		s.data.VideoCodec = loaded.VideoCodec
	}
	if loaded.AudioBitrate != "" {
		s.data.AudioBitrate = loaded.AudioBitrate
	}
	if loaded.FormatMemory != nil {
		for k, v := range loaded.FormatMemory {
			k = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(k)), ".")
			v = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(v)), ".")
			if k != "" && v != "" {
				s.data.FormatMemory[k] = v
			}
		}
	}
	if loaded.EnginePaths != nil {
		for k, v := range loaded.EnginePaths {
			if strings.TrimSpace(k) != "" {
				s.data.EnginePaths[k] = strings.TrimSpace(v)
			}
		}
	}
}

// Get returns a snapshot copy of Preferences.
func (s *Store) Get() Preferences {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := s.data
	cp.FormatMemory = make(map[string]string, len(s.data.FormatMemory))
	for k, v := range s.data.FormatMemory {
		cp.FormatMemory[k] = v
	}
	cp.EnginePaths = make(map[string]string, len(s.data.EnginePaths))
	for k, v := range s.data.EnginePaths {
		cp.EnginePaths[k] = v
	}
	return cp
}

// Update mutates Preferences under a lock and persists atomically to disk.
func (s *Store) Update(fn func(p *Preferences)) error {
	s.mu.Lock()
	fn(&s.data)
	if s.data.FormatMemory == nil {
		s.data.FormatMemory = make(map[string]string)
	}
	if s.data.EnginePaths == nil {
		s.data.EnginePaths = make(map[string]string)
	}
	raw, err := json.MarshalIndent(s.data, "", "  ")
	path := s.path
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// RememberTarget stores the user's chosen target format for a source extension.
func (s *Store) RememberTarget(sourceExt, targetExt string) {
	sourceExt = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(sourceExt)), ".")
	targetExt = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(targetExt)), ".")
	if sourceExt == "" || targetExt == "" {
		return
	}
	_ = s.Update(func(p *Preferences) {
		p.FormatMemory[sourceExt] = targetExt
	})
}

// PreferredTarget returns the remembered target format for sourceExt, or "".
func (s *Store) PreferredTarget(sourceExt string) string {
	sourceExt = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(sourceExt)), ".")
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.FormatMemory[sourceExt]
}
