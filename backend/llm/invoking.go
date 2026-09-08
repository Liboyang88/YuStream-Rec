package llm

import (
	"dcpro/domain"
	"dcpro/pkg"
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

var (
	ToolRecommend = "recommend"
	ToolUS        = "generate_user_profile_summary"
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

// recommendItemsList recommend 工具 items_list 属性的 schema
type recommendItemsList struct {
	Type        string         `json:"type"`
	Description string         `json:"description"`
	Items       recommendItems `json:"items"`
}

// recommendParams recommend 工具的 parameters schema
type recommendParams struct {
	Type       string                        `json:"type"`
	Required   []string                      `json:"required"`
	Properties map[string]recommendItemsList `json:"properties"`
}

// summaryField generate_user_profile_summary 工具属性的 schema
type summaryField struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

// summaryParams generate_user_profile_summary 工具的 parameters schema
type summaryParams struct {
	Type       string                 `json:"type"`
	Required   []string               `json:"required"`
	Properties map[string]summaryField `json:"properties"`
}

func BuildToolRecommend(totalLen int, reLen int) []pkg.Tool {
	return []pkg.Tool{
		{
			Type: "function",
			Function: pkg.ToolFunction{
				Name: ToolRecommend,
				Description: fmt.Sprintf("根据用户信息与物品列表中的物品描述，决定将哪些物品推荐给用户。注意：备选的物品列表长度为%d，最终决定推荐的物品列表长度为%d；且你只需返回一个JSON数组，只包含备选物品列表中的序号ID。", totalLen, reLen),
				Parameters: recommendParams{
					Type:     "object",
					Required: []string{"items_list"},
					Properties: map[string]recommendItemsList{
						"items_list": {
							Type:        "array",
							Description: fmt.Sprintf("最终决定推荐给用户的物品序号ID列表，长度为%d", reLen),
							Items: recommendItems{
								Type:        "integer",
								Description: "物品ID",
								Minimum:     1,
								Maximum:     totalLen,
							},
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
				Parameters: summaryParams{
					Type:     "object",
					Required: []string{"user_profile_summary"},
					Properties: map[string]summaryField{
						"user_profile_summary": {
							Type:        "string",
							Description: "由自然语言描述和概括的用户画像",
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

func (w *LLMInvoking) GenerateUserProfile(user *domain.User) error {
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
		{Role: "user", Content: fmt.Sprintf("以下为该用户信息，请进行描述和概括：%s", userStr)},
	}

	resp := &pkg.RespData{}
	err := w.cli.Send(fmt.Sprintf("%d", user.ID), messages, BuildToolUS(), BuildToolChoice(ToolUS), resp)
	log.Println("LLM resp:", resp)
	if err != nil {
		return err
	}
	summary, err := extractSummary(resp)
	if err != nil {
		return err
	}
	user.Summary = summary

	return nil
}

func (w *LLMInvoking) Recommend(userID uint, userProfile string, items *[]domain.Item, totalLen int, reLen int) error {
	var builder strings.Builder
	for ind, item := range *items {
		builder.WriteString(fmt.Sprintf("%d. %s\n", ind+1, item.Summary))
	}
	itemsSummary := builder.String()

	messages := []pkg.ChatMessage{
		{Role: "system", Content: fmt.Sprintf(PromptRecommend, reLen)},
		{Role: "user", Content: fmt.Sprintf(PromptRecommendUser, reLen, userProfile, itemsSummary)},
	}

	resp := &pkg.RespData{}
	err := w.cli.Send(fmt.Sprintf("%d", userID), messages, BuildToolRecommend(totalLen, reLen), BuildToolChoice(ToolRecommend), resp)
	if err != nil {
		return err
	}

	type Arguments struct {
		ItemsList []int `json:"items_list"`
	}

	var args Arguments

	if len(resp.Choices) == 0 || len(resp.Choices[0].Message.ToolCalls) == 0 {
		return fmt.Errorf("LLM 未返回工具调用")
	}
	argsStr := resp.Choices[0].Message.ToolCalls[0].Function.Arguments

	if err := json.Unmarshal([]byte(argsStr), &args); err != nil {
		return err
	}

	itemsRes := &[]domain.Item{}
	for _, itemID := range args.ItemsList {
		if itemID < 1 || itemID > len(*items) {
			return fmt.Errorf("推荐序号越界: %d (有效范围 1-%d)", itemID, len(*items))
		}
		*itemsRes = append(*itemsRes, (*items)[itemID-1])
	}
	*items = *itemsRes // 写回调用方；之前 items = itemsRes 只改了形参指针

	return nil
}

func (w *LLMInvoking) UpdateUserProfile(user *domain.User, new_ia *[]domain.Interaction, his_ia *[]domain.Interaction) error {
	buf_new_ia, err := json.MarshalIndent(*new_ia, "", "\t")
	if err != nil {
		return err
	}
	buf_his_ia, err := json.MarshalIndent(*his_ia, "", "\t")
	if err != nil {
		return err
	}

	messages := []pkg.ChatMessage{
		{Role: "system", Content: PromptUpdateUS},
		{Role: "user", Content: fmt.Sprintf("原用户画像：%s\n历史交互记录：%s\n当前交互记录：%s", user.Summary, string(buf_his_ia), string(buf_new_ia))},
	}

	resp := &pkg.RespData{}
	err = w.cli.Send(fmt.Sprintf("%d", user.ID), messages, BuildToolUS(), BuildToolChoice(ToolUS), resp)
	if err != nil {
		return err
	}
	summary, err := extractSummary(resp)
	if err != nil {
		return err
	}
	user.Summary = summary

	return nil
}

// extractSummary 从强制工具调用的响应中提取用户画像摘要。
// 摘要文本在 tool_calls[0].function.arguments 的 user_profile_summary 字段里，
// 而 Message.Content 为空（因为 tool_choice 强制调用函数）。
func extractSummary(resp *pkg.RespData) (string, error) {
	if len(resp.Choices) == 0 || len(resp.Choices[0].Message.ToolCalls) == 0 {
		return "", fmt.Errorf("LLM 未返回工具调用, resp:%v", resp)
	}
	var args struct {
		UserProfileSummary string `json:"user_profile_summary"`
	}
	if err := json.Unmarshal([]byte(resp.Choices[0].Message.ToolCalls[0].Function.Arguments), &args); err != nil {
		return "", err
	}
	return args.UserProfileSummary, nil
}
