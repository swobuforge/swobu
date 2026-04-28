package views

// ListWindowBounds returns the visible [start,end) range for a bounded list
// based on a caller-managed offset.
func ListWindowBounds(total int, offset int, window int) (int, int) {
	if total <= 0 {
		return 0, 0
	}
	if window <= 0 || window >= total {
		return 0, total
	}
	maxStart := total - window
	if offset < 0 {
		offset = 0
	}
	if offset > maxStart {
		offset = maxStart
	}
	return offset, offset + window
}
