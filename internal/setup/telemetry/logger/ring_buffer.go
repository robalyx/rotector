package logger

// RingBuffer implements a circular buffer for log lines.
type RingBuffer struct {
	lines     []string
	capacity  int
	head      int // Points to the next write position
	size      int // Current number of items in buffer
	totalSeen int // Total number of lines that have passed through
}

// NewRingBuffer creates a new ring buffer with the specified capacity.
func NewRingBuffer(capacity int) *RingBuffer {
	return &RingBuffer{
		lines:    make([]string, capacity),
		capacity: capacity,
	}
}

// add adds a line to the ring buffer.
func (rb *RingBuffer) add(line string) {
	rb.lines[rb.head] = line

	rb.head = (rb.head + 1) % rb.capacity
	if rb.size < rb.capacity {
		rb.size++
	}

	rb.totalSeen++
}

// getLines returns all lines in chronological order.
func (rb *RingBuffer) getLines() []string {
	if rb.size == 0 {
		return nil
	}

	result := make([]string, rb.size)
	start := (rb.head - rb.size + rb.capacity) % rb.capacity

	for i := range rb.size {
		idx := (start + i) % rb.capacity
		result[i] = rb.lines[idx]
	}

	return result
}
