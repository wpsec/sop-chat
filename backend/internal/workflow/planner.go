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
	"proxy":              {"accesslog"},
	"gateway":            {"accesslog"},
	"postgresql":         {"database-replication", "database-long-transaction"},
	"postgresql_runtime": {"database-replication", "database-long-transaction"},
}

type Plan struct {
	Root                 string            `json:"root"`
	Summary              string            `json:"summary"`
	Workflow             *WorkflowPlan     `json:"workflow,omitempty"`
	EntryModule          *ModulePlan       `json:"entryModule,omitempty"`
	HandoffModules       []ModulePlan      `json:"handoffModules,omitempty"`
	CandidateDataSources []DataSourcePlan  `json:"candidateDataSources,omitempty"`
	CorrelationKeys      []CorrelationPlan `json:"correlationKeys,omitempty"`
	TimeWindow           string            `json:"timeWindow,omitempty"`
	CommonSteps          []string          `json:"commonSteps,omitempty"`
	EvidenceChecklist    []string          `json:"evidenceChecklist,omitempty"`
}

type WorkflowPlan struct {
	Name       string `json:"name"`
	File       string `json:"file,omitempty"`
	Reason     string `json:"reason,omitempty"`
	TimeWindow string `json:"timeWindow,omitempty"`
}

type ModulePlan struct {
	Name        string   `json:"name"`
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
	Files             []workflowFileRef            `yaml:"files"`
	CommonWorkflow    commonWorkflowDoc            `yaml:"common_workflow"`
	TimeRangeStrategy map[string]timeRangeStrategy `yaml:"time_range_strategy"`
}

type workflowFileRef struct {
	File   string   `yaml:"file"`
	Role   string   `yaml:"role"`
	Alerts []string `yaml:"alerts"`
	Note   string   `yaml:"note"`
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
	Name              string   `yaml:"name"`
	Description       string   `yaml:"description"`
	TriggerConditions []string `yaml:"trigger_conditions"`
	TimeRange         string   `yaml:"time_range"`
}

type moduleDoc struct {
	Name               string            `yaml:"module"`
	Description        string            `yaml:"description"`
	TaskRoutingRules   []routingRuleDoc  `yaml:"task_routing_rules"`
	SupportedIntents   []intentRuleDoc   `yaml:"supported_intents"`
	ImportantNotes     []string          `yaml:"important_notes"`
	AnalysisDimensions []string          `yaml:"analysis_dimensions"`
	CoreFields         []string          `yaml:"core_fields_reference"`
	QuickLinks         map[string]string `yaml:"quick_links"`
	Path               string
	Keywords           []string
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
		"执行约束：先在入口模块拿到直接证据，再决定是否联动下游模块；证据不足时明确写待确认项，不要把候选方向当根因；输出时请显式区分直接触发器、应用侧原因、依赖侧原因和影响范围。",
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
			Name:       firstNonEmpty(top.doc.Name, top.doc.Role),
			File:       top.doc.File,
			Reason:     buildReason(top.matchedWords),
			TimeWindow: firstNonEmpty(top.doc.TimeRange, plan.TimeWindow),
		}
		if plan.Workflow.TimeWindow != "" {
			plan.TimeWindow = plan.Workflow.TimeWindow
		}
	}

	if len(moduleMatches) > 0 {
		entry := moduleMatches[0]
		plan.EntryModule = &ModulePlan{
			Name:        entry.doc.Name,
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
	for _, rule := range entryDoc.TaskRoutingRules {
		joinedActions := strings.Join(rule.Action, " ")
		for _, ref := range extractModuleRefs(rule.Condition, joinedActions) {
			refDoc, exists := r.modules[ref]
			if !exists {
				if mappedName, mapped := r.moduleDirs[ref]; mapped {
					refDoc, exists = r.modules[mappedName]
				}
			}
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

	result := make([]DataSourcePlan, 0, min(3, len(matches)))
	for _, item := range matches[:min(3, len(matches))] {
		result = append(result, DataSourcePlan{
			Name:            item.doc.Name,
			Project:         strings.TrimSpace(item.doc.Project),
			Logstore:        strings.TrimSpace(item.doc.Logstore),
			LogstorePattern: firstLogstorePattern(item.doc.DeploymentTypes),
			Reason:          buildReason(capStrings(item.matchedWords, 4)),
		})
	}
	return result
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
			items = append(items, capStrings(doc.CoreFields, 3)...)
		}
	}
	for _, handoff := range handoffs {
		if doc, ok := r.modules[handoff.Name]; ok {
			items = append(items, capStrings(doc.CoreFields, 1)...)
		}
	}

	return capStrings(dedupeStrings(items), 8)
}

func (r *repository) matchWorkflows(message string) []scoredWorkflow {
	matches := make([]scoredWorkflow, 0, len(r.workflows))
	for _, item := range r.workflows {
		keywords := extractKeywords(item.Name, item.Role, item.Description, strings.Join(item.TriggerConditions, " "), strings.Join(item.Alerts, " "))
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

	repo.workflows = make([]workflowDoc, 0, len(repo.workflowOverview.Files))
	for _, item := range repo.workflowOverview.Files {
		doc := workflowDoc{
			File:   item.File,
			Role:   item.Role,
			Alerts: item.Alerts,
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

func buildModuleKeywords(doc moduleDoc, dirName string) []string {
	texts := []string{doc.Name, dirName, doc.Description}
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
		boost += keywordGroupScore(normalizedMessage, "pod", "namespace", "oom", "backoff", "crashloopbackoff", "unhealthy", "重启", "探针", "调度", "驱逐")
	case strings.Contains(normalizedModule, "backend"):
		boost += keywordGroupScore(normalizedMessage, "backend", "error", "warn", "trace", "接口", "报错", "异常", "类方法", "日志")
	case strings.Contains(normalizedModule, "proxy"), strings.Contains(normalizedModule, "gateway"):
		boost += keywordGroupScore(normalizedMessage, "gateway", "status", "5xx", "4xx", "请求", "uri", "回源", "网关", "慢请求")
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
