package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// TestEmbedBatching 用 mock 的 DashScope 兼容接口验证：
// 1) 单次请求的 input 不超过 20 条；
// 2) 分批后所有文本都拿到向量，且按原顺序归位。
func TestEmbedBatching(t *testing.T) {
	const dim = 4
	var (
		mu       sync.Mutex
		callN    int
		maxBatch int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		var body struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, err.Error(), 400)
			return
		}
		if body.Model != embeddingModel {
			t.Errorf("model = %q, want %q", body.Model, embeddingModel)
		}

		mu.Lock()
		call := callN
		callN++
		if len(body.Input) > maxBatch {
			maxBatch = len(body.Input)
		}
		mu.Unlock()
		if len(body.Input) > embeddingBatchSize {
			t.Errorf("批次 %d 输入 %d 条，超过上限 %d", call, len(body.Input), embeddingBatchSize)
		}

		// 返回该批向量；值 = 全局序号+1，便于校验顺序
		data := make([]map[string]any, 0, len(body.Input))
		for i, txt := range body.Input {
			global := call*embeddingBatchSize + i
			if want := fmt.Sprintf("text-%d", global); txt != want {
				t.Errorf("批次 %d 第 %d 条 = %q, want %q", call, i, txt, want)
			}
			vec := make([]float32, dim)
			vec[0] = float32(global + 1)
			data = append(data, map[string]any{
				"object":    "embedding",
				"index":     i,
				"embedding": vec,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data":   data,
			"model":  body.Model,
			"usage":  map[string]int{"prompt_tokens": 1, "total_tokens": 1},
		})
	}))
	defer srv.Close()

	e := NewEmbedder("test-key", srv.URL+"/v1", dim)

	const n = 45 // 45 = 2 整批(20) + 1 余批(5)
	texts := make([]string, n)
	for i := range texts {
		texts[i] = fmt.Sprintf("text-%d", i)
	}
	vecs, err := e.Embed(texts)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vecs) != n {
		t.Fatalf("返回 %d 个向量，want %d", len(vecs), n)
	}
	for i, v := range vecs {
		if len(v) != dim {
			t.Fatalf("向量 %d 维度 %d，want %d", i, len(v), dim)
		}
		if v[0] != float32(i+1) {
			t.Errorf("向量 %d 首位 = %v, want %d（顺序错乱）", i, v[0], i+1)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if maxBatch > embeddingBatchSize {
		t.Errorf("最大批次 %d 超过上限 %d", maxBatch, embeddingBatchSize)
	}
	if callN != 3 {
		t.Errorf("请求次数 %d，want 3（20+20+5）", callN)
	}
}

// TestEmbedEmpty 空输入不报错。
func TestEmbedEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "should not be called", 500)
	}))
	defer srv.Close()
	e := NewEmbedder("test-key", srv.URL+"/v1", 4)
	vecs, err := e.Embed(nil)
	if err != nil {
		t.Fatalf("Embed(nil): %v", err)
	}
	if len(vecs) != 0 {
		t.Fatalf("len = %d, want 0", len(vecs))
	}
}
