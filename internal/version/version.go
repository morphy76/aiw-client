package version

var (
	// Version is the current component version, injected at build time.
	Version = "dev"
	// Commit is the git commit hash, injected at build time.
	Commit = "none"
	// BuildTime is the build timestamp, injected at build time.
	BuildTime = "unknown"
)
