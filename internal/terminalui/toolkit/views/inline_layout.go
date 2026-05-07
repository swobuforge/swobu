package views

import "strings"

// OverflowPriority controls which inline items lose width first under
// constraint pressure. Preserve survives longest, Sacrifice drops first.
type OverflowPriority uint8

const (
	OverflowPreserve OverflowPriority = iota
	OverflowNormal
	OverflowSacrifice
)

// InlineItemSpec defines one horizontal slot in a row.
type InlineItemSpec struct {
	Text      string
	Basis     int
	Grow      int
	Shrink    int
	Min       int
	Max       int
	Priority  OverflowPriority
	AlignRight bool
}

// InlineLayoutSpec is a minimal row/inline algebra for transcript/retained
// text rows. It resolves slot widths from constraints and renders one line.
type InlineLayoutSpec struct {
	Gap   int
	Items []InlineItemSpec
}

type inlineResolvedItem struct {
	spec  InlineItemSpec
	width int
}

func renderInline(spec InlineLayoutSpec, width int) string {
	if width <= 0 {
		return ""
	}
	if spec.Gap < 0 {
		spec.Gap = 0
	}
	if len(spec.Items) == 0 {
		return strings.Repeat(" ", width)
	}
	resolved := resolveInlineWidths(spec, width)
	parts := make([]string, 0, len(resolved))
	for _, it := range resolved {
		cellWidth := it.width
		if cellWidth < 0 {
			cellWidth = 0
		}
		// Zero-width slots are removed from final composition so they do not
		// leave orphaned inter-item gaps (prevents action-only drift on narrow rows).
		if cellWidth == 0 {
			continue
		}
		text := trimToWidthRaw(it.spec.Text, cellWidth)
		if it.spec.AlignRight {
			text = padRightAligned(text, cellWidth)
		} else {
			text = padRight(text, cellWidth)
		}
		parts = append(parts, text)
	}
	if len(parts) == 0 {
		return strings.Repeat(" ", width)
	}
	line := strings.Join(parts, strings.Repeat(" ", spec.Gap))
	return padRight(trimToWidthRaw(line, width), width)
}

func resolveInlineWidths(spec InlineLayoutSpec, width int) []inlineResolvedItem {
	out := make([]inlineResolvedItem, 0, len(spec.Items))
	for _, item := range spec.Items {
		basis := item.Basis
		if basis <= 0 {
			basis = runeLen(item.Text)
		}
		minW := item.Min
		if minW < 0 {
			minW = 0
		}
		maxW := item.Max
		if maxW > 0 && basis > maxW {
			basis = maxW
		}
		if basis < minW {
			basis = minW
		}
		out = append(out, inlineResolvedItem{
			spec:  item,
			width: basis,
		})
	}

	totalGap := 0
	if len(out) > 1 {
		totalGap = spec.Gap * (len(out) - 1)
	}
	used := totalGap
	for _, it := range out {
		used += it.width
	}
	if used < width {
		expandInline(out, width-used)
	} else if used > width {
		shrinkInline(out, used-width)
	}
	return out
}

func expandInline(items []inlineResolvedItem, extra int) {
	if extra <= 0 {
		return
	}
	for extra > 0 {
		progressed := false
		for i := range items {
			if items[i].spec.Grow <= 0 {
				continue
			}
			maxW := items[i].spec.Max
			if maxW > 0 && items[i].width >= maxW {
				continue
			}
			items[i].width++
			extra--
			progressed = true
			if extra == 0 {
				return
			}
		}
		if !progressed {
			return
		}
	}
}

func shrinkInline(items []inlineResolvedItem, deficit int) {
	if deficit <= 0 {
		return
	}
	priorities := []OverflowPriority{OverflowSacrifice, OverflowNormal, OverflowPreserve}
	for _, p := range priorities {
		for deficit > 0 {
			progressed := false
			for i := range items {
				if items[i].spec.Priority != p || items[i].spec.Shrink <= 0 {
					continue
				}
				minW := items[i].spec.Min
				if minW < 0 {
					minW = 0
				}
				if items[i].width <= minW {
					continue
				}
				items[i].width--
				deficit--
				progressed = true
				if deficit == 0 {
					return
				}
			}
			if !progressed {
				break
			}
		}
	}
}

func padRightAligned(s string, width int) string {
	if width <= 0 {
		return ""
	}
	n := runeLen(s)
	if n >= width {
		return trimToWidthRaw(s, width)
	}
	return strings.Repeat(" ", width-n) + s
}
