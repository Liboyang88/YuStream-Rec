package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"litstream/domain"
	"litstream/pkg"
	"litstream/pkg/mylog"
	"log"
	"strings"
)

var (
	ToolRecommend = "recommend"
	ToolUS        = "extract_user_profile"
)

// ---- 工具 parameters 的 JSON Schema ----
// 结构化定义后作为 ToolFunction.Parameters 传入，由 json.Marshal 统一序列化。

// recommendItems recommend 工具 items_list 数组中元素的 schema
type recommendItems struct {
	Type        string `json:"type"`
	Description string `json:"description"`
	Minimum     int    `json:"minimum"`
	Maximum     int    `json:"maximum"`
}

func BuildToolRecommend(totalLen int, reLen int) []pkg.Tool {
	return []pkg.Tool{
		{
			Type: "function",
			Function: pkg.ToolFunction{
				Name:        ToolRecommend,
				Description: fmt.Sprintf("根据用户信息与物品列表中的物品描述，决定将哪些物品推荐给用户。注意：备选的物品列表长度为%d，最终决定推荐的物品列表长度为%d；且你只需返回一个JSON数组，只包含备选物品列表中的序号ID。", totalLen, reLen),
				Parameters: pkg.ToolParameters{
					Type:     "object",
					Required: []string{"items_list", "reason"},
					Properties: map[string]any{
						"items_list": pkg.FieldWithItems{
							Type:        "array",
							Description: fmt.Sprintf("最终决定推荐给用户的物品序号ID列表，长度为%d", reLen),
							Items: recommendItems{
								Type:        "integer",
								Description: "物品ID",
								Minimum:     1,
								Maximum:     totalLen,
							},
						},
						"outline": pkg.CommonField{
							Type:        "string",
							Description: "推荐物品的概要描述",
						},
					},
				},
			},
		},
	}
}

func BuildToolUS() []pkg.Tool {
	return []pkg.Tool{
		{
			Type: "function",
			Function: pkg.ToolFunction{
				Name:        ToolUS,
				Description: "根据用户信息生成自然语言描述和概括的用户画像",
				Parameters: pkg.ToolParameters{
					Type:     "object",
					Required: []string{"summary", "interest_topics"},
					Properties: map[string]any{
						"summary": pkg.CommonField{
							Type:        "string",
							Description: "由自然语言描述的用户画像",
						},
						"interest_topics": pkg.CommonField{
							Type:        "string",
							Description: "用户感兴趣的论题或话题",
						},
					},
				},
			},
		},
	}
}

func BuildToolChoice(toolName string) *pkg.ToolChoice {
	return &pkg.ToolChoice{
		Type: "function",
		Function: pkg.ToolChoiceFunction{
			Name: toolName,
		},
	}
}

type LLMInvoking struct {
	cli *pkg.LLMClient
}

func NewLLMInvoking(ApiKey string,
	ModelName string) *LLMInvoking {
	return &LLMInvoking{cli: pkg.NewLLMClient(ApiKey, ModelName)}
}

func (w *LLMInvoking) GenerateUserProfile(ctx context.Context, user *domain.User) (string, error) {
	userStr := fmt.Sprintf(
		`用户名：%s
		年龄：%d
		性别：%s
		工作：%s
		教育背景：%s
		地理位置：%s
		所在组织：%s
		感兴趣的领域：%s
		`, user.Username, user.Age, user.Gender, user.Job, user.Education,
		user.Location, user.Organization, strings.Join(user.InterestingFields, ","))

	messages := []pkg.ChatMessage{
		{Role: "system", Content: PromptGenerateUS},
		{Role: "user", Content: fmt.Sprintf("以下为该用户信息，请进行描述和提炼：%s", userStr)},
	}

	resp := &pkg.RespData{}
	err := w.cli.Send(ctx, messages, BuildToolUS(), BuildToolChoice(ToolUS), resp)
	log.Println("LLM resp:", resp)
	if err != nil {
		return "", err
	}
	summary, interestTopics, err := extractSummaryAndTopics(resp)
	if err != nil {
		return "", err
	}
	user.Summary = summary

	return interestTopics, nil
}

func (w *LLMInvoking) Recommend(
	ctx context.Context, userID uint, userProfile string, interestTopics string,
	items *[]domain.Item, totalLen int, reLen int) (string, error) {
	var builder strings.Builder
	for ind, item := range *items {
		builder.WriteString(fmt.Sprintf("%d. %s\n", ind+1, item.Summary))
	}
	itemsSummary := builder.String()

	messages := []pkg.ChatMessage{
		{Role: "system", Content: fmt.Sprintf(PromptRecommend, reLen)},
		{Role: "user", Content: fmt.Sprintf(PromptRecommendUser, reLen, userProfile, interestTopics, itemsSummary)},
	}

	resp := &pkg.RespData{}
	err := w.cli.Send(ctx, messages, BuildToolRecommend(totalLen, reLen), BuildToolChoice(ToolRecommend), resp)
	if err != nil {
		return "", err
	}

	type Arguments struct {
		ItemsList []int  `json:"items_list"`
		Outline   string `json:"outline"`
	}

	var args Arguments

	if len(resp.Choices) == 0 || len(resp.Choices[0].Message.ToolCalls) == 0 {
		return "", fmt.Errorf("LLM 未返回工具调用")
	}
	argsStr := resp.Choices[0].Message.ToolCalls[0].Function.Arguments

	if err := json.Unmarshal([]byte(argsStr), &args); err != nil {
		return "", err
	}

	picked := make([]domain.Item, 0, reLen)
	skipped := 0
	for _, idx := range args.ItemsList {
		if idx < 1 || idx > len(*items) {
			skipped++
			continue
		}
		picked = append(picked, (*items)[idx-1])
		if len(picked) == reLen {
			break
		}
	}
	if skipped > 0 {
		mylog.LoggerOf(ctx, mylog.CtxGLKey{}).Warn().
			Msgf("LLM 推荐 %d 个物品，其中 %d 个 LLM 返回了越界序号", len(args.ItemsList), skipped)
	}
	*items = picked
	return args.Outline, nil
}

func (w *LLMInvoking) UpdateWithNewInfo(ctx context.Context, cols string) (string, string, error) {
	userStr := fmt.Sprintf("发生改动的字段及改动后内容：%s", cols)
	messages := []pkg.ChatMessage{
		{Role: "system", Content: PromptUpdateWithNewInfo},
		{Role: "user", Content: userStr},
	}
	resp := &pkg.RespData{}
	err := w.cli.Send(ctx, messages, BuildToolUS(), BuildToolChoice(ToolUS), resp)
	if err != nil {
		return "", "", err
	}
	summary, interestTopics, err := extractSummaryAndTopics(resp)
	if err != nil {
		return "", "", err
	}

	return summary, interestTopics, nil
}

func (w *LLMInvoking) UpdateUserProfile(
	ctx context.Context, user *domain.User,
	new_ia *[]domain.Interaction, his_ia *[]domain.Interaction) (string, error) {
	buf_new_ia, err := json.MarshalIndent(*new_ia, "", "\t")
	if err != nil {
		return "", err
	}
	buf_his_ia, err := json.MarshalIndent(*his_ia, "", "\t")
	if err != nil {
		return "", err
	}

	messages := []pkg.ChatMessage{
		{Role: "system", Content: PromptUpdateUS},
		{Role: "user", Content: fmt.Sprintf("原用户画像：%s\n历史交互记录：%s\n当前交互记录：%s", user.Summary, string(buf_his_ia), string(buf_new_ia))},
	}

	resp := &pkg.RespData{}
	err = w.cli.Send(ctx, messages, BuildToolUS(), BuildToolChoice(ToolUS), resp)
	if err != nil {
		return "", err
	}
	summary, interestTopics, err := extractSummaryAndTopics(resp)
	if err != nil {
		return "", err
	}
	user.Summary = summary

	return interestTopics, nil
}

// extractSummary 从强制工具调用的响应中提取用户画像摘要。
// 摘要文本在 tool_calls[0].function.arguments 的 user_profile_summary 字段里，
// 而 Message.Content 为空（因为 tool_choice 强制调用函数）。
func extractSummaryAndTopics(resp *pkg.RespData) (string, string, error) {
	if len(resp.Choices) == 0 || len(resp.Choices[0].Message.ToolCalls) == 0 {
		return "", "", fmt.Errorf("LLM 未返回工具调用, resp:%v", resp)
	}
	var args struct {
		Summary        string `json:"summary"`
		InterestTopics string `json:"interest_topics"`
	}
	if err := json.Unmarshal([]byte(resp.Choices[0].Message.ToolCalls[0].Function.Arguments), &args); err != nil {
		return "", "", err
	}
	if strings.TrimSpace(args.Summary) == "" {
		return "", "", pkg.ErrLLMSummaryEmpty
	}
	if strings.TrimSpace(args.InterestTopics) == "" {
		return "", "", pkg.ErrLLMTopicsEmpty
	}
	return args.Summary, args.InterestTopics, nil
}
