package service

import "strconv"

func formatPrice(p float64) string {
	return strconv.FormatFloat(p, 'f', -1, 64)
}

func formatSize(s float64) string {
	return strconv.FormatFloat(s, 'f', -1, 64)
}
