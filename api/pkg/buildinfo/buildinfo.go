// Package buildinfo exposes the VCS information stamped into the binary at
// build time (revision, dirty flag, build time) over HTTP.
package buildinfo

import (
	"encoding/json"
	"net/http"
)

// Populated at build time via the linker (`-ldflags -X`), fed by the flake's
// `self.rev` / `self.dirtyRev` (see flake.nix). They default to empty so a bare
// `go build` outside Nix still compiles and just reports empty build info. We
// can't use runtime/debug.ReadBuildInfo: the Nix sandbox has no .git, so the
// toolchain can't stamp vcs.* itself.
var (
	revision  string
	buildTime string
	modified  string // "true" when built from a dirty tree, else "false"/empty
)

// Info describes the commit the running binary was built from. Fields are
// empty when the binary was built outside Nix (e.g. a bare `go build`).
type Info struct {
	Revision string `json:"revision"`
	Modified bool   `json:"modified"`
	Time     string `json:"time,omitempty"`
}

// Read returns the build info stamped into the binary.
func Read() Info {
	return Info{
		Revision: revision,
		Modified: modified == "true",
		Time:     buildTime,
	}
}

// Handler serves the build info as JSON.
func Handler() http.HandlerFunc {
	info := Read()
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(info)
	}
}
