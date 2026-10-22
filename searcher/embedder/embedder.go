// Package embedder 把文本编码为向量（与后端使用同一个 embedding 模型）。
package embedder

import (
	"context"
	"fmt"
	"strings"

	openai "github.com/sashabaranov/go-openai"
)

const (
	// DefaultModel 是未在 .env 中配置 EMBEDDING_MODEL_NAME 时的兜底模型名。
	// 必须与 litstream 后端 embedutil 实际使用的模型一致，否则向量不在同一语义空间，
	// 检索会失效——因此优先从 .env 读取，与后端同源。
	//
	// 该模型默认输出 1024 维，与 VEC_DIM 一致；也支持通过 dimensions 指定
	// 256/512/768/1024（本包总是显式传 dim，避免依赖服务端默认值）。
	DefaultModel = "qwen3.7-text-embedding-flash"

	// BatchSize 是单次 embedding 请求的批量上限：百炼兼容接口实测最多接受 25 条，
	// 26 条会返回 400（InternalError.Algo.InvalidParameter: batch size is invalid）。
	// 这里取 10 留出余量，也让单次请求失败时重试的代价更小。
	BatchSize = 10
)

// Embedder 封装 embedding 接口。
type Embedder struct {
	client    *openai.Client
	modelName string
	dim       int
}

// New 构造 Embedder；baseURL 为 OpenAI 兼容接口地址，modelName 为空时使用 DefaultModel，
// dim 为向量维度。
func New(apiKey, baseURL, modelName string, dim int) *Embedder {
	if strings.TrimSpace(modelName) == "" {
		modelName = DefaultModel
	}
	cfg := openai.DefaultConfig(apiKey)
	cfg.BaseURL = baseURL
	return &Embedder{client: openai.NewClientWithConfig(cfg), modelName: modelName, dim: dim}
}

// Dim 返回向量维度。
func (e *Embedder) Dim() int { return e.dim }

// Model 返回使用的 embedding 模型名。
func (e *Embedder) Model() string { return e.modelName }

// Embed 分批编码，返回与输入等长的向量列表（每个长度为 dim）。
// 单次请求最多 BatchSize 条，超过会自动拆成多个请求并按 index 归位。
func (e *Embedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	vecs := make([][]float32, len(texts))
	for start := 0; start < len(texts); start += BatchSize {
		end := min(start+BatchSize, len(texts))
		batch := texts[start:end]

		resp, err := e.client.CreateEmbeddings(ctx, openai.EmbeddingRequest{
			Model:      openai.EmbeddingModel(e.modelName),
			Input:      batch,
			Dimensions: e.dim,
		})
		if err != nil {
			return nil, fmt.Errorf("embedding 批次 [%d:%d] 失败: %w", start, end, err)
		}
		if len(resp.Data) != len(batch) {
			return nil, fmt.Errorf("embedding 返回数量不符: 批次 %d 条输入, 返回 %d", len(batch), len(resp.Data))
		}
		// 归位规则：只有 index 构成一个合法排列（互不相同且在范围内）时才按 index 排，
		// 否则一律按返回顺序一一对应。
		//
		// 为什么不能无条件按 index：百炼兼容接口（实测 qwen3.7-text-embedding-flash）
		// 对批量输入**每一条都返回 index=0**。无条件按 index 归位会把下标 0 反复覆盖、
		// 其余位置留空，等于把整批向量弄丢。OpenAI 的约定本就是 data 顺序与输入顺序一致。
		useIndex := true
		seen := make(map[int]struct{}, len(batch))
		for _, d := range resp.Data {
			if d.Index < 0 || d.Index >= len(batch) {
				useIndex = false
				break
			}
			if _, dup := seen[d.Index]; dup {
				useIndex = false
				break
			}
			seen[d.Index] = struct{}{}
		}

		for i, d := range resp.Data {
			if len(d.Embedding) != e.dim {
				return nil, fmt.Errorf("embedding 维度不符（第 %d 条）: 得到 %d, 期望 %d", i, len(d.Embedding), e.dim)
			}
			if useIndex {
				vecs[start+d.Index] = d.Embedding
				continue
			}
			vecs[start+i] = d.Embedding
		}
	}
	return vecs, nil
}
