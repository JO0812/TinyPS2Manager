package queue

import (
	"fmt"

	"github.com/jo/TinyPS2Manager/internal/transfer"
)

// ResolveDestination merges per-path settings with live detection into
// the Destination view the planner, executor, and API share.
// Uncustomized live volumes resolve as drives; anything else defaults to
// folder. Detection never fails closed here: unknown filesystems and
// sizes surface as unknown/-1 for callers to handle.
func ResolveDestination(s *Store, path string) (*Destination, error) {
	if path == "" {
		return nil, fmt.Errorf("destination path is empty")
	}
	kind := DestFolder
	var prefix, override, updated string
	customized := false
	if st, err := s.GetSettings(path); err != nil {
		return nil, err
	} else if st != nil {
		customized = true
		kind, prefix, override, updated = st.Kind, st.BDMPrefix, st.FSOverride, st.UpdatedAt
	}
	var label string
	if vols, err := transfer.Volumes(); err == nil {
		for _, v := range vols {
			if v.Path == path {
				kind = DestDrive
				label = v.Label
				break
			}
		}
	}
	probed, _ := transfer.Probe(path)
	return &Destination{
		Path: path, Label: label, Kind: kind, Filesystem: string(probed.Filesystem),
		FSOverride: override, BDMPrefix: prefix,
		FreeBytes: probed.FreeBytes, TotalBytes: probed.TotalBytes,
		Customized: customized, UpdatedAt: updated,
	}, nil
}

// LivePaths returns every destination path worth serving: live volumes,
// customized paths, and paths with live jobs. The executor spawns one
// runner per entry; waitForPath parks the ones currently unplugged.
func LivePaths(s *Store) map[string]bool {
	targets := map[string]bool{}
	if vols, err := transfer.Volumes(); err == nil {
		for _, v := range vols {
			targets[v.Path] = true
		}
	}
	if sts, err := s.ListSettings(); err == nil {
		for _, st := range sts {
			targets[st.Path] = true
		}
	}
	if paths, err := s.ActiveJobPaths(); err == nil {
		for _, p := range paths {
			targets[p] = true
		}
	}
	return targets
}
