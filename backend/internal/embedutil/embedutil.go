package embedutil

import (
	"context"
	"dcpro/domain"
	openai "github.com/sashabaranov/go-openai"
)

type Embedding struct {
	vecDim int
	client *openai.Client
}

func NewEmbedding(env *domain.Env) *Embedding {
	apiKey := env.EmbeddingModelApiKey
	config := openai.DefaultConfig(apiKey)
	// 阿里云兼容接口地址
	config.BaseURL = env.EmbeddingModelBaseURL

	client := openai.NewClientWithConfig(config)

	return &Embedding{
		vecDim: env.VecDim,
		client: client,
	}
}

func (e *Embedding) Encode(text string) ([]float32, error) {
	req := openai.EmbeddingRequest{
		Model: openai.EmbeddingModel("text-embedding-v4"),
		Input: []string{
			text,
		},
		Dimensions: e.vecDim,
	}

	resp, err := e.client.CreateEmbeddings(context.Background(), req)
	if err != nil {
		return nil, err
	}

	vec := resp.Data[0].Embedding

	return vec, nil
}