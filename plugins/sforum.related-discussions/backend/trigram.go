package main

import "strings"

// trigramSimilarity 是 pg_trgm `similarity()` 的近似实现。
//
// 只在两种场合使用：没有数据库连接时的降级模式，以及不依赖 PostgreSQL 的单测。
// 它与 pg_trgm 的差异是「按整串切三元组」而不是「按单词切」——对中文标题
// （本来就没有空格分词）两者等价，对英文标题会略微低估，排序方向一致。
// 生产路径一律走 SQL 的 similarity()，不走这里。
func trigramSimilarity(a, b string) float64 {
	left := trigramSet(a)
	right := trigramSet(b)
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	common := 0
	for gram := range left {
		if _, ok := right[gram]; ok {
			common++
		}
	}
	union := len(left) + len(right) - common
	if union == 0 {
		return 0
	}
	return float64(common) / float64(union)
}

// trigramSet 前后各补空格，让首尾三元组也能参与比较（与 pg_trgm 一致）。
func trigramSet(value string) map[string]struct{} {
	runes := []rune(strings.ToLower(strings.TrimSpace(value)))
	if len(runes) == 0 {
		return nil
	}
	padded := make([]rune, 0, len(runes)+3)
	padded = append(padded, ' ', ' ')
	padded = append(padded, runes...)
	padded = append(padded, ' ')

	set := make(map[string]struct{}, len(padded))
	for index := 0; index+3 <= len(padded); index++ {
		set[string(padded[index:index+3])] = struct{}{}
	}
	return set
}
