// Package lang 提供轻量的语种判断，用于决定论文是否还需要翻译。
package lang

import (
	"unicode"
)

// IsMostlyChinese 判断文本是否以中文为主：去掉空白后，汉字占字符数的比例
// 达到 threshold 即认为是中文文本。
//
// 注意按「字符（rune）」而非「字节」计数：一个汉字在 UTF-8 下占 3 字节，
// 若按字节统计，纯中文文本的比例只有约 0.33，会被误判为非中文。
func IsMostlyChinese(text string, threshold float64) bool {
	total, chinese := 0, 0
	for _, r := range text {
		if unicode.IsSpace(r) {
			continue
		}
		total++
		if unicode.Is(unicode.Han, r) {
			chinese++
		}
	}
	if total == 0 {
		return false
	}
	return float64(chinese)/float64(total) >= threshold
}
