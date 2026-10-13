package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestLoadValid(t *testing.T) {
	path := writeConfig(t, "table: true\ninput-format: csv\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Config{Table: true, InputFormat: "csv"}
	if cfg != want {
		t.Errorf("Load(%q) = %+v, want %+v", path, cfg, want)
	}
}

func TestLoadEveryKey(t *testing.T) {
	path := writeConfig(t, `
compact: true
compact-output: true
raw-output: true
table: true
csv: true
tsv: true
input-format: ods
delimiter: ";"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Config{
		Compact:       true,
		CompactOutput: true,
		RawOutput:     true,
		Table:         true,
		CSV:           true,
		TSV:           true,
		InputFormat:   "ods",
		Delimiter:     ";",
	}
	if cfg != want {
		t.Errorf("Load(%q) = %+v, want %+v", path, cfg, want)
	}
}

func TestLoadEmptyFileIsZeroConfig(t *testing.T) {
	path := writeConfig(t, "")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg != (Config{}) {
		t.Errorf("Load(%q) = %+v, want the zero Config", path, cfg)
	}
}

func TestLoadMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.yaml")

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load with a missing file: want error, got nil")
	}
	if !os.IsNotExist(err) {
		t.Errorf("Load error = %v, want an os.IsNotExist error so callers can distinguish it from a real parse failure", err)
	}
}

func TestLoadUnknownKeyRejected(t *testing.T) {
	path := writeConfig(t, "delimeter: \";\"\n") // typo: "delimeter", not "delimiter"

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load with an unrecognized key: want error, got nil")
	}
}

func TestLoadMultiDocumentRejected(t *testing.T) {
	path := writeConfig(t, "table: true\n---\ncsv: true\n")

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load with a multi-document YAML file: want error, got nil")
	}
}

// TestLoadNonMapRootRejected guards against yaml.v3's own tolerance
// for decoding a non-mapping document (a bare "null"/"~", a scalar, or
// a list) into a zero-valued Config without complaint - this format is
// documented as a flat mapping, so anything else should be a clear
// error, not silently treated the same as an empty file.
func TestLoadNonMapRootRejected(t *testing.T) {
	cases := []struct {
		name     string
		contents string
	}{
		{"explicit null", "null\n"},
		{"tilde null", "~\n"},
		{"bare scalar", "42\n"},
		{"a list", "- a\n- b\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeConfig(t, tc.contents)
			if _, err := Load(path); err == nil {
				t.Errorf("Load(%q): want error, got nil", tc.contents)
			}
		})
	}
}

func TestLoadMalformedYAMLRejected(t *testing.T) {
	path := writeConfig(t, "table: [this is not a bool\n")

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load with malformed YAML: want error, got nil")
	}
}

func TestDefaultPathEndsInXlqConfigYAML(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")

	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	if got, want := filepath.Base(path), "config.yaml"; got != want {
		t.Errorf("DefaultPath() base = %q, want %q", got, want)
	}
	if got, want := filepath.Base(filepath.Dir(path)), "xlq"; got != want {
		t.Errorf("DefaultPath() parent dir = %q, want %q", got, want)
	}
}
