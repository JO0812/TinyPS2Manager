// Package isotool parses ISO9660 and UDF volume descriptors (read-only) and
// computes sector/size math used by internal/library for disc-type detection
// (spec §2.3). All operations are streaming; no full-image load.
package isotool