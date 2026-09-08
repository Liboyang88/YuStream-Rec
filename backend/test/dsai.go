package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
)

type LLMClient struct {
	apiKey string
	modelName string
	tools string
	toolChoice string
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}
type ToolCall struct {
	ID       string        `json:"id"`
	Type     string        `json:"type"`
	Function FunctionCall `json:"function"`
}
type Message struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	ReasoningContent string     `json:"reasoning_content"`
	ToolCalls []ToolCall `json:"tool_calls"`
}
type Choice struct {
	Index   int     `json:"index"`
	Message Message `json:"message"`
	FinishReason string `json:"finish_reason"`
}
type Usage struct {
	PromptTokens int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens int `json:"total_tokens"`
	PromptCacheHitTokens int `json:"prompt_cache_hit_tokens"`
	PromptCacheMissTokens int `json:"prompt_cache_miss_tokens"`
}
type RespData struct {
	ID string `json:"id"`
	Object string `json:"object"`
	Created int64 `json:"created"`
	Model string `json:"model"`
	Choices []Choice `json:"choices"`
	Usage Usage `json:"usage"`
}

func (cli *LLMClient) Send(userId string, message string, resp *RespData) error {
	url := "https://api.deepseek.com/chat/completions"
	method := "POST"

	reqStr := fmt.Sprintf(`{
		"messages": %s,
		"thinking":{"type":"disabled"},
		"model": "%s",
		"max_tokens": 4096,
		"temperature": 1,
		"top_p": 1,
		"stream": false
	}`,message, cli.modelName)

	payload := strings.NewReader(reqStr)

	client := &http.Client{}
	req, err := http.NewRequest(method, url, payload)
	if err != nil {
		fmt.Printf("创建请求失败: %v\n", err)
		return err
	}

	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", cli.apiKey))

	res, err := client.Do(req)
	if err != nil {
		fmt.Printf("请求发送失败: %v\n", err)
		return err
	}
	defer res.Body.Close()

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		fmt.Printf("读取响应失败: %v\n", err)
		return err
	}
	fmt.Println(string(body))

	if err := json.Unmarshal(body, resp); err != nil {
		fmt.Printf("解析失败: %v\n原始响应：%s\n", err, string(body))
		return err
	}
	return nil
}

func main(){
	cli := LLMClient{
		apiKey: "sk-47056955fdb049d3898f3228b3db1e1b",
		modelName: "deepseek-v4-pro",
	}
	cli.Send("1", `[{"role":"user","content":"给人工智能发展这篇文章提取关键词和摘要"}]`, nil)
}