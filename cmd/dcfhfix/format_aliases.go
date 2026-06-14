package main

import "github.com/mattkeenan/dircachefilehash/pkg/format"

// dcfhfix previously hand-copied the on-disk binaryEntry and indexHeader structs
// (and a parallel offset table) because the originals were unexported. They now
// live in pkg/format, the single owner of the on-disk layout. These aliases keep
// dcfhfix's existing references working while removing the duplication.
//
// Note: the old local indexHeader was 96 bytes (no Timestamp tail); format.Header
// is 104 bytes with identical field offsets 0..92. Adopting it fixes a latent
// 8-byte over-read in the header write path and preserves the v3 Timestamp.
type (
	indexHeader = format.Header
)
