package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

var (
	repositoryCache sync.Map

	backtickRegex   = regexp.MustCompile("`([^`]+)`")
	moduleRefRegex  = regexp.MustCompile(`\.\./([^/\s]+)/`)
	spaceRegex      = regexp.MustCompile(`\s+`)
	tokenSplitRegex = regexp.MustCompile(`[<>{}\[\]()"“”'‘’,:;!?/\\|=+*&^%$#@~，。；：！？、（）《》\n\r\t]+`)
)

var stopKeywords = map[string]struct{}{
	"": {}, "用户": {}, "问题": {}, "分析": {}, "说明": {}, "当前": {}, "模块": {}, "最近": {}, "先": {}, "再": {},
	"读取": {}, "输出": {}, "结果": {}, "使用": {}, "需要": {}, "默认": {}, "日志": {}, "告警": {}, "正式报告": {},
	"原因": {}, "时间": {}, "范围": {}, "当前模块": {}, "继续": {}, "处理": {}, "进入": {}, "继续分析": {},
	"mapped_action": {}, "condition": {}, "action": {}, "important_notes": {}, "user_says": {},
}

var moduleSourceBoosts = map[string][]string{
	"k8s":                {"k8s-event"},
	"k8s_runtime":        {"k8s-event"},
	"k8s_event":          {"k8s-event"},
	"backend":            {"application"},
	"backend_app":        {"application"},
	"proxy":              {"accesslog", "proxy-runtime"},
	"gateway":            {"accesslog", "proxy-runtime"},
	"postgresql":         {"database-replication", "database-long-transaction"},
	"postgresql_runtime": {"database-replication", "database-long-transaction"},
}

type Plan struct {
	Root                 string             `json:"root"`
	Summary              string             `json:"summary"`
	Workflow             *WorkflowPlan      `json:"workflow,omitempty"`
	WorkflowSteps        []WorkflowStepPlan `json:"workflowSteps,omitempty"`
	EntryModule          *ModulePlan        `json:"entryModule,omitempty"`
	HandoffModules       []ModulePlan       `json:"handoffModules,omitempty"`
	CandidateDataSources []DataSourcePlan   `json:"candidateDataSources,omitempty"`
	CorrelationKeys      []CorrelationPlan  `json:"correlationKeys,omitempty"`
	TimeWindow           string             `json:"timeWindow,omitempty"`
	CommonSteps          []string           `json:"commonSteps,omitempty"`
	EvidenceChecklist    []string           `json:"evidenceChecklist,omitempty"`
}

type WorkflowPlan struct {
	Name         string   `json:"name"`
	ID           string   `json:"id,omitempty"`
	File         string   `json:"file,omitempty"`
	Reason       string   `json:"reason,omitempty"`
	TimeWindow   string   `json:"timeWindow,omitempty"`
	EntryModules []string `json:"entryModules,omitempty"`
}

type WorkflowStepPlan struct {
	ID          string   `json:"id,omitempty"`
	Kind        string   `json:"kind,omitempty"`
	Module      string   `json:"module,omitempty"`
	Description string   `json:"description,omitempty"`
	DependsOn   []string `json:"dependsOn,omitempty"`
	RunIfAny    []string `json:"runIfAny,omitempty"`
	Produces    []string `json:"produces,omitempty"`
}

type ModulePlan struct {
	Name        string   `json:"name"`
	ID          string   `json:"id,omitempty"`
	Path        string   `json:"path,omitempty"`
	Description string   `json:"description,omitempty"`
	Reason      string   `json:"reason,omitempty"`
	Keywords    []string `json:"keywords,omitempty"`
}

type DataSourcePlan struct {
	Name            string `json:"name"`
	Project         string `json:"project,omitempty"`
	Logstore        string `json:"logstore,omitempty"`
	LogstorePattern string `json:"logstorePattern,omitempty"`
	Reason          string `json:"reason,omitempty"`
}

type CorrelationPlan struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Scopes      []string `json:"scopes,omitempty"`
	Reason      string   `json:"reason,omitempty"`
}

type repository struct {
	root             string
	workflowOverview workflowOverviewDoc
	workflows        []workflowDoc
	modules          map[string]moduleDoc
	moduleDirs       map[string]string
	moduleOrder      []string
	logSources       []logSourceDoc
	correlationKeys  []correlationKeyDoc
}

type workflowOverviewDoc struct {
	WorkflowFiles      []workflowFileRef            `yaml:"workflow_files"`
	Files              []workflowFileRef            `yaml:"files"`
	CommonExecution    commonExecutionDoc           `yaml:"common_execution"`
	CommonWorkflow     commonWorkflowDoc            `yaml:"common_workflow"`
	TimeWindowPolicies map[string]timeRangeStrategy `yaml:"time_window_policies"`
	TimeRangeStrategy  map[string]timeRangeStrategy `yaml:"time_range_strategy"`
}

type workflowFileRef struct {
	WorkflowID   string   `yaml:"workflow_id"`
	File         string   `yaml:"file"`
	Role         string   `yaml:"role"`
	IntentTypes  []string `yaml:"intent_types"`
	EntryModules []string `yaml:"entry_modules"`
	Alerts       []string `yaml:"alerts"`
	Note         string   `yaml:"note"`
}

type commonExecutionDoc struct {
	Description string                `yaml:"description"`
	Steps       []commonExecutionStep `yaml:"steps"`
}

type commonExecutionStep struct {
	ID     string `yaml:"id"`
	Kind   string `yaml:"kind"`
	Title  string `yaml:"title"`
	Action string `yaml:"action"`
}

type commonWorkflowDoc struct {
	Description string            `yaml:"description"`
	Steps       []commonStepEntry `yaml:"steps"`
}

type commonStepEntry struct {
	Title  string `yaml:"title"`
	Action string `yaml:"action"`
}

type timeRangeStrategy struct {
	Range  string `yaml:"range"`
	Reason string `yaml:"reason"`
}

type workflowDoc struct {
	File              string
	Role              string
	Alerts            []string
	RefWorkflowID     string
	RefIntentTypes    []string
	RefEntryModules   []string
	Schema            string            `yaml:"schema"`
	WorkflowID        string            `yaml:"workflow_id"`
	Title             string            `yaml:"title"`
	IntentTypes       []string          `yaml:"intent_types"`
	EntryModules      []string          `yaml:"entry_modules"`
	RequiredContext   []string          `yaml:"required_context"`
	OptionalContext   []string          `yaml:"optional_context"`
	ProducesFacts     []string          `yaml:"produces_facts"`
	Steps             []workflowStepDoc `yaml:"steps"`
	Name              string            `yaml:"name"`
	Description       string            `yaml:"description"`
	TriggerConditions []string          `yaml:"trigger_conditions"`
	TimeRange         string            `yaml:"time_range"`
}

type workflowStepDoc struct {
	ID          string   `yaml:"id"`
	StepID      string   `yaml:"step_id"`
	Kind        string   `yaml:"kind"`
	Module      string   `yaml:"module"`
	Description string   `yaml:"description"`
	DependsOn   []string `yaml:"depends_on"`
	RunIfAny    []string `yaml:"run_if_any"`
	Produces    []string `yaml:"produces"`
}

type moduleDoc struct {
	Name               string             `yaml:"module"`
	ModuleID           string             `yaml:"module_id"`
	Description        string             `yaml:"description"`
	EntryHints         entryHintsDoc      `yaml:"entry_hints"`
	Execution          moduleExecutionDoc `yaml:"execution"`
	TaskRoutingRules   []routingRuleDoc   `yaml:"task_routing_rules"`
	SupportedIntents   []intentRuleDoc    `yaml:"supported_intents"`
	ImportantNotes     []string           `yaml:"important_notes"`
	AnalysisDimensions []string           `yaml:"analysis_dimensions"`
	CoreFields         []string           `yaml:"core_fields_reference"`
	QuickLinks         map[string]string  `yaml:"quick_links"`
	Path               string
	Keywords           []string
}

type entryHintsDoc struct {
	Keywords     []string `yaml:"keywords"`
	ObjectInputs []string `yaml:"object_inputs"`
	Priority     string   `yaml:"priority"`
}

type moduleExecutionDoc struct {
	RequiredInputs       inputSpecDoc       `yaml:"required_inputs"`
	OptionalInputs       []string           `yaml:"optional_inputs"`
	PrimarySource        moduleSourceDoc    `yaml:"primary_source"`
	AlternateSources     []moduleSourceDoc  `yaml:"alternate_sources"`
	Produces             []string           `yaml:"produces"`
	EvidenceRequirements []string           `yaml:"evidence_requirements"`
	Handoff              []moduleHandoffDoc `yaml:"handoff"`
}

type inputSpecDoc struct {
	AnyOf []string `yaml:"any_of"`
	AllOf []string `yaml:"all_of"`
}

type moduleSourceDoc struct {
	SourceAlias     string `yaml:"source_alias"`
	Project         string `yaml:"project"`
	Logstore        string `yaml:"logstore"`
	LogstorePattern string `yaml:"logstore_pattern"`
	SourceRole      string `yaml:"source_role"`
}

type moduleHandoffDoc struct {
	TargetModule string          `yaml:"target_module"`
	TriggerFacts triggerFactsDoc `yaml:"trigger_facts"`
	Purpose      string          `yaml:"purpose"`
}

type triggerFactsDoc struct {
	AnyOf []string `yaml:"any_of"`
	AllOf []string `yaml:"all_of"`
}

type routingRuleDoc struct {
	Condition string   `yaml:"condition"`
	Action    []string `yaml:"action"`
}

type intentRuleDoc struct {
	UserSays     string   `yaml:"user_says"`
	MappedAction []string `yaml:"mapped_action"`
}

type logSourceDoc struct {
	Name            string              `yaml:"name"`
	Description     string              `yaml:"description"`
	Project         string              `yaml:"project"`
	Logstore        string              `yaml:"logstore"`
	MetricStore     string              `yaml:"metric_store"`
	StoreType       string              `yaml:"store_type"`
	DeploymentTypes []deploymentTypeDoc `yaml:"deployment_types"`
	Fields          []fieldDoc          `yaml:"fields"`
}

type deploymentTypeDoc struct {
	Type            string `yaml:"type"`
	LogstorePattern string `yaml:"logstore_pattern"`
	Example         string `yaml:"example"`
}

type fieldDoc struct {
	Name string `yaml:"name"`
	Type string `yaml:"type"`
}

type correlationKeyDoc struct {
	Name            string              `yaml:"name"`
	Description     string              `yaml:"desc"`
	Scope           []string            `yaml:"scope"`
	ExtractionRules []extractionRuleDoc `yaml:"extraction_rules"`
}

type extractionRuleDoc struct {
	Source  string `yaml:"source"`
	Method  string `yaml:"method"`
	Example string `yaml:"example"`
}

type scoredWorkflow struct {
	doc          workflowDoc
	score        int
	matchedWords []string
}

type scoredModule struct {
	doc          moduleDoc
	score        int
	matchedWords []string
}

type scoredLogSource struct {
	doc          logSourceDoc
	score        int
	matchedWords []string
}

type scoredCorrelationKey struct {
	doc          correlationKeyDoc
	score        int
	matchedWords []string
}

func BuildPlan(root, message string) (*Plan, error) {
	root = strings.TrimSpace(root)
	message = strings.TrimSpace(message)
	if root == "" || message == "" {
		return nil, nil
	}

	repo, err := loadRepository(root)
	if err != nil {
		return nil, err
	}

	return repo.buildPlan(message), nil
}

func BuildPromptBlock(plan *Plan) string {
	if plan == nil {
		return ""
	}

	lines := []string{
		"【sop-chat 联动分析上下文】",
	}

	if plan.Workflow != nil && plan.Workflow.Name != "" {
		line := "主工作流：" + plan.Workflow.Name
		if plan.Workflow.TimeWindow != "" {
			line += "；建议时间窗口：" + plan.Workflow.TimeWindow
		}
		lines = append(lines, line)
	}

	if plan.EntryModule != nil && plan.EntryModule.Name != "" {
		line := "建议入口模块：" + plan.EntryModule.Name
		if plan.EntryModule.Reason != "" {
			line += "；触发依据：" + plan.EntryModule.Reason
		}
		lines = append(lines, line)
	}

	if len(plan.HandoffModules) > 0 {
		parts := make([]string, 0, len(plan.HandoffModules))
		for _, item := range plan.HandoffModules {
			if item.Name == "" {
				continue
			}
			if item.Reason != "" {
				parts = append(parts, fmt.Sprintf("%s（%s）", item.Name, item.Reason))
				continue
			}
			parts = append(parts, item.Name)
		}
		if len(parts) > 0 {
			lines = append(lines, "候选联动模块："+strings.Join(parts, " -> "))
		}
	}

	if len(plan.WorkflowSteps) > 0 {
		parts := make([]string, 0, len(plan.WorkflowSteps))
		for _, item := range plan.WorkflowSteps {
			label := firstNonEmpty(item.ID, item.Kind, item.Module)
			if item.Module != "" {
				label += "@" + item.Module
			}
			if item.Description != "" {
				label += "：" + item.Description
			}
			if len(item.RunIfAny) > 0 {
				label += "；条件：" + strings.Join(item.RunIfAny, " / ")
			}
			if len(item.Produces) > 0 {
				label += "；产出：" + strings.Join(item.Produces, "、")
			}
			parts = append(parts, label)
		}
		lines = append(lines, "必须执行/判定步骤："+strings.Join(parts, "；"))
	}

	if len(plan.CorrelationKeys) > 0 {
		parts := make([]string, 0, len(plan.CorrelationKeys))
		for _, item := range plan.CorrelationKeys {
			parts = append(parts, item.Name)
		}
		lines = append(lines, "优先提取关联键："+strings.Join(parts, "、"))
	}

	if len(plan.CandidateDataSources) > 0 {
		parts := make([]string, 0, len(plan.CandidateDataSources))
		for _, item := range plan.CandidateDataSources {
			label := item.Name
			if item.Project != "" || item.Logstore != "" || item.LogstorePattern != "" {
				target := strings.TrimSpace(item.Project + "/" + firstNonEmpty(item.Logstore, item.LogstorePattern))
				if target != "/" && target != "" {
					label = fmt.Sprintf("%s（%s）", item.Name, target)
				}
			}
			parts = append(parts, label)
		}
		lines = append(lines, "优先数据源："+strings.Join(parts, "；"))
	}

	if len(plan.EvidenceChecklist) > 0 {
		lines = append(lines, "证据检查："+strings.Join(plan.EvidenceChecklist, "；"))
	}

	lines = append(lines,
		"执行约束：按工作流步骤逐项标注已完成、跳过或证据不足；告警名称、阈值命中、状态码、事件 reason、错误数量、延迟指标等只能作为 direct_trigger 或 symptom_evidence，必须按 handoff 条件补 root_cause_evidence；证据不足时明确写待确认项，不要把候选方向当根因；输出时请显式区分直接触发器、根因证据状态、应用侧原因、依赖侧原因和影响范围。",
	)

	return strings.Join(lines, "\n")
}

func (r *repository) buildPlan(message string) *Plan {
	workflowMatches := r.matchWorkflows(message)
	moduleMatches := r.matchModules(message)

	plan := &Plan{
		Root:        r.root,
		CommonSteps: r.commonSteps(),
		TimeWindow:  r.defaultTimeWindow(workflowMatches),
	}

	if len(workflowMatches) > 0 {
		top := workflowMatches[0]
		plan.Workflow = &WorkflowPlan{
			Name:         firstNonEmpty(top.doc.Title, top.doc.Name, top.doc.Role, top.doc.WorkflowID, top.doc.RefWorkflowID),
			ID:           firstNonEmpty(top.doc.WorkflowID, top.doc.RefWorkflowID),
			File:         top.doc.File,
			Reason:       buildReason(top.matchedWords),
			TimeWindow:   firstNonEmpty(top.doc.TimeRange, plan.TimeWindow),
			EntryModules: firstNonEmptySlice(top.doc.EntryModules, top.doc.RefEntryModules),
		}
		plan.WorkflowSteps = workflowStepPlans(top.doc.Steps)
		if plan.Workflow.TimeWindow != "" {
			plan.TimeWindow = plan.Workflow.TimeWindow
		}
	}

	if entry, ok := r.selectEntryModuleMatch(plan.Workflow, moduleMatches); ok {
		plan.EntryModule = &ModulePlan{
			Name:        entry.doc.Name,
			ID:          entry.doc.ModuleID,
			Path:        entry.doc.Path,
			Description: entry.doc.Description,
			Reason:      buildReason(entry.matchedWords),
			Keywords:    capStrings(entry.doc.Keywords, 6),
		}
	}

	plan.HandoffModules = r.buildHandoffs(message, plan.EntryModule, moduleMatches)
	plan.CandidateDataSources = r.buildDataSources(message, plan.EntryModule, plan.HandoffModules)
	plan.CorrelationKeys = r.buildCorrelationKeys(message, plan.EntryModule, plan.HandoffModules, plan.CandidateDataSources)
	plan.EvidenceChecklist = r.buildEvidenceChecklist(plan.EntryModule, plan.HandoffModules, plan.CorrelationKeys)
	plan.Summary = r.buildSummary(plan)

	if plan.Summary == "" && plan.EntryModule == nil && plan.Workflow == nil {
		return nil
	}
	return plan
}

func (r *repository) commonSteps() []string {
	if len(r.workflowOverview.CommonExecution.Steps) > 0 {
		steps := make([]string, 0, len(r.workflowOverview.CommonExecution.Steps))
		for _, item := range r.workflowOverview.CommonExecution.Steps {
			title := strings.TrimSpace(item.Title)
			action := strings.TrimSpace(item.Action)
			kind := strings.TrimSpace(item.Kind)
			switch {
			case title != "" && action != "" && kind != "":
				steps = append(steps, title+"("+kind+")："+action)
			case title != "" && action != "":
				steps = append(steps, title+"："+action)
			case action != "":
				steps = append(steps, action)
			}
		}
		return capStrings(dedupeStrings(steps), 6)
	}

	steps := make([]string, 0, len(r.workflowOverview.CommonWorkflow.Steps))
	for _, item := range r.workflowOverview.CommonWorkflow.Steps {
		title := strings.TrimSpace(item.Title)
		action := strings.TrimSpace(item.Action)
		switch {
		case title != "" && action != "":
			steps = append(steps, title+"："+action)
		case title != "":
			steps = append(steps, title)
		case action != "":
			steps = append(steps, action)
		}
	}
	return capStrings(dedupeStrings(steps), 6)
}

func (r *repository) defaultTimeWindow(matches []scoredWorkflow) string {
	if len(matches) > 0 && strings.TrimSpace(matches[0].doc.TimeRange) != "" {
		return strings.TrimSpace(matches[0].doc.TimeRange)
	}
	for _, key := range []string{"availability", "error", "performance", "database", "prometheus"} {
		if item, ok := r.workflowOverview.TimeWindowPolicies[key]; ok && strings.TrimSpace(item.Range) != "" {
			return strings.TrimSpace(item.Range)
		}
	}
	for _, key := range []string{"availability_alerts", "error_alerts", "performance_alerts", "database_alerts", "prometheus_alerts"} {
		if item, ok := r.workflowOverview.TimeRangeStrategy[key]; ok && strings.TrimSpace(item.Range) != "" {
			return strings.TrimSpace(item.Range)
		}
	}
	return ""
}

func (r *repository) buildSummary(plan *Plan) string {
	parts := make([]string, 0, 4)
	if plan.EntryModule != nil && plan.EntryModule.Name != "" {
		parts = append(parts, "入口模块为 "+plan.EntryModule.Name)
	}
	if len(plan.HandoffModules) > 0 {
		names := make([]string, 0, len(plan.HandoffModules))
		for _, item := range plan.HandoffModules {
			if item.Name != "" {
				names = append(names, item.Name)
			}
		}
		if len(names) > 0 {
			parts = append(parts, "后续视证据联动 "+strings.Join(names, "、"))
		}
	}
	if len(plan.CorrelationKeys) > 0 {
		names := make([]string, 0, len(plan.CorrelationKeys))
		for _, item := range plan.CorrelationKeys {
			if item.Name != "" {
				names = append(names, item.Name)
			}
		}
		if len(names) > 0 {
			parts = append(parts, "先提取 "+strings.Join(names, "、"))
		}
	}
	if plan.TimeWindow != "" {
		parts = append(parts, "建议时间窗口 "+plan.TimeWindow)
	}
	return strings.Join(parts, "；")
}

func (r *repository) selectEntryModuleMatch(workflow *WorkflowPlan, ranked []scoredModule) (scoredModule, bool) {
	if len(ranked) == 0 {
		return scoredModule{}, false
	}
	if workflow == nil || len(workflow.EntryModules) == 0 {
		return ranked[0], true
	}
	for _, ref := range workflow.EntryModules {
		doc, exists := r.lookupModule(ref)
		if !exists {
			continue
		}
		for _, item := range ranked {
			if item.doc.Name == doc.Name {
				return item, true
			}
		}
	}
	return ranked[0], true
}

func (r *repository) buildHandoffs(message string, entry *ModulePlan, ranked []scoredModule) []ModulePlan {
	if entry == nil {
		return nil
	}

	entryDoc, ok := r.modules[entry.Name]
	if !ok {
		return nil
	}

	candidates := make([]ModulePlan, 0)
	seen := map[string]struct{}{}

	for _, handoff := range entryDoc.Execution.Handoff {
		refDoc, exists := r.lookupModule(handoff.TargetModule)
		if !exists {
			continue
		}
		if _, duplicated := seen[refDoc.Name]; duplicated {
			continue
		}
		ruleKeywords := extractKeywords(handoff.TargetModule, handoff.Purpose, strings.Join(handoff.TriggerFacts.AnyOf, " "), strings.Join(handoff.TriggerFacts.AllOf, " "))
		ruleScore, matched := scoreKeywords(message, ruleKeywords)
		if ruleScore == 0 {
			for _, item := range ranked {
				if item.doc.Name == refDoc.Name && item.score > 0 {
					ruleScore = item.score
					matched = item.matchedWords
					break
				}
			}
		}
		if ruleScore == 0 {
			continue
		}
		seen[refDoc.Name] = struct{}{}
		candidates = append(candidates, ModulePlan{
			Name:        refDoc.Name,
			ID:          refDoc.ModuleID,
			Path:        refDoc.Path,
			Description: refDoc.Description,
			Reason:      firstNonEmpty(buildReason(capStrings(matched, 4)), handoff.Purpose),
			Keywords:    capStrings(refDoc.Keywords, 6),
		})
	}

	for _, rule := range entryDoc.TaskRoutingRules {
		joinedActions := strings.Join(rule.Action, " ")
		for _, ref := range extractModuleRefs(rule.Condition, joinedActions) {
			refDoc, exists := r.lookupModule(ref)
			if !exists {
				continue
			}
			if _, duplicated := seen[refDoc.Name]; duplicated {
				continue
			}
			ruleKeywords := extractKeywords(rule.Condition, joinedActions)
			ruleScore, matched := scoreKeywords(message, ruleKeywords)
			if ruleScore == 0 {
				for _, item := range ranked {
					if item.doc.Name == refDoc.Name && item.score > 0 {
						ruleScore = item.score
						matched = item.matchedWords
						break
					}
				}
			}
			if ruleScore == 0 {
				continue
			}
			seen[refDoc.Name] = struct{}{}
			candidates = append(candidates, ModulePlan{
				Name:        refDoc.Name,
				ID:          refDoc.ModuleID,
				Path:        refDoc.Path,
				Description: refDoc.Description,
				Reason:      buildReason(capStrings(matched, 4)),
				Keywords:    capStrings(refDoc.Keywords, 6),
			})
		}
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].Name < candidates[j].Name
	})
	return capModulePlans(candidates, 3)
}

func (r *repository) buildDataSources(message string, entry *ModulePlan, handoffs []ModulePlan) []DataSourcePlan {
	result := make([]DataSourcePlan, 0, 6)
	addSource := func(source moduleSourceDoc, reason string) {
		name := strings.TrimSpace(firstNonEmpty(source.SourceAlias, source.Logstore, source.LogstorePattern))
		if name == "" {
			return
		}
		result = append(result, DataSourcePlan{
			Name:            name,
			Project:         strings.TrimSpace(source.Project),
			Logstore:        strings.TrimSpace(source.Logstore),
			LogstorePattern: strings.TrimSpace(source.LogstorePattern),
			Reason:          reason,
		})
	}
	addModuleSources := func(moduleName, reason string) {
		if moduleName == "" {
			return
		}
		doc, ok := r.modules[moduleName]
		if !ok {
			return
		}
		addSource(doc.Execution.PrimarySource, reason)
		for _, source := range doc.Execution.AlternateSources {
			addSource(source, reason)
		}
	}

	if entry != nil {
		addModuleSources(entry.Name, "来自入口模块 primary_source")
	}
	for _, handoff := range handoffs {
		addModuleSources(handoff.Name, "来自联动模块 primary_source")
	}

	matches := make([]scoredLogSource, 0, len(r.logSources))
	for _, item := range r.logSources {
		keywords := extractKeywords(item.Name, item.Description, strings.Join(fieldNames(item.Fields), " "))
		score, matched := scoreKeywords(message, keywords)
		if entry != nil {
			score += moduleSourceScore(entry.Name, item.Name)
		}
		for _, handoff := range handoffs {
			score += moduleSourceScore(handoff.Name, item.Name) / 2
		}
		if score == 0 {
			continue
		}
		matches = append(matches, scoredLogSource{doc: item, score: score, matchedWords: matched})
	}

	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score == matches[j].score {
			return matches[i].doc.Name < matches[j].doc.Name
		}
		return matches[i].score > matches[j].score
	})

	for _, item := range matches[:min(3, len(matches))] {
		result = append(result, DataSourcePlan{
			Name:            item.doc.Name,
			Project:         strings.TrimSpace(item.doc.Project),
			Logstore:        strings.TrimSpace(item.doc.Logstore),
			LogstorePattern: firstLogstorePattern(item.doc.DeploymentTypes),
			Reason:          buildReason(capStrings(item.matchedWords, 4)),
		})
	}
	return capDataSourcePlans(dedupeDataSources(result), 5)
}

func (r *repository) buildCorrelationKeys(message string, entry *ModulePlan, handoffs []ModulePlan, sources []DataSourcePlan) []CorrelationPlan {
	moduleHints := make([]string, 0, 1+len(handoffs))
	if entry != nil {
		moduleHints = append(moduleHints, entry.Name)
	}
	for _, item := range handoffs {
		moduleHints = append(moduleHints, item.Name)
	}

	sourceHints := make([]string, 0, len(sources))
	for _, item := range sources {
		sourceHints = append(sourceHints, item.Name)
	}

	matches := make([]scoredCorrelationKey, 0, len(r.correlationKeys))
	for _, item := range r.correlationKeys {
		keywords := extractKeywords(item.Name, item.Description, strings.Join(item.Scope, " "), extractMethods(item.ExtractionRules))
		score, matched := scoreKeywords(message, keywords)
		score += correlationHintScore(item.Name, moduleHints, sourceHints, message)
		if score == 0 {
			continue
		}
		matches = append(matches, scoredCorrelationKey{doc: item, score: score, matchedWords: matched})
	}

	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score == matches[j].score {
			return matches[i].doc.Name < matches[j].doc.Name
		}
		return matches[i].score > matches[j].score
	})

	result := make([]CorrelationPlan, 0, min(5, len(matches)))
	for _, item := range matches[:min(5, len(matches))] {
		result = append(result, CorrelationPlan{
			Name:        item.doc.Name,
			Description: item.doc.Description,
			Scopes:      item.doc.Scope,
			Reason:      buildReason(capStrings(item.matchedWords, 4)),
		})
	}
	return result
}

func (r *repository) buildEvidenceChecklist(entry *ModulePlan, handoffs []ModulePlan, keys []CorrelationPlan) []string {
	items := make([]string, 0, 10)
	items = append(items,
		"先确认主症状对应的直接证据，再联动下游模块",
		"时间线统一使用用户问题中的主窗口，补充窗口单独标注",
	)

	if len(keys) > 0 {
		names := make([]string, 0, len(keys))
		for _, item := range keys {
			names = append(names, item.Name)
		}
		items = append(items, "关联键至少覆盖："+strings.Join(names, "、"))
	}

	if entry != nil {
		if doc, ok := r.modules[entry.Name]; ok {
			items = append(items, capStrings(doc.Execution.EvidenceRequirements, 4)...)
			items = append(items, capStrings(doc.CoreFields, 3)...)
		}
	}
	for _, handoff := range handoffs {
		if doc, ok := r.modules[handoff.Name]; ok {
			items = append(items, capStrings(doc.Execution.EvidenceRequirements, 2)...)
			items = append(items, capStrings(doc.CoreFields, 1)...)
		}
	}

	return capStrings(dedupeStrings(items), 8)
}

func (r *repository) matchWorkflows(message string) []scoredWorkflow {
	matches := make([]scoredWorkflow, 0, len(r.workflows))
	for _, item := range r.workflows {
		stepTexts := make([]string, 0, len(item.Steps))
		for _, step := range item.Steps {
			stepTexts = append(stepTexts, step.ID, step.StepID, step.Kind, step.Module, step.Description, strings.Join(step.RunIfAny, " "), strings.Join(step.Produces, " "))
		}
		keywords := extractKeywords(
			item.WorkflowID,
			item.RefWorkflowID,
			item.Title,
			item.Name,
			item.Role,
			item.Description,
			strings.Join(item.IntentTypes, " "),
			strings.Join(item.RefIntentTypes, " "),
			strings.Join(item.EntryModules, " "),
			strings.Join(item.RefEntryModules, " "),
			strings.Join(item.RequiredContext, " "),
			strings.Join(item.OptionalContext, " "),
			strings.Join(item.ProducesFacts, " "),
			strings.Join(item.TriggerConditions, " "),
			strings.Join(item.Alerts, " "),
			strings.Join(stepTexts, " "),
		)
		score, matched := scoreKeywords(message, keywords)
		if score == 0 {
			continue
		}
		matches = append(matches, scoredWorkflow{doc: item, score: score, matchedWords: matched})
	}

	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score == matches[j].score {
			return matches[i].doc.File < matches[j].doc.File
		}
		return matches[i].score > matches[j].score
	})
	return matches
}

func (r *repository) matchModules(message string) []scoredModule {
	matches := make([]scoredModule, 0, len(r.modules))
	for _, name := range r.moduleOrder {
		item := r.modules[name]
		score, matched := scoreKeywords(message, item.Keywords)
		score += moduleIntentBoost(message, name)
		if score == 0 {
			continue
		}
		matches = append(matches, scoredModule{doc: item, score: score, matchedWords: matched})
	}

	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score == matches[j].score {
			return matches[i].doc.Name < matches[j].doc.Name
		}
		return matches[i].score > matches[j].score
	})
	return matches
}

func loadRepository(root string) (*repository, error) {
	cleanRoot := filepath.Clean(root)
	if cached, ok := repositoryCache.Load(cleanRoot); ok {
		return cached.(*repository), nil
	}

	loaded, err := loadRepositoryFromDisk(cleanRoot)
	if err != nil {
		return nil, err
	}
	repositoryCache.Store(cleanRoot, loaded)
	return loaded, nil
}

func loadRepositoryFromDisk(root string) (*repository, error) {
	if root == "" {
		return nil, fmt.Errorf("workflow root is empty")
	}

	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("读取 workflow 目录失败: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("workflow root 不是目录: %s", root)
	}

	repo := &repository{
		root:        root,
		modules:     make(map[string]moduleDoc),
		moduleDirs:  make(map[string]string),
		moduleOrder: make([]string, 0),
	}

	if err := readYAML(filepath.Join(root, "workflows", "overview.yaml"), &repo.workflowOverview); err != nil {
		return nil, err
	}

	workflowRefs := repo.workflowOverview.WorkflowFiles
	if len(workflowRefs) == 0 {
		workflowRefs = repo.workflowOverview.Files
	}

	repo.workflows = make([]workflowDoc, 0, len(workflowRefs))
	for _, item := range workflowRefs {
		doc := workflowDoc{
			File:            item.File,
			Role:            item.Role,
			Alerts:          item.Alerts,
			RefWorkflowID:   item.WorkflowID,
			RefIntentTypes:  item.IntentTypes,
			RefEntryModules: item.EntryModules,
		}
		if err := readYAML(filepath.Join(root, "workflows", item.File), &doc); err != nil {
			return nil, err
		}
		repo.workflows = append(repo.workflows, doc)
	}

	var correlationOverview struct {
		CorrelationKeys []correlationKeyDoc `yaml:"correlation_keys"`
	}
	if err := readYAML(filepath.Join(root, "correlation-keys", "overview.yaml"), &correlationOverview); err != nil {
		return nil, err
	}
	repo.correlationKeys = correlationOverview.CorrelationKeys

	var logSourceOverview struct {
		LogSources []logSourceDoc `yaml:"log_sources"`
	}
	if err := readYAML(filepath.Join(root, "log-sources", "overview.yaml"), &logSourceOverview); err != nil {
		return nil, err
	}
	repo.logSources = logSourceOverview.LogSources

	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("列出 workflow 根目录失败: %w", err)
	}

	reservedDirs := map[string]struct{}{
		"workflows": {}, "correlation-keys": {}, "log-sources": {}, ".git": {},
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, reserved := reservedDirs[entry.Name()]; reserved {
			continue
		}

		overviewPath := filepath.Join(root, entry.Name(), "overview.yaml")
		if _, statErr := os.Stat(overviewPath); statErr != nil {
			continue
		}

		var doc moduleDoc
		if err := readYAML(overviewPath, &doc); err != nil {
			return nil, err
		}
		if strings.TrimSpace(doc.Name) == "" {
			continue
		}
		if strings.TrimSpace(doc.ModuleID) != "" {
			repo.moduleDirs[doc.ModuleID] = doc.Name
		}
		doc.Path = overviewPath
		doc.Keywords = buildModuleKeywords(doc, entry.Name())
		repo.modules[doc.Name] = doc
		repo.moduleDirs[entry.Name()] = doc.Name
		repo.moduleOrder = append(repo.moduleOrder, doc.Name)
	}

	sort.Strings(repo.moduleOrder)
	return repo, nil
}

func readYAML(path string, target interface{}) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取 %s 失败: %w", path, err)
	}
	if err := yaml.Unmarshal(content, target); err != nil {
		return fmt.Errorf("解析 %s 失败: %w", path, err)
	}
	return nil
}

func (r *repository) lookupModule(ref string) (moduleDoc, bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return moduleDoc{}, false
	}
	if doc, ok := r.modules[ref]; ok {
		return doc, true
	}
	if mappedName, ok := r.moduleDirs[ref]; ok {
		doc, exists := r.modules[mappedName]
		return doc, exists
	}
	return moduleDoc{}, false
}

func workflowStepPlans(items []workflowStepDoc) []WorkflowStepPlan {
	result := make([]WorkflowStepPlan, 0, min(len(items), 8))
	for _, item := range items {
		id := firstNonEmpty(item.ID, item.StepID)
		if id == "" && item.Kind == "" && item.Module == "" && item.Description == "" {
			continue
		}
		result = append(result, WorkflowStepPlan{
			ID:          id,
			Kind:        strings.TrimSpace(item.Kind),
			Module:      strings.TrimSpace(item.Module),
			Description: strings.TrimSpace(item.Description),
			DependsOn:   capStrings(item.DependsOn, 4),
			RunIfAny:    capStrings(item.RunIfAny, 4),
			Produces:    capStrings(item.Produces, 6),
		})
		if len(result) >= 8 {
			break
		}
	}
	return result
}

func buildModuleKeywords(doc moduleDoc, dirName string) []string {
	texts := []string{
		doc.Name,
		doc.ModuleID,
		dirName,
		doc.Description,
		strings.Join(doc.EntryHints.Keywords, " "),
		strings.Join(doc.EntryHints.ObjectInputs, " "),
		doc.EntryHints.Priority,
		strings.Join(doc.Execution.RequiredInputs.AnyOf, " "),
		strings.Join(doc.Execution.RequiredInputs.AllOf, " "),
		strings.Join(doc.Execution.OptionalInputs, " "),
		doc.Execution.PrimarySource.SourceAlias,
		doc.Execution.PrimarySource.Logstore,
		doc.Execution.PrimarySource.LogstorePattern,
		strings.Join(doc.Execution.Produces, " "),
		strings.Join(doc.Execution.EvidenceRequirements, " "),
	}
	for _, source := range doc.Execution.AlternateSources {
		texts = append(texts, source.SourceAlias, source.Logstore, source.LogstorePattern, source.SourceRole)
	}
	for _, handoff := range doc.Execution.Handoff {
		texts = append(texts, handoff.TargetModule, handoff.Purpose, strings.Join(handoff.TriggerFacts.AnyOf, " "), strings.Join(handoff.TriggerFacts.AllOf, " "))
	}
	for _, item := range doc.TaskRoutingRules {
		texts = append(texts, item.Condition)
		texts = append(texts, strings.Join(item.Action, " "))
	}
	for _, item := range doc.SupportedIntents {
		texts = append(texts, item.UserSays)
		texts = append(texts, strings.Join(item.MappedAction, " "))
	}
	texts = append(texts, doc.ImportantNotes...)
	texts = append(texts, doc.AnalysisDimensions...)
	texts = append(texts, doc.CoreFields...)
	return extractKeywords(texts...)
}

func extractKeywords(texts ...string) []string {
	collected := make([]string, 0)
	seen := map[string]struct{}{}

	appendToken := func(token string) {
		token = normalizeToken(token)
		if token == "" {
			return
		}
		if _, ignored := stopKeywords[token]; ignored {
			return
		}
		if _, exists := seen[token]; exists {
			return
		}
		seen[token] = struct{}{}
		collected = append(collected, token)
	}

	for _, text := range texts {
		normalized := strings.TrimSpace(text)
		if normalized == "" {
			continue
		}

		for _, match := range backtickRegex.FindAllStringSubmatch(normalized, -1) {
			for _, part := range splitTokenCandidate(match[1]) {
				appendToken(part)
			}
		}

		for _, part := range splitTokenCandidate(normalized) {
			appendToken(part)
		}
	}

	sort.SliceStable(collected, func(i, j int) bool {
		return len([]rune(collected[i])) > len([]rune(collected[j]))
	})
	return collected
}

func splitTokenCandidate(text string) []string {
	text = tokenSplitRegex.ReplaceAllString(text, " ")
	text = strings.ReplaceAll(text, "-", " ")
	text = strings.ReplaceAll(text, "_", " ")
	text = strings.ReplaceAll(text, ".", " ")
	parts := strings.Fields(text)
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		result = append(result, part)
	}
	return result
}

func normalizeToken(token string) string {
	token = strings.ToLower(strings.TrimSpace(token))
	token = strings.Trim(token, " -_/")
	token = spaceRegex.ReplaceAllString(token, " ")
	if token == "" {
		return ""
	}
	runes := []rune(token)
	if len(runes) < 2 {
		return ""
	}
	if len(runes) > 40 {
		return ""
	}
	return token
}

func scoreKeywords(message string, keywords []string) (int, []string) {
	normalizedMessage := normalizeMessage(message)
	score := 0
	matched := make([]string, 0)
	seen := map[string]struct{}{}
	for _, keyword := range keywords {
		if keyword == "" {
			continue
		}
		if !strings.Contains(normalizedMessage, keyword) {
			continue
		}
		if _, exists := seen[keyword]; exists {
			continue
		}
		seen[keyword] = struct{}{}
		matched = append(matched, keyword)
		length := len([]rune(keyword))
		switch {
		case length >= 10:
			score += 8
		case length >= 6:
			score += 5
		default:
			score += 3
		}
	}
	sort.SliceStable(matched, func(i, j int) bool {
		return len([]rune(matched[i])) > len([]rune(matched[j]))
	})
	return score, matched
}

func normalizeMessage(message string) string {
	message = strings.ToLower(strings.TrimSpace(message))
	message = strings.ReplaceAll(message, "\n", " ")
	message = strings.ReplaceAll(message, "\t", " ")
	return spaceRegex.ReplaceAllString(message, " ")
}

func buildReason(matched []string) string {
	if len(matched) == 0 {
		return ""
	}
	return "命中关键词：" + strings.Join(capStrings(matched, 4), "、")
}

func extractModuleRefs(texts ...string) []string {
	result := make([]string, 0)
	seen := map[string]struct{}{}
	for _, text := range texts {
		for _, match := range moduleRefRegex.FindAllStringSubmatch(text, -1) {
			name := strings.TrimSpace(match[1])
			if name == "" {
				continue
			}
			if _, exists := seen[name]; exists {
				continue
			}
			seen[name] = struct{}{}
			result = append(result, name)
		}
	}
	return result
}

func fieldNames(items []fieldDoc) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Name) != "" {
			result = append(result, item.Name)
		}
	}
	return result
}

func extractMethods(items []extractionRuleDoc) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		if item.Source != "" {
			parts = append(parts, item.Source)
		}
		if item.Method != "" {
			parts = append(parts, item.Method)
		}
		if item.Example != "" {
			parts = append(parts, item.Example)
		}
	}
	return strings.Join(parts, " ")
}

func moduleIntentBoost(message, moduleName string) int {
	normalizedMessage := normalizeMessage(message)
	normalizedModule := normalizeToken(moduleName)
	boost := 0
	switch {
	case strings.Contains(normalizedModule, "k8s"):
		boost += keywordGroupScore(normalizedMessage, "pod", "namespace", "oom", "backoff", "crashloopbackoff", "unhealthy", "重启", "探针", "调度", "驱逐", "告警原因", "为什么")
	case strings.Contains(normalizedModule, "backend"):
		boost += keywordGroupScore(normalizedMessage, "backend", "error", "warn", "trace", "接口", "报错", "异常", "类方法", "日志")
	case strings.Contains(normalizedModule, "proxy"), strings.Contains(normalizedModule, "gateway"):
		boost += keywordGroupScore(normalizedMessage, "gateway", "proxy", "nginx", "deploy-proxy", "deploy-gateway", "status", "5xx", "4xx", "请求", "uri", "回源", "网关", "慢请求")
	case strings.Contains(normalizedModule, "postgresql"):
		boost += keywordGroupScore(normalizedMessage, "postgres", "pg", "数据库", "长事务", "复制槽", "wal", "slot", "等待事件", "连接")
	}
	return boost
}

func moduleSourceScore(moduleName, sourceName string) int {
	normalizedModule := normalizeToken(moduleName)
	normalizedSource := normalizeToken(sourceName)
	for key, values := range moduleSourceBoosts {
		if !strings.Contains(normalizedModule, key) {
			continue
		}
		for _, candidate := range values {
			if normalizeToken(candidate) == normalizedSource {
				return 18
			}
		}
	}
	return 0
}

func correlationHintScore(keyName string, moduleHints, sourceHints []string, message string) int {
	normalizedKey := normalizeToken(keyName)
	normalizedMessage := normalizeMessage(message)
	boost := 0
	switch normalizedKey {
	case "pod name", "pod_name":
		boost += keywordGroupScore(normalizedMessage, "pod", "namespace", "重启", "oom", "backoff", "probe")
	case "trace id", "trace_id":
		boost += keywordGroupScore(normalizedMessage, "trace", "trace_id", "链路", "请求id", "requestid")
	case "tenant id", "tenant_id":
		boost += keywordGroupScore(normalizedMessage, "tenant", "租户", "域名", "subdomain")
	case "slot name", "slot_name":
		boost += keywordGroupScore(normalizedMessage, "slot", "cdc", "wal", "复制槽")
	case "database name", "database_name":
		boost += keywordGroupScore(normalizedMessage, "database", "db", "postgres", "数据库")
	}

	for _, item := range moduleHints {
		normalizedModule := normalizeToken(item)
		if strings.Contains(normalizedModule, "k8s") && (normalizedKey == "pod name" || normalizedKey == "pod_name") {
			boost += 4
		}
		if strings.Contains(normalizedModule, "postgresql") && (normalizedKey == "database name" || normalizedKey == "database_name") {
			boost += 4
		}
	}
	for _, item := range sourceHints {
		normalizedSource := normalizeToken(item)
		if strings.Contains(normalizedSource, "application") && (normalizedKey == "trace id" || normalizedKey == "trace_id") {
			boost += 3
		}
		if strings.Contains(normalizedSource, "database") && (normalizedKey == "database name" || normalizedKey == "database_name" || normalizedKey == "slot name" || normalizedKey == "slot_name") {
			boost += 3
		}
	}

	return boost
}

func keywordGroupScore(message string, keywords ...string) int {
	score := 0
	for _, keyword := range keywords {
		if strings.Contains(message, normalizeToken(keyword)) {
			score += 3
		}
	}
	return score
}

func firstLogstorePattern(items []deploymentTypeDoc) string {
	for _, item := range items {
		if strings.TrimSpace(item.LogstorePattern) != "" {
			return strings.TrimSpace(item.LogstorePattern)
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func dedupeStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		key := strings.TrimSpace(value)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, key)
	}
	return result
}

func capStrings(values []string, limit int) []string {
	if limit <= 0 || len(values) <= limit {
		return values
	}
	return append([]string(nil), values[:limit]...)
}

func capModulePlans(values []ModulePlan, limit int) []ModulePlan {
	if limit <= 0 || len(values) <= limit {
		return values
	}
	return append([]ModulePlan(nil), values[:limit]...)
}

func capDataSourcePlans(values []DataSourcePlan, limit int) []DataSourcePlan {
	if limit <= 0 || len(values) <= limit {
		return values
	}
	return append([]DataSourcePlan(nil), values[:limit]...)
}

func dedupeDataSources(values []DataSourcePlan) []DataSourcePlan {
	result := make([]DataSourcePlan, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		key := strings.TrimSpace(value.Name + "|" + value.Project + "|" + value.Logstore + "|" + value.LogstorePattern)
		if key == "|||" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

func firstNonEmptySlice(groups ...[]string) []string {
	for _, values := range groups {
		if len(values) == 0 {
			continue
		}
		cleaned := dedupeStrings(values)
		if len(cleaned) > 0 {
			return cleaned
		}
	}
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
