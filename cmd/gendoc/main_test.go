package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// TestEscapeForMan guards the actual bug this exists to work around:
// confirmed directly against github.com/cpuguy83/go-md2man (the
// library cobra/doc's GenManTree uses to turn Long/Example text into
// roff) that a bare "<filter>" is silently dropped, since its
// markdown pipeline treats it as unrecognized inline HTML, while a
// markdown code span survives unchanged. This doesn't re-verify
// md2man's own behavior (that's its library, not xlq's) - it checks
// escapeForMan actually produces that code-span form, and leaves
// ordinary text (no placeholder) alone.
func TestEscapeForMan(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"<filter> is evaluated against <file>", "`<filter>` is evaluated against `<file>`"},
		{"no placeholders here", "no placeholders here"},
		{"", ""},
		{"<a><b>", "`<a>``<b>`"},
	}
	for _, tt := range tests {
		if got := escapeForMan(tt.in); got != tt.want {
			t.Errorf("escapeForMan(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestEscapeCommandForMan confirms the walk reaches every field cobra
// actually renders (Use/Short/Long/Example), reaches a subcommand, and
// - the one easy way to get this wrong - doesn't double-escape a flag
// visited through both Flags() and PersistentFlags(), which share the
// same underlying *pflag.Flag once a persistent flag is defined.
func TestEscapeCommandForMan(t *testing.T) {
	root := &cobra.Command{
		Use:     "root <file>",
		Short:   "short <file>",
		Long:    "long <file>",
		Example: "example <file>",
	}
	root.PersistentFlags().String("shared", "", "usage <file>")
	sub := &cobra.Command{Use: "sub <file>"}
	root.AddCommand(sub)

	escapeCommandForMan(root, map[*pflag.Flag]bool{})

	for field, got := range map[string]string{
		"Use":     root.Use,
		"Short":   root.Short,
		"Long":    root.Long,
		"Example": root.Example,
	} {
		if !strings.Contains(got, "`<file>`") {
			t.Errorf("root.%s = %q, want it to contain the escaped placeholder", field, got)
		}
	}
	if sub.Use != "sub `<file>`" {
		t.Errorf("sub.Use = %q, want %q - the walk should reach subcommands", sub.Use, "sub `<file>`")
	}

	flag := root.PersistentFlags().Lookup("shared")
	if flag.Usage != "usage `<file>`" {
		t.Errorf("flag usage = %q, want exactly one escape - visiting it via both Flags() and PersistentFlags() must not double-wrap it", flag.Usage)
	}
}
