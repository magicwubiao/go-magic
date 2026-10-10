package server

import (
	"runtime/debug"
	"sync"
)

// Build metadata for the running binary.
//
// cmd/magic owns the ldflags-injected values (main.Version / main.Commit /
// main.BuildDate) and forwards them here via SetBuildInfo before starting the
// server. This package cannot import package main, and a main module does not
// list itself in debug.ReadBuildInfo().Deps, so this hand-off is the only way
// the HTTP API can report the real release -- without it /api/status and
// /api/system/version fall back to "dev" on every release while the
// `magic version` CLI subcommand is correct.
var (
	buildInfoMu       sync.RWMutex
	injectedVersion   string
	injectedCommit    string
	injectedBuildDate string
)

// Mono-repo module path, used to detect the (unusual) case where go-magic is
// consumed as a dependency rather than built as the main module.
const selfModulePath = "github.com/magicwubiao/go-magic"

// SetBuildInfo records the build metadata to report. Empty values are ignored
// so a partially populated call cannot erase a known value. Safe to call
// before any Server exists (tests, embedders) and safe for concurrent use.
func SetBuildInfo(version, commit, buildDate string) {
	buildInfoMu.Lock()
	defer buildInfoMu.Unlock()
	if version != "" {
		injectedVersion = version
	}
	if commit != "" {
		injectedCommit = commit
	}
	if buildDate != "" {
		injectedBuildDate = buildDate
	}
}

// inferVersion derives a version from Go's embedded build info. It only
// succeeds for the dependency/`go install` case; building the main module
// yields an empty main-module version, in which case "dev" is not returned
// here (the caller applies that default) because SetBuildInfo may still win.
func inferVersion(info *debug.BuildInfo) string {
	if info == nil {
		return ""
	}
	if info.Main.Path == selfModulePath && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	for _, dep := range info.Deps {
		if dep.Path != selfModulePath {
			continue
		}
		if dep.Replace != nil {
			return dep.Replace.Version
		}
		return dep.Version
	}
	return ""
}

// inferVCS reports the commit and commit time Go stamps in when VCS stamping
// is enabled (it is off in the container build, hence "unknown" fallbacks).
func inferVCS(info *debug.BuildInfo) (commit, buildDate string) {
	if info == nil {
		return "", ""
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			commit = s.Value
		case "vcs.time":
			buildDate = s.Value
		}
	}
	return commit, buildDate
}
