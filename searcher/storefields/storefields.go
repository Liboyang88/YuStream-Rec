// Package storefields 定义"重新生成"时需要回写的字段集合。
//
// 放在独立包里的原因：写入方是 store（把它翻译成 UPDATE 语句与 Milvus 列），
// 生产方是 regen（它算这些值），两边必须用同一组类型才能直接在接口上对接。
// 若各自声明一份，装配处就得逐条转换，多一层纯粹的样板代码。
package storefields

// TranslationUpdate 是一条记录重新翻译后的中文字段。
type TranslationUpdate struct {
	ID              uint
	TitleChinese    string
	AbstractChinese string
	IsChinese       bool
}

// SummaryUpdate 是一条记录重新生成后的文献介绍。
type SummaryUpdate struct {
	ID      uint
	Summary string
}

// VectorUpdate 是一条记录重新生成后的向量。
type VectorUpdate struct {
	ID  uint
	Vec []float32
}
