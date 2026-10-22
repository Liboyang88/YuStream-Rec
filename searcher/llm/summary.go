package llm

import (
	"context"
	"fmt"
	"strings"

	"searcher/retry"
)

// promptGenerateSummary 是生成文献介绍的系统提示词。
//
// 输出形态由提示词约束，不再用 tool calling 强制：
// tool calling 会额外产生一次"函数调用"往返（以及 arguments 的 JSON 转义开销），
// 而服务的 TPM 是每分钟 5M（qwen3.8-flash），批量富化时额度比往返次数更紧张。
// 因此这里把"只输出一段连续文本、不要任何包装"写死在提示词里，
// 并在 extractSummary 再做一层剥离，防止模型偶尔加上标题或代码块围栏。
const promptGenerateSummary = `下面是一篇论文的原始摘要，请完成信息提取，不要改写专业名词、不要编造内容、不要增加额外推测。
只输出一段连续文本，用于embedding向量入库。
保留内容：
1. 本文研究的核心问题与研究对象
2. 本文提出的方法/模型/实验方案
3. 核心实验结果与关键结论

删除内容：
1. 领域背景铺垫、行业现状套话
2. 研究局限、未来展望
3. 泛泛的前人工作综述、无价值评价
4. 冗余修饰语句

约束：
- 尽量使用原文术语，不要同义替换专业词汇
- 直接输出正文本身，不要任何前后缀、标题、序号、解释或客套话
- 不要复述上面的"保留内容/删除内容"条目，也不要出现"核心问题为""研究对象为""方法上采用"这类提示词中的标签，把它们融进通顺的句子里
- 不要使用 Markdown 语法（不要 **加粗**、不要 # 标题、不要列表符号）
- 不要输出 JSON、XML 或代码块围栏（不要 ` + "```" + `）
- 保持简洁，控制在500token以内
- 禁止幻觉，原文没有的内容不能添加
`

// Summarizer 根据标题与摘要生成简短的文献介绍。
type Summarizer struct {
	client *Client
}

// NewSummarizer 构造文献介绍生成器。
// opts 透传给底层 chat 客户端（例如 WithSemaphore 限制并发、WithRetryConfig 改重试）。
func NewSummarizer(apiKey, modelName string, opts ...Option) *Summarizer {
	return &Summarizer{client: NewClient(apiKey, modelName, opts...)}
}

// GenerateSummary 生成文献介绍。返回空串表示模型未给出有效内容。
//
// 调用失败（网络错误、非 2xx、内容为空、被输出上限截断）都会按客户端配置重试，
// 默认失败后再试 3 次；全部失败才把最后一次的错误返回给调用方。
func (s *Summarizer) GenerateSummary(ctx context.Context, title, abstract string) (string, error) {
	messages := []ChatMessage{
		{Role: "system", Content: promptGenerateSummary},
		{Role: "user", Content: fmt.Sprintf("标题：%s\n摘要：%s", title, abstract)},
	}

	var summary string
	err := retry.Do(ctx, s.client.retry, "生成文献介绍", func() error {
		resp, err := s.client.Send(ctx, messages)
		if err != nil {
			return err
		}
		summary, err = extractSummary(resp)
		if err != nil {
			return err
		}
		// 空内容同样视为失败，交给重试；都失败时 ingest 会回退为摘要原文。
		if strings.TrimSpace(summary) == "" {
			return fmt.Errorf("模型返回的文献介绍为空")
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(summary), nil
}

// extractSummary 从 chat 响应里取出文献介绍正文。
//
// 提示词已经要求"只输出正文"，这里再兜一层：模型偶尔会多给一段前言、
// 用代码块围栏包住结果，或把内容写进 reasoning_content（思考模式没关干净时）。
// 这些都是"看起来成功、实际半成品"的情况，宁可报错重试，也不要让脏文本进向量库。
func extractSummary(resp *RespData) (string, error) {
	if resp == nil || len(resp.Choices) == 0 {
		return "", fmt.Errorf("LLM 未返回任何候选: %+v", resp)
	}

	choice := resp.Choices[0]
	content := strings.TrimSpace(choice.Message.Content)

	// 被输出上限截断的文本是半截话，进向量库只会污染检索结果。
	if choice.FinishReason == "length" {
		return "", fmt.Errorf("文献介绍被 max_tokens 截断（finish_reason=length），请调大 MaxTokens 或收紧提示词")
	}

	if content == "" {
		if reasoning := strings.TrimSpace(choice.Message.ReasoningContent); reasoning != "" {
			return "", fmt.Errorf("文献介绍为空，但模型把内容写进了 reasoning_content（思考模式未关闭）: %.200s", reasoning)
		}
		return "", fmt.Errorf("LLM 返回的文献介绍为空")
	}

	return sanitizeSummary(content), nil
}

// sanitizeSummary 兜底清理：剥掉代码块围栏与常见的前缀标签。
//
// 只做"包裹层"的剥离，不动正文内容本身——提示词已经约束了格式，
// 这里多做的每一步都有可能误删真实的正文。
func sanitizeSummary(content string) string {
	out := strings.TrimSpace(content)

	// 去掉 ```lang ... ``` 围栏（模型最常加的包装）。
	if strings.HasPrefix(out, "```") {
		lines := strings.Split(out, "\n")
		if len(lines) >= 2 {
			lines = lines[1:] // 丢掉开围栏
			if last := strings.TrimSpace(lines[len(lines)-1]); strings.HasPrefix(last, "```") {
				lines = lines[:len(lines)-1] // 丢掉闭围栏
			}
			out = strings.TrimSpace(strings.Join(lines, "\n"))
		}
	}

	// 去掉"文献介绍：""摘要：""总结："这类前缀标签。
	for _, prefix := range []string{"文献介绍：", "文献介绍:", "摘要：", "摘要:", "总结：", "总结:", "概括：", "概括:"} {
		if strings.HasPrefix(out, prefix) {
			out = strings.TrimSpace(strings.TrimPrefix(out, prefix))
			break
		}
	}

	return strings.TrimSpace(out)
}
