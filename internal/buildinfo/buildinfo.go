package buildinfo

// Version is the current version of the application.
// It is intended to be set at build time using ldflags:
// -ldflags "-X github.com/fanzy618/pop/internal/buildinfo.Version=v1.0.0"
var Version = "unknown"
