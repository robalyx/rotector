package discord

import (
	"sync/atomic"
)

// ScannerPool manages a pool of scanners with round-robin load distribution.
type ScannerPool struct {
	scanners []*Scanner
	index    atomic.Int32
}

// NewScannerPool creates a new scanner pool.
func NewScannerPool(scanners []*Scanner) *ScannerPool {
	return &ScannerPool{
		scanners: scanners,
	}
}

// GetNext returns the next scanner using round-robin selection.
// Returns the scanner and its index. Returns (nil, -1) if pool is empty.
func (p *ScannerPool) GetNext() (*Scanner, int) {
	if len(p.scanners) == 0 {
		return nil, -1
	}

	// Use atomic operation for thread-safe round-robin
	index := p.index.Add(1)
	accountIndex := int(index-1) % len(p.scanners)

	return p.scanners[accountIndex], accountIndex
}

// GetAll returns all scanners in the pool.
func (p *ScannerPool) GetAll() []*Scanner {
	return p.scanners
}

// Size returns the number of scanners in the pool.
func (p *ScannerPool) Size() int {
	return len(p.scanners)
}
