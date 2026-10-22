package embedutil

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"litstream/domain"
	"math"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
	openai "github.com/sashabaranov/go-openai"
	"golang.org/x/sync/singleflight"
)

type Embedding struct {
	vecDim     int
	modelName  string
	client     *openai.Client
	cache      *redis.Client
	gsf        *singleflight.Group
	expiration time.Duration
}

// 设置user summary的vector的存活时间
func NewEmbedding(apiKey string, baseURL string,
	vecDim int, redisClient *redis.Client,
	infoTTL time.Duration, modelName string) *Embedding {
	config := openai.DefaultConfig(apiKey)
	// 阿里云兼容接口地址
	config.BaseURL = baseURL
	// 设置超时时间 10s
	config.HTTPClient = &http.Client{Timeout: time.Second * 10}

	client := openai.NewClientWithConfig(config)

	return &Embedding{
		vecDim:     vecDim,
		client:     client,
		cache:      redisClient,
		gsf:        &singleflight.Group{},
		expiration: infoTTL,
		modelName:  modelName,
	}
}

func (e *Embedding) Encode(ctx context.Context, text string, uid uint) ([]float32, error) {
	// 调用远程模型
	vec, err := DoCtx(ctx, e.gsf, fmt.Sprintf("%d", uid), func(ctx context.Context) ([]float32, error) {
		vec, err := e.encodeRemote(ctx, text)
		if err == nil {
			// 缓存结果
			e.setCache(ctx, uid, vec)
		}
		return vec, err
	})
	if err != nil {
		return nil, err
	}
	return vec, nil
}

func (e *Embedding) GetVec(ctx context.Context, test string, uid uint) ([]float32, error) {
	// 先查缓存
	v, err := e.getCache(ctx, uid)
	if err == nil && v != nil && len(v) == e.vecDim {
		return v, nil
	}
	// 缓存未命中，计算向量
	vec, err := e.Encode(ctx, test, uid)
	if err != nil {
		return nil, err
	}
	return vec, nil
}

func encodeVec(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(x))
	}
	return b
}

// []byte → []float32，输入原始字节
func decodeVec(b []byte) ([]float32, error) {
	if len(b)%4 != 0 {
		return nil, fmt.Errorf("invalid byte length %d, must multiple of 4", len(b))
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		u := binary.LittleEndian.Uint32(b[4*i:])
		v[i] = math.Float32frombits(u)
	}
	return v, nil
}

func (e *Embedding) getCache(ctx context.Context, uid uint) ([]float32, error) {
	vec, err := e.cache.Get(ctx, domain.KeyTopicsVec(uid)).Result()
	if err != nil {
		return nil, err
	}
	v, err := decodeVec([]byte(vec))
	if err != nil {
		return nil, err
	}
	if len(v) != e.vecDim {
		return nil, fmt.Errorf("缓存的向量维度不符: got %d want %d", len(v), e.vecDim)
	}
	_ = e.cache.Expire(ctx, domain.KeyTopicsVec(uid), e.expiration).Err()
	return v, nil
}

func (e *Embedding) setCache(ctx context.Context, uid uint, vec []float32) error {
	err := e.cache.Set(ctx, domain.KeyTopicsVec(uid), encodeVec(vec), e.expiration).Err()
	if err != nil {
		return err
	}
	return nil
}

// 远程调用 embedding 模型（OpenAI兼容）进行文本向量化
func (e *Embedding) encodeRemote(ctx context.Context, text string) ([]float32, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Second*15)
	defer cancel()
	req := openai.EmbeddingRequest{
		Model: openai.EmbeddingModel(e.modelName),
		Input: []string{
			text,
		},
		Dimensions: e.vecDim,
	}

	resp, err := e.client.CreateEmbeddings(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("embedding 调用失败: %w", err)
	}
	if len(resp.Data) == 0 {
		return nil, errors.New("embedding 返回空 data")
	}
	vec := resp.Data[0].Embedding
	if len(vec) != e.vecDim {
		return nil, fmt.Errorf("embedding 维度不符: got %d want %d", len(vec), e.vecDim)
	}

	return resp.Data[0].Embedding, nil
}

func DoCtx[T any](ctx context.Context, g *singleflight.Group, key string, fn func(context.Context) (T, error)) (T, error) {
	ch := g.DoChan(key, func() (any, error) {
		// 关键一行：把 fn 的 ctx 从"某个调用者的请求生命周期"上摘下来。
		// 不加这行，第一个调用者被取消/超时，所有等待者会一起失败。
		return fn(context.WithoutCancel(ctx))
	})
	select {
	case r := <-ch:
		if r.Err != nil {
			var zero T
			return zero, r.Err
		}
		if r.Val == nil {
			var zero T
			return zero, nil
		}
		t, ok := r.Val.(T)
		if !ok {
			var zero T
			return zero, fmt.Errorf("singleflight: key=%q 的返回值类型是 %T", key, r.Val)
		}
		return t, nil
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err() // 自己走人，不拖累别人
	}
}
