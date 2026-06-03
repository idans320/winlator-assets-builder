package fp16cache

import (
	"fmt"
)

// SieveCache implements the SIEVE eviction algorithm for FP16 buffer caching.
//
// Formal specification:
//
//	Ω     — capacity limit (Ω ∈ ℕ⁺)
//	C_t   — ordered indexable sequence, |C_t| ≤ Ω
//	        index 0 = Head (most recent insertion)
//	        index |C_t|-1 = Tail (oldest insertion)
//	Each element x ∈ C_t: { key uint64, value []byte, visited bool }
//	h_t   — eviction pointer (Hand), ∈ [0, |C_t|-1]
//
// Operations:
//
//	Get(key) → (value, hit)  — Hit sets visited=true, position unchanged
//	Put(key, value)          — Insert at Head with visited=false
//	  If full → run SIEVE scan:
//	    1. While entries[hand].visited:
//	         entries[hand].visited = false
//	         hand = (hand - 1) mod Ω
//	    2. Evict entries[hand]
//	    3. hand = (hand - 1) mod Ω
//
// Invariants:
//   - Scan Resistance: visited=true items survive one hand pass
//   - Zero-Position: new items start at Head with visited=false
//   - Queue Order Stability: hits do NOT change position

type SieveEntry struct {
	Key     uint64 `json:"key"`
	Size    int    `json:"size"`
	Visited bool   `json:"visited"`
}

type SieveCache struct {
	capacity int
	entries  []SieveEntry
	hand     int
	// Per-entry value storage (separate to allow arbitrary []byte values)
	values map[uint64][]byte
	// Stats
	Hits      uint64 `json:"hits"`
	Misses    uint64 `json:"misses"`
	Evictions uint64 `json:"evictions"`
}

func NewSieveCache(capacity int) *SieveCache {
	return &SieveCache{
		capacity: capacity,
		entries:  make([]SieveEntry, 0, capacity),
		hand:     0,
		values:   make(map[uint64][]byte, capacity),
	}
}

func (c *SieveCache) Size() int {
	return len(c.entries)
}

func (c *SieveCache) Capacity() int {
	return c.capacity
}

// indexOf finds the position of a key in the ordered sequence.
// Returns -1 if not found.
func (c *SieveCache) indexOf(key uint64) int {
	for i, e := range c.entries {
		if e.Key == key {
			return i
		}
	}
	return -1
}

// Get retrieves a cached value. Sets visited=true on hit (in-place, no reordering).
func (c *SieveCache) Get(key uint64) ([]byte, bool) {
	idx := c.indexOf(key)
	if idx < 0 {
		c.Misses++
		return nil, false
	}
	c.entries[idx].Visited = true // in-place mutation — Queue Order Stability invariant
	c.Hits++
	val := c.values[key]
	return val, true
}

// Put inserts a new entry. Evicts using SIEVE algorithm if at capacity.
func (c *SieveCache) Put(key uint64, value []byte, size int) {
	if c.indexOf(key) >= 0 {
		return
	}

	// Case III: Eviction needed
	if len(c.entries) >= c.capacity {
		c.evict()
	}

	// Case II: Insert at Head (index 0)
	entry := SieveEntry{Key: key, Size: size, Visited: false}
	c.entries = append([]SieveEntry{entry}, c.entries...)
	c.values[key] = value

	// Advance hand to account for the new Head (index 0 pushed all others right)
	if c.hand < len(c.entries)-1 {
		c.hand++
	}
}

// evict performs the SIEVE scan-and-annihilate loop (Case III).
func (c *SieveCache) evict() {
	n := len(c.entries)
	if n == 0 {
		return
	}

	// Clamp hand to valid range
	if c.hand >= n {
		c.hand = n - 1
	}

	// Loop/Scan Phase: demote visited entries
	for c.entries[c.hand].Visited {
		c.entries[c.hand].Visited = false
		c.hand = (c.hand - 1 + n) % n
	}

	// Annihilation Phase: evict at hand
	evicted := c.entries[c.hand]
	c.Evictions++

	// Remove from sequence, preserving order
	c.entries = append(c.entries[:c.hand], c.entries[c.hand+1:]...)
	delete(c.values, evicted.Key)

	// Set hand for next cycle
	if len(c.entries) > 0 {
		c.hand = (c.hand - 1 + len(c.entries)) % len(c.entries)
	} else {
		c.hand = 0
	}
}

// Contains checks if a key is present without modifying visited state.
func (c *SieveCache) Contains(key uint64) bool {
	return c.indexOf(key) >= 0
}

// HitRate returns the cache hit ratio.
func (c *SieveCache) HitRate() float64 {
	total := c.Hits + c.Misses
	if total == 0 {
		return 0
	}
	return float64(c.Hits) / float64(total)
}

// Stats returns a summary of cache metrics.
func (c *SieveCache) Stats() map[string]interface{} {
	return map[string]interface{}{
		"capacity":   c.capacity,
		"size":       len(c.entries),
		"hits":       c.Hits,
		"misses":     c.Misses,
		"evictions":  c.Evictions,
		"hit_rate":   c.HitRate(),
		"hand_pos":   c.hand,
	}
}

func (c *SieveCache) String() string {
	return fmt.Sprintf("SIEVE[cap=%d size=%d hits=%d misses=%d evictions=%d rate=%.1f%% hand=%d]",
		c.capacity, len(c.entries), c.Hits, c.Misses, c.Evictions, c.HitRate()*100, c.hand)
}
