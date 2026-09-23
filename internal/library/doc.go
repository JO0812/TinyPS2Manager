// Package library performs source scanning, content hashing for dedupe and
// override caching, disc-type detection (spec §2.3), and persistence of
// library item metadata.
//
// Detection priority (spec §2.3):
//  1. override — persisted per-hash user choice.
//  2. inspected — ISO9660/UDF volume inspection via internal/isotool.
//  3. heuristic — ≤700 MiB → CD, otherwise DVD.
//  4. database — bundled known-games JSON (required per spec §10 Q3);
//     wins ties against the heuristic.
//
// The bundled database lives in ./data/ps2_cd_titles.json (go:embed) and is
// user-extensible via ps2_cd_titles.user.json in the user config dir.
package library