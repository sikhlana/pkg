package log

// BufferCap returns the capacity of w's line buffer, so tests can check that it stays bounded.
func BufferCap(w *Writer) int {
	w.mu.Lock()
	defer w.mu.Unlock()

	return cap(w.buf)
}
