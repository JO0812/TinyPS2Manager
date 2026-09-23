// Package cuebin parses CUE sheets and merges referenced BIN tracks into a
// single .VCD image (spec §2.4) for POPSTARTER. The merge is streamed with a
// bounded buffer; the full merged image is never held in memory.
//
// Multi-track CUE/BIN sets are folded into one contiguous 2352-byte/sector
// raw image using the sheet's index/pregap data. The PS-X disc-ID is extracted
// from the system area to form the preferred SXXX_NNN.NN.Title.VCD filename.
//
// This is the highest-risk component in M1 (BUILD-PLAN.md §9); its design and
// golden fixtures should begin first.
package cuebin
