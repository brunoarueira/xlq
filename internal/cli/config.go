package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/brunoarueira/xlq/internal/config"
)

// mergeConfigDefaults is root's PersistentPreRunE
// (docs/adr/0015-config-file-for-default-flags.md): it loads the
// optional config file and applies its values as flag defaults before
// cmd's own RunE reads any flag, so every RunE (and
// outputOptions.validate()) sees a config-sourced value exactly as if
// the user had typed it - no config-awareness of their own needed.
// cmd is the command actually being run (root itself, or a subcommand
// like sheets), not necessarily root, since Cobra always invokes the
// nearest PersistentPreRun(E) with that command.
func mergeConfigDefaults(cmd *cobra.Command, _ []string) error {
	// Cobra's own "completion" command (and its bash/zsh/fish/
	// powershell children) and its hidden __complete shell-completion-
	// request command both have root as their nearest
	// PersistentPreRunE too, but neither reads any config-defaultable
	// flag - loading config for them only risks a config file's own
	// typo silently breaking tab completion on every keystroke, for a
	// command that has nothing to do with config at all.
	if isCompletionRequest(cmd) {
		return nil
	}

	explicit := cmd.Flags().Changed("config")
	path, _ := cmd.Flags().GetString("config")
	if !explicit {
		def, err := config.DefaultPath()
		if err != nil {
			return err
		}
		path = def
	}

	cfg, err := config.Load(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && !explicit {
			return nil
		}
		return fmt.Errorf("--config: %w", err)
	}

	// -i suppresses every config value outright, not just the six
	// flags root.go's own --in-place check names: -i is .xlsx-only
	// and rejects anything else regardless of the real file's actual
	// content, so a config input-format/delimiter default meant for a
	// user's everyday csv work has no business reaching an occasional
	// -i edit of a real .xlsx file (ADR-0015's Precedence section).
	// This checks the flag's actual value, not Changed - an explicit
	// but no-op "--in-place=false" must not suppress every config
	// value the way "-i"/"--in-place" (true) does.
	if inPlace, _ := cmd.Flags().GetBool("in-place"); inPlace {
		return nil
	}

	mergeString(cmd, "input-format", cfg.InputFormat)
	mergeString(cmd, "delimiter", cfg.Delimiter)

	// compact-output, raw-output, table, csv, and tsv aren't
	// independent: they're outputOptions.validate()'s own mutually-
	// exclusive set, so an explicitly *active* flag among them must
	// suppress every one of config's values for the whole group, not
	// just the one with the same name - otherwise an explicit --csv
	// would leave a config table:true in place and fail validation
	// instead of honoring the override. This checks each flag's
	// actual value (GetBool), not Changed, for the same reason as the
	// -i check above: an explicit but no-op "--table=false" must not
	// suppress a config csv:true default.
	if !anyTrue(cmd, "compact-output", "raw-output", "table", "csv", "tsv") {
		mergeBool(cmd, "compact-output", cfg.CompactOutput)
		mergeBool(cmd, "raw-output", cfg.RawOutput)
		mergeBool(cmd, "table", cfg.Table)
		mergeBool(cmd, "csv", cfg.CSV)
		mergeBool(cmd, "tsv", cfg.TSV)
	}
	// compact composes freely with every format (validate() never
	// objects to it), so it merges independently of the group above.
	mergeBool(cmd, "compact", cfg.Compact)

	return nil
}

// completionCmdName is Cobra's own name for its built-in "completion"
// command (internal to Cobra as compCmdName - not exported, so it's
// duplicated here as the literal it's defined as).
const completionCmdName = "completion"

// isCompletionRequest reports whether cmd is Cobra's own "completion"
// command, one of its direct bash/zsh/fish/powershell children, or the
// hidden shell-completion-request command (__complete/__completeNoDesc,
// both named ShellCompRequestCmd - CalledAs, not Name, distinguishes
// the alias, which doesn't matter here).
func isCompletionRequest(cmd *cobra.Command) bool {
	if cmd.Name() == cobra.ShellCompRequestCmd || cmd.Name() == completionCmdName {
		return true
	}
	return cmd.Parent() != nil && cmd.Parent().Name() == completionCmdName
}

func anyTrue(cmd *cobra.Command, names ...string) bool {
	for _, name := range names {
		if cmd.Flags().Lookup(name) == nil {
			continue
		}
		if v, _ := cmd.Flags().GetBool(name); v {
			return true
		}
	}
	return false
}

// mergeBool sets cmd's name flag to true from config, unless the
// current command doesn't define that flag at all (e.g. sheets has no
// --table), the user already passed it explicitly, or config's own
// value is false (config's zero value is indistinguishable from
// "not set," so there's nothing to apply either way).
func mergeBool(cmd *cobra.Command, name string, value bool) {
	if !value {
		return
	}
	f := cmd.Flags().Lookup(name)
	if f == nil || f.Changed {
		return
	}
	_ = cmd.Flags().Set(name, "true")
}

// mergeString is mergeBool for a string-valued flag.
func mergeString(cmd *cobra.Command, name, value string) {
	if value == "" {
		return
	}
	f := cmd.Flags().Lookup(name)
	if f == nil || f.Changed {
		return
	}
	_ = cmd.Flags().Set(name, value)
}
