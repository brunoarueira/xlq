// Package config loads xlq's optional config file
// (docs/adr/0015-config-file-for-default-flags.md): a flat YAML
// mapping, keyed by each CLI flag's own long name, used as that
// flag's default when the user doesn't pass it explicitly. This
// package only loads and parses the file; internal/cli owns deciding
// where it lives and how its values merge with the command line.
package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config mirrors every config-defaultable flag (ADR-0015's Scope
// section) by its own long flag name. Plain bool/string fields, not
// pointers: every one of these flags' own built-in zero value
// (false, "") is already "not set" for the flag itself, so a config
// file that omits a key or explicitly repeats its zero value are
// indistinguishable - exactly the behavior ADR-0015's Precedence
// section relies on.
type Config struct {
	Compact       bool   `yaml:"compact"`
	CompactOutput bool   `yaml:"compact-output"`
	RawOutput     bool   `yaml:"raw-output"`
	Table         bool   `yaml:"table"`
	CSV           bool   `yaml:"csv"`
	TSV           bool   `yaml:"tsv"`
	InputFormat   string `yaml:"input-format"`
	Delimiter     string `yaml:"delimiter"`
}

// DefaultPath returns the default config file location:
// os.UserConfigDir()/xlq/config.yaml - the right per-OS convention
// ($XDG_CONFIG_HOME or ~/.config on Linux, ~/Library/Application
// Support on macOS, %AppData% on Windows), per ADR-0015.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("config: %w", err)
	}
	return filepath.Join(dir, "xlq", "config.yaml"), nil
}

// Load reads and parses the config file at path. A missing file is
// returned as an ordinary *PathError (checkable with os.IsNotExist) -
// Load itself has no opinion on whether that's an error or not; the
// caller decides, since that depends on whether path came from an
// explicit --config or the default location (see ADR-0015).
//
// Unknown keys are a hard error, not silently ignored - a config file
// is edited by hand, so a typo'd key (e.g. "delimeter") is far more
// likely than a genuine need to carry extra, unrecognized data.
func Load(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer func() { _ = f.Close() }()

	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	var cfg Config
	if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("config: parse %q: %w", path, err)
	}

	// A second "---"-separated document is rejected outright, rather
	// than silently decoded and discarded: yaml.Decoder is a streaming
	// API meant for exactly that (one Decode call per document), so
	// without this check a config file with more than one document
	// would have every document but the first silently ignored instead
	// of being flagged as the probable mistake it is.
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Config{}, fmt.Errorf("config: parse %q: multiple YAML documents are not supported", path)
		}
		return Config{}, fmt.Errorf("config: parse %q: %w", path, err)
	}

	return cfg, nil
}
