package tui

func pageCount(total, size int) int {
	if total <= 0 || size <= 0 {
		return 0
	}
	return (total + size - 1) / size
}

func clampPage(page, total, size int) int {
	count := pageCount(total, size)
	if count == 0 || page < 0 {
		return 0
	}
	if page >= count {
		return count - 1
	}
	return page
}

func pageRange(page, total, size int) (int, int, bool) {
	if page < 0 || total <= 0 || size <= 0 {
		return 0, 0, false
	}
	start := page * size
	if start >= total {
		return 0, 0, false
	}
	return start, min(start+size, total), true
}
