package dircachefilehash

import (
	"fmt"
	"time"
)

// mockBinaryEntry is a minimal in-memory BinaryEntryInterface used by the
// hash-coordination tests. It carries only the fields the tests touch and
// returns benign defaults for everything else.
type mockBinaryEntry struct {
	relPath       string
	size          uint64
	deleted       bool
	mtime         time.Time
	hashValue     [20]byte
	hashRequested bool
	hashCompleted bool
	hashJobID     uint64
}

func (m *mockBinaryEntry) RelativePath() (string, error)                   { return m.relPath, nil }
func (m *mockBinaryEntry) Size() (uint32, error)                           { return uint32(m.size), nil }
func (m *mockBinaryEntry) FileSize() (uint64, error)                       { return m.size, nil }
func (m *mockBinaryEntry) IsDeleted() (bool, error)                        { return m.deleted, nil }
func (m *mockBinaryEntry) Hash() ([20]byte, error)                         { return m.hashValue, nil }
func (m *mockBinaryEntry) HashString() (string, error)                     { return fmt.Sprintf("%x", m.hashValue), nil }
func (m *mockBinaryEntry) HashType() (uint16, error)                       { return HashTypeSHA1, nil }
func (m *mockBinaryEntry) MTimeWall() (uint64, error)                      { return uint64(m.mtime.Unix()), nil }
func (m *mockBinaryEntry) CTimeWall() (uint64, error)                      { return uint64(m.mtime.Unix()), nil }
func (m *mockBinaryEntry) Dev() (uint32, error)                            { return 123, nil }
func (m *mockBinaryEntry) Ino() (uint32, error)                            { return 456, nil }
func (m *mockBinaryEntry) Mode() (uint32, error)                           { return 0644, nil }
func (m *mockBinaryEntry) UID() (uint32, error)                            { return 1000, nil }
func (m *mockBinaryEntry) GID() (uint32, error)                            { return 1000, nil }
func (m *mockBinaryEntry) EntryFlags() (uint32, error)                     { return 0, nil }
func (m *mockBinaryEntry) SetHash(hashBytes []byte, hashType uint16) error { return nil }
func (m *mockBinaryEntry) SetDeleted(deleted bool) error                   { m.deleted = deleted; return nil }
func (m *mockBinaryEntry) RLock()                                          {}
func (m *mockBinaryEntry) RUnlock()                                        {}
func (m *mockBinaryEntry) Lock()                                           {}
func (m *mockBinaryEntry) Unlock()                                         {}
func (m *mockBinaryEntry) IsValid() bool                                   { return true }
func (m *mockBinaryEntry) SupportsSkiplistBuilding() bool                  { return false }
func (m *mockBinaryEntry) GetBinaryEntryRef() (binaryEntryRef, bool)       { return binaryEntryRef{}, false }
func (m *mockBinaryEntry) GetContext() (string, error)                     { return "mock", nil }
func (m *mockBinaryEntry) RequestHash() error                              { m.hashRequested = true; return nil }
func (m *mockBinaryEntry) IsHashRequested() (bool, error)                  { return m.hashRequested, nil }
func (m *mockBinaryEntry) IsHashCompleted() (bool, error)                  { return m.hashCompleted, nil }
func (m *mockBinaryEntry) SetHashJobID(jobID uint64)                       { m.hashJobID = jobID }
func (m *mockBinaryEntry) GetHashJobID() uint64                            { return m.hashJobID }
func (m *mockBinaryEntry) MarkHashCompleted()                              { m.hashCompleted = true }
