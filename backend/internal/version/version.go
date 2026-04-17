package version

import (
	"fmt"
	"runtime/debug"
	"strings"
)

const BaseVersion = "v0.3.0-beta.1"

var (
	Version   = BaseVersion
	Commit    = ""
	BuildTime = ""
)

type Info struct {
	Version     string
	BaseVersion string
	Commit      string
	BuildTime   string
	Dirty       bool
}

func Current() Info {
	info := Info{
		Version:     strings.TrimSpace(Version),
		BaseVersion: BaseVersion,
		Commit:      strings.TrimSpace(Commit),
		BuildTime:   strings.TrimSpace(BuildTime),
	}
	if info.Version == "" {
		info.Version = BaseVersion
	}

	if buildInfo, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range buildInfo.Settings {
			switch setting.Key {
			case "vcs.revision":
				if info.Commit == "" {
					info.Commit = strings.TrimSpace(setting.Value)
				}
			case "vcs.time":
				if info.BuildTime == "" {
					info.BuildTime = strings.TrimSpace(setting.Value)
				}
			case "vcs.modified":
				info.Dirty = setting.Value == "true"
			}
		}
	}

	return info
}

func (i Info) ShortCommit() string {
	commit := strings.TrimSpace(i.Commit)
	if len(commit) > 7 {
		return commit[:7]
	}
	return commit
}

func (i Info) Display() string {
	version := strings.TrimSpace(i.Version)
	if version == "" {
		version = BaseVersion
	}
	shortCommit := i.ShortCommit()
	if shortCommit == "" {
		return version
	}
	if i.Dirty {
		return fmt.Sprintf("%s (%s-dirty)", version, shortCommit)
	}
	return fmt.Sprintf("%s (%s)", version, shortCommit)
}
