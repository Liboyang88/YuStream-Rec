package translator

import (
	"regexp"
	"strings"
)

// maxTextLengthDefault 是 alimt 单次请求允许的最大字符数（接口文档：5000）。
// 超过这个长度会被接口以 10008 拒绝，所以调用前先按它分段。
const maxTextLengthDefault = 5000

// splitSegments 把文本切成若干个不超过 maxChars 字符的分段。
//
// 两条硬约束：
//
//  1. **绝不在分隔符中间切**——分隔符必须原样留在某个分段里，否则标题与摘要的
//     合并结果就再也拆不回来了（宁可把超长的那一段整体丢给接口报错，也不能悄悄产出
//     一条永远拆不开的译文）；
//  2. 分段拼接起来必须与原文逐字相同——请求文本被改动就等于翻译了别的内容。
//
// 先按分隔符切，再对仍然超长的部分按标点、空格逐级切，最后才按字符数硬切。
// 绝大多数论文摘要远短于 5000 字符，这个函数通常只返回一个分段（等于不分段）。
func splitSegments(text, sep string, maxChars int) []string {
	if maxChars <= 0 {
		maxChars = maxTextLengthDefault
	}
	if runeLen(text) <= maxChars {
		return []string{text}
	}
	if sep == "" || !strings.Contains(text, sep) {
		return splitLong(text, maxChars)
	}

	// 分隔符保持原子：它一定完整落在某个分段里，由 splitKeepingSeparator 保证。
	raw := strings.SplitAfter(text, sep)
	segments := make([]string, 0, len(raw))
	for _, part := range raw {
		segments = append(segments, splitKeepingSeparator(part, sep, maxChars)...)
	}
	return segments
}

// splitKeepingSeparator 切分一个可能含分隔符的片段，并保证分隔符不被切断。
func splitKeepingSeparator(part, sep string, maxChars int) []string {
	if runeLen(part) <= maxChars {
		return []string{part}
	}
	idx := strings.LastIndex(part, sep)

	// 没有分隔符，或分隔符在开头（前面没有可切断的正文）：直接交给普通切分。
	if idx <= 0 {
		return splitLong(part, maxChars)
	}

	// 分隔符自身就超过上限：无解，只能硬切（不会发生：上限 5000，分隔符 15 字符）。
	if runeLen(sep) >= maxChars {
		return hardCut(part, maxChars)
	}

	// 正常情况下分隔符在末尾：把正文切好，再让分隔符单独成段，
	// 从而既不超过上限，也不切断分隔符——拼接回来仍是原文。
	return append(splitLong(part[:idx], maxChars), part[idx:])
}

// splitLong 把一段文本切成不超过 maxChars 的分段，逐级尝试更细的切分点。
func splitLong(text string, maxChars int) []string {
	if runeLen(text) <= maxChars {
		return []string{text}
	}
	return splitWithLevel(text, 0, maxChars)
}

// splitLevels 是逐级细化的切分点：段落 → 句子 → 分句 → 词 → 空格。
//
// 每级都是"边界正则 + 分隔符归属哪一侧"：分隔符留在左侧时用前瞻匹配
// （Go 的 regexp 不支持前瞻，所以用 FindAllStringIndex 自己切），
// 这样空白会归到右侧、句末标点归到左侧，拼接回来与原文完全一致。
var splitLevels = []struct {
	pattern  string
	keepLeft bool // true：匹配到的文本归左段；false：归右段
}{
	{`\n+`, true},               // 段落
	{`[.!?。！？]+[ \t]*`, false},  // 句末标点 + 尾随空白 → 空白给下一段
	{`[;；,，、:：]+[ \t]*`, false}, // 分句
	{`[ \t]+`, false},           // 词
}

// splitWithLevel 从第 level 级切分点开始切分。
//
// 某一级切完之后如果还有超长片段，要用**更细的下一级**继续切，
// 而不是把片段原样再喂给同一级——那等于什么都没做，还会无限递归。
func splitWithLevel(text string, level, maxChars int) []string {
	if runeLen(text) <= maxChars {
		return []string{text}
	}
	if level >= len(splitLevels) {
		return hardCut(text, maxChars) // 连一个词都超长，只能按字符数切
	}

	pieces := splitAtPattern(text, splitLevels[level].pattern, splitLevels[level].keepLeft, maxChars)
	if len(pieces) <= 1 {
		return splitWithLevel(text, level+1, maxChars) // 这一级切不动，换更细的
	}

	segments := make([]string, 0, len(pieces))
	for _, piece := range packPieces(pieces, maxChars) {
		if runeLen(piece) <= maxChars {
			segments = append(segments, piece)
			continue
		}
		segments = append(segments, splitWithLevel(piece, level+1, maxChars)...)
	}
	return segments
}

// splitAtPattern 按 pattern 把文本切成"匹配到的边界"与"边界之间的内容"。
//
// keepLeft 为 true 时边界的空白归左侧（例如段落分隔的换行），
// 为 false 时归右侧（例如句末句号留在左句、其后的空格归右句）。
func splitAtPattern(text, pattern string, keepLeft bool, maxChars int) []string {
	matches := regexp.MustCompile(pattern).FindAllStringIndex(text, -1)
	if len(matches) == 0 {
		return []string{text}
	}

	pieces := make([]string, 0, len(matches)*2+1)
	pos := 0
	for _, m := range matches {
		start, end := m[0], m[1]
		// 一段"内容 + 边界"整体就已超限时，这个边界帮不上忙（切了也没用），
		// 跳过它，让更细的切分点来处理。
		if start-pos+runeLen(text[start:end]) > maxChars {
			continue
		}
		if keepLeft {
			if s := text[pos:end]; s != "" {
				pieces = append(pieces, s)
			}
		} else {
			if s := text[pos:start]; s != "" {
				pieces = append(pieces, s)
			}
			if s := text[start:end]; s != "" {
				pieces = append(pieces, s)
			}
		}
		pos = end
	}
	if pos < len(text) {
		pieces = append(pieces, text[pos:])
	}
	return pieces
}

// packPieces 把相邻 fragment 顺序装进分段，尽量塞满但不超过 maxChars。
//
// 不处理"单个 fragment 就超长"的情况：那种片段留给调用方换更细的切分点，
// 因为在这里硬切会切断分隔符。
func packPieces(pieces []string, maxChars int) []string {
	var (
		chunks  []string
		current strings.Builder
		size    int
	)
	for _, piece := range pieces {
		n := runeLen(piece)
		if size > 0 && size+n > maxChars {
			chunks = append(chunks, current.String())
			current.Reset()
			size = 0
		}
		current.WriteString(piece)
		size += n
	}
	if size > 0 {
		chunks = append(chunks, current.String())
	}
	return chunks
}

// hardCut 按字符数硬切（最后一个手段：连一个"词"都超过上限时）。
func hardCut(text string, maxChars int) []string {
	var (
		chunks  []string
		current []rune
	)
	for _, r := range text {
		current = append(current, r)
		if len(current) == maxChars {
			chunks = append(chunks, string(current))
			current = current[:0]
		}
	}
	if len(current) > 0 {
		chunks = append(chunks, string(current))
	}
	return chunks
}
