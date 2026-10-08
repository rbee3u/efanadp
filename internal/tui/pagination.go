package tui

import (
	"strings"
)

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

func renderPaginationDots(page, count int) string {
	if count < 2 {
		return ""
	}
	page = min(max(page, 0), count-1)
	var b strings.Builder
	for i := range count {
		if i == page {
			b.WriteString(paginationActiveDotStyle.Render("•"))
			continue
		}
		b.WriteString(paginationInactiveDotStyle.Render("•"))
	}
	return b.String()
}
