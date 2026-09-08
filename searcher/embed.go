package main

import (
	"context"
	"fmt"

	openai "github.com/sashabaranov/go-openai"
)

// embeddingModel 必须与 dcpro/internal/embedutil 里 Encode 用的模型一致，
// 否则向量不在同一语义空间，检索会失效。
const embeddingModel = "text-embedding-v4"

// embeddingBatchSize：阿里云 DashScope 兼容接口单次 embedding 请求的批量上限，
// 超过 20 条会返回 400（InternalError.Algo.InvalidParameter: batch size is invalid）。
const embeddingBatchSize = 10

type Embedder struct {
	client *openai.Client
	dim    int
}

func NewEmbedder(apiKey, baseURL string, dim int) *Embedder {
	cfg := openai.DefaultConfig(apiKey)
	cfg.BaseURL = baseURL // 阿里云 DashScope 兼容接口
	return &Embedder{client: openai.NewClientWithConfig(cfg), dim: dim}
}

// Embed 分批编码，返回与输入等长的向量列表（每个长度为 dim）。
// 单次请求最多 embeddingBatchSize 条，超过会自动拆成多个请求并按 index 归位。
func (e *Embedder) Embed(texts []string) ([][]float32, error) {
	vecs := make([][]float32, len(texts))
	for start := 0; start < len(texts); start += embeddingBatchSize {
		end := start + embeddingBatchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch := texts[start:end]

		req := openai.EmbeddingRequest{
			Model:      openai.EmbeddingModel(embeddingModel),
			Input:      batch,
			Dimensions: e.dim,
		}
		resp, err := e.client.CreateEmbeddings(context.Background(), req)
		if err != nil {
			return nil, fmt.Errorf("embedding 批次 [%d:%d] 失败: %w", start, end, err)
		}
		if len(resp.Data) != len(batch) {
			return nil, fmt.Errorf("embedding 返回数量不符: 批次 %d 条输入, 返回 %d", len(batch), len(resp.Data))
		}
		for _, d := range resp.Data {
			if d.Index < 0 || d.Index >= len(batch) {
				return nil, fmt.Errorf("embedding 返回非法 index: %d", d.Index)
			}
			if len(d.Embedding) != e.dim {
				return nil, fmt.Errorf("embedding 维度不符: 得到 %d, 期望 %d", len(d.Embedding), e.dim)
			}
			vecs[start+d.Index] = d.Embedding
		}
	}
	return vecs, nil
}
