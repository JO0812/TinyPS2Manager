// Package usbextreme implements the USBExtreme (.ul) split writer and index
// file generator (spec §2.2). Splitting is required for DVD images exceeding
// 4 GiB − 1 byte when the destination is FAT32. Writing is streamed with a
// small ring buffer; chunks are never held fully in memory.
//
// A reader is also provided for verification: it parses the index and returns
// a combined io.Reader over the numbered chunks in order.
package usbextreme
