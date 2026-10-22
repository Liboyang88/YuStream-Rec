package embedder

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// TestEmbedBatching 用 mock 的 DashScope 兼容接口验证：
// 1) 单次请求的 input 不超过 BatchSize；
// 2) 分批后所有文本都拿到向量，且按原顺序归位；
// 3) 请求次数等于分批数。
func TestEmbedBatching(t *testing.T) {
	const dim = 4
	const extra = 5               // 最后一批不足整批
	const n = 2*BatchSize + extra // 至少覆盖"整批 + 余批"
	wantCalls := (n + BatchSize - 1) / BatchSize

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
			Model      string   `json:"model"`
			Input      []string `json:"input"`
			Dimensions int      `json:"dimensions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, err.Error(), 400)
			return
		}
		if body.Model != DefaultModel {
			t.Errorf("model = %q, want %q", body.Model, DefaultModel)
		}

		mu.Lock()
		call := callN
		callN++
		if len(body.Input) > maxBatch {
			maxBatch = len(body.Input)
		}
		mu.Unlock()

		if len(body.Input) > BatchSize {
			t.Errorf("批次 %d 输入 %d 条，超过上限 %d", call, len(body.Input), BatchSize)
		}

		// 返回该批向量；值 = 全局序号+1，便于校验顺序
		data := make([]map[string]any, 0, len(body.Input))
		for i, txt := range body.Input {
			global := call*BatchSize + i
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

	e := New("test-key", srv.URL+"/v1", DefaultModel, dim)

	texts := make([]string, n)
	for i := range texts {
		texts[i] = fmt.Sprintf("text-%d", i)
	}

	vecs, err := e.Embed(context.Background(), texts)
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
	if maxBatch > BatchSize {
		t.Errorf("最大批次 %d 超过上限 %d", maxBatch, BatchSize)
	}
	if callN != wantCalls {
		t.Errorf("请求次数 %d，want %d（%d 条按每批 %d 条拆分）", callN, wantCalls, n, BatchSize)
	}
}

// TestEmbedEmpty 空输入不报错，也不发起请求。
func TestEmbedEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "should not be called", 500)
	}))
	defer srv.Close()

	e := New("test-key", srv.URL+"/v1", DefaultModel, 4)
	vecs, err := e.Embed(context.Background(), nil)
	if err != nil {
		t.Fatalf("Embed(nil): %v", err)
	}
	if len(vecs) != 0 {
		t.Fatalf("len = %d, want 0", len(vecs))
	}
}

// TestEmbedAllIndexZero 批量输入时接口可能对每一条都返回 index=0。
//
// 这是百炼兼容接口的实测行为（qwen3.7-text-embedding-flash）：data[0].index 与
// data[1].index 都是 0。此时**必须按返回顺序归位**——若照 index 归位，
// 下标 0 会被反复覆盖、其余位置留空，整批向量静默丢失（nil 向量要到写 Milvus 时才报错）。
func TestEmbedAllIndexZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]any{
				{"object": "embedding", "index": 0, "embedding": []float32{1, 0, 0, 0}},
				{"object": "embedding", "index": 0, "embedding": []float32{0, 1, 0, 0}},
				{"object": "embedding", "index": 0, "embedding": []float32{0, 0, 1, 0}},
			},
			"model": DefaultModel,
		})
	}))
	defer srv.Close()

	e := New("test-key", srv.URL+"/v1", DefaultModel, 4)
	vecs, err := e.Embed(context.Background(), []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vecs) != 3 {
		t.Fatalf("返回 %d 个向量, want 3", len(vecs))
	}
	for i, v := range vecs {
		if len(v) != 4 {
			t.Fatalf("vec[%d] 维度 = %d, want 4（有位置没填上向量）", i, len(v))
		}
	}
	// 三条输入必须拿到三个互不相同的向量，且顺序与输入一致。
	if vecs[0][0] != 1 || vecs[1][1] != 1 || vecs[2][2] != 1 {
		t.Errorf("向量与输入顺序不一致: %v %v %v", vecs[0], vecs[1], vecs[2])
	}
}

// TestEmbedHonorsValidPermutationIndex index 构成合法排列时仍按 index 归位
// （服务端真正表达"乱序返回"的约定）。
func TestEmbedHonorsValidPermutationIndex(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]any{
				// 乱序返回：先给第 2 条（index=1），再给第 1 条（index=0）
				{"object": "embedding", "index": 1, "embedding": []float32{0, 1, 0, 0}},
				{"object": "embedding", "index": 0, "embedding": []float32{1, 0, 0, 0}},
			},
			"model": DefaultModel,
		})
	}))
	defer srv.Close()

	e := New("test-key", srv.URL+"/v1", DefaultModel, 4)
	vecs, err := e.Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if vecs[0][0] != 1 || vecs[1][1] != 1 {
		t.Errorf("应按 index 归位，实际 %v %v", vecs[0], vecs[1])
	}
}

// TestEmbedDimensionMismatch 维度不符时应报错，避免写入错误长度的向量。
func TestEmbedDimensionMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]any{
				{"object": "embedding", "index": 0, "embedding": []float32{1, 2}}, // 只有 2 维
			},
			"model": DefaultModel,
		})
	}))
	defer srv.Close()

	e := New("test-key", srv.URL+"/v1", DefaultModel, 4)
	if _, err := e.Embed(context.Background(), []string{"x"}); err == nil {
		t.Fatal("期望维度不符时报错，实际返回 nil")
	}
}
