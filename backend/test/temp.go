package main

import (
	"dcpro/llm"
	"fmt"
)

func main1() {
	fmt.Println(fmt.Sprintf(llm.PromptRecommendUser, 1, "aaa", "bbb"))
}
