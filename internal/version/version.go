// Package version holds xlq's build version. It defaults to "dev" for
// local/CI builds; release builds inject the real value via -ldflags
// (see .goreleaser.yaml and docs/adr/0006-release-automation.md).
package version

var Version = "dev"
