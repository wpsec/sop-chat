package chatflow

import (
	"fmt"
	"strings"
	"time"

	"sop-chat/internal/config"
	"sop-chat/internal/workflow"
)

type PreparedRequest struct {
	Message   string
	Variables map[string]interface{}
	Plan      *workflow.Plan
}

func Prepare(globalCfg *config.Config, message string, concise bool, timeZone, language string, ctx config.ProductContext) (*PreparedRequest, error) {
	trimmed := strings.TrimSpace(message)
	plan, planErr := maybeBuildPlan(globalCfg, trimmed, ctx)
	enrichedMessage := trimmed
	if plan != nil {
		if promptBlock := workflow.BuildPromptBlock(plan); promptBlock != "" {
			enrichedMessage = strings.TrimSpace(trimmed + "\n\n" + promptBlock)
		}
	}

	return &PreparedRequest{
		Message:   config.ApplyReplyStyleInstruction(enrichedMessage, concise, ctx.Product),
		Variables: BuildVariables(timeZone, language, ctx),
		Plan:      plan,
	}, planErr
}

func BuildVariables(timeZone, language string, ctx config.ProductContext) map[string]interface{} {
	variables := map[string]interface{}{
		"timeStamp": fmt.Sprintf("%d", time.Now().Unix()),
		"timeZone":  timeZone,
		"language":  language,
	}

	if config.IsSlsProduct(ctx.Product) {
		variables["skill"] = "sop"
		if ctx.Project != "" {
			variables["project"] = ctx.Project
		}
		return variables
	}

	if ctx.Workspace != "" {
		variables["workspace"] = ctx.Workspace
	}
	if ctx.Region != "" {
		variables["region"] = ctx.Region
	}
	now := time.Now()
	variables["fromTime"] = now.Add(-15 * time.Minute).Unix()
	variables["toTime"] = now.Unix()
	return variables
}

func maybeBuildPlan(globalCfg *config.Config, message string, ctx config.ProductContext) (*workflow.Plan, error) {
	if globalCfg == nil || message == "" || !config.IsSlsProduct(ctx.Product) {
		return nil, nil
	}

	root := globalCfg.GetSOPWorkflowRoot()
	if root == "" {
		return nil, nil
	}

	return workflow.BuildPlan(root, message)
}
