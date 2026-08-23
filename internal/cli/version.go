package cli

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

func runVersion(_ []string, stdio IO, build BuildInfo) int {
	b := resolveBuildInfo(build)
	fmt.Fprintf(stdio.Out, "swagger-merger %s\n", b.Version)
	if b.Commit != "" {
		fmt.Fprintf(stdio.Out, "  commit:   %s\n", b.Commit)
	}
	if b.Date != "" {
		fmt.Fprintf(stdio.Out, "  built:    %s\n", b.Date)
	}
	fmt.Fprintf(stdio.Out, "  go:       %s\n", runtime.Version())
	fmt.Fprintf(stdio.Out, "  platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	return ExitOK
}

// resolveBuildInfo fills the gaps left when the binary was not stamped at link
// time, so a "go install"ed copy still reports something meaningful.
func resolveBuildInfo(b BuildInfo) BuildInfo {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		if b.Version == "" {
			b.Version = "unknown"
		}
		return b
	}
	if b.Version == "" || b.Version == "dev" {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			b.Version = v
		}
	}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			if b.Commit == "" {
				b.Commit = setting.Value
			}
		case "vcs.time":
			if b.Date == "" {
				b.Date = setting.Value
			}
		case "vcs.modified":
			if setting.Value == "true" && b.Commit != "" {
				b.Commit += "-dirty"
			}
		}
	}
	if b.Version == "" {
		b.Version = "dev"
	}
	return b
}
