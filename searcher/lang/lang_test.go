package lang

import "testing"

func TestIsMostlyChinese(t *testing.T) {
	cases := []struct {
		name      string
		text      string
		threshold float64
		want      bool
	}{
		{"纯中文", "本文提出了一种新的检索增强生成方法。", 0.5, true},
		{"纯英文", "We propose a new retrieval augmented generation method.", 0.5, false},
		{"空文本", "", 0.5, false},
		{"只有空白", "  \n\t ", 0.5, false},
		{"中英混排且中文过半", "本文提出了 RAG 方法，用于提升检索的准确率。", 0.5, true},
		{"中英混排且英文过半", "This paper proposes a 检索 method for RAG systems and evaluation.", 0.5, false},
		// 关键回归点：按字节统计时纯中文占比只有 1/3，会被误判为非中文。
		{"单个汉字", "中", 0.5, true},
		{"较长中文摘要", "针对现有方法在长文档检索中召回率偏低的问题，本文提出一种分层索引结构，并在多个数据集上验证了有效性。", 0.5, true},
		{"阈值边界", "中文ab", 0.5, true},  // 2/4 = 0.5，取等号
		{"阈值偏高", "中文ab", 1.0, false}, // 0.5 < 1.0
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsMostlyChinese(tc.text, tc.threshold); got != tc.want {
				t.Errorf("IsMostlyChinese(%q, %v) = %v, want %v", tc.text, tc.threshold, got, tc.want)
			}
		})
	}
}
