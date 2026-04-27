package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildPlanRoutesPodRestartToK8sThenBackend(t *testing.T) {
	root := buildTestWorkflowRepo(t)

	plan, err := BuildPlan(root, "分析示例命名空间里的这个 Pod 重启原因，Pod backoff 且探针失败")
	if err != nil {
		t.Fatalf("BuildPlan returned error: %v", err)
	}
	if plan == nil {
		t.Fatalf("expected non-nil plan")
	}
	if plan.EntryModule == nil || plan.EntryModule.Name != "k8s_runtime_event_log" {
		t.Fatalf("expected k8s entry module, got %+v", plan.EntryModule)
	}
	if len(plan.HandoffModules) == 0 || plan.HandoffModules[0].Name != "backend_app_log" {
		t.Fatalf("expected backend handoff, got %+v", plan.HandoffModules)
	}
	if !containsPlanItem(plan.CorrelationKeys, "pod_name") {
		t.Fatalf("expected pod_name correlation key, got %+v", plan.CorrelationKeys)
	}
	if !containsDataSource(plan.CandidateDataSources, "k8s-event") {
		t.Fatalf("expected k8s-event datasource, got %+v", plan.CandidateDataSources)
	}
	if block := BuildPromptBlock(plan); !strings.Contains(block, "建议入口模块：k8s_runtime_event_log") {
		t.Fatalf("expected prompt block to include entry module, got %q", block)
	}
}

func TestBuildPlanRoutesDatabaseIssueToPostgreSQL(t *testing.T) {
	root := buildTestWorkflowRepo(t)

	plan, err := BuildPlan(root, "分析 PostgreSQL 最近1小时长事务和等待事件，数据库连接很慢")
	if err != nil {
		t.Fatalf("BuildPlan returned error: %v", err)
	}
	if plan == nil {
		t.Fatalf("expected non-nil plan")
	}
	if plan.EntryModule == nil || plan.EntryModule.Name != "postgresql_runtime_log" {
		t.Fatalf("expected postgresql entry module, got %+v", plan.EntryModule)
	}
	if !containsDataSource(plan.CandidateDataSources, "database-long-transaction") {
		t.Fatalf("expected database-long-transaction datasource, got %+v", plan.CandidateDataSources)
	}
	if !containsPlanItem(plan.CorrelationKeys, "database_name") {
		t.Fatalf("expected database_name correlation key, got %+v", plan.CorrelationKeys)
	}
}

func containsPlanItem(items []CorrelationPlan, target string) bool {
	for _, item := range items {
		if item.Name == target {
			return true
		}
	}
	return false
}

func containsDataSource(items []DataSourcePlan, target string) bool {
	for _, item := range items {
		if item.Name == target {
			return true
		}
	}
	return false
}

func buildTestWorkflowRepo(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "workflows", "overview.yaml"), `
files:
  - file: availability-troubleshooting.yaml
    role: 可用性问题分析流程
    alerts: ["POD异常重启-Error"]
  - file: database-troubleshooting.yaml
    role: 数据库问题分析流程
    alerts: ["近 15 分钟 PG 长事务"]
common_workflow:
  steps:
    - title: 识别告警类型
      action: 根据问题匹配工作流
    - title: 提取关联键
      action: 提取 pod、trace、tenant、database 等线索
time_range_strategy:
  availability_alerts:
    range: 告警时间前后 30 分钟
  database_alerts:
    range: 告警时间前后 1 小时
`)
	mustWriteFile(t, filepath.Join(root, "workflows", "availability-troubleshooting.yaml"), `
name: 可用性告警根因分析工作流
description: 支持 Pod 重启、BackOff、探针失败
trigger_conditions:
  - Pod 重启
  - BackOff
  - 探针失败
time_range: 告警时间前后 30 分钟
`)
	mustWriteFile(t, filepath.Join(root, "workflows", "database-troubleshooting.yaml"), `
name: 数据库问题分析流程
description: 支持 PostgreSQL 长事务、等待事件、连接异常
trigger_conditions:
  - PostgreSQL
  - 长事务
  - 等待事件
time_range: 告警时间前后 1 小时
`)
	mustWriteFile(t, filepath.Join(root, "correlation-keys", "overview.yaml"), `
correlation_keys:
  - name: pod_name
    desc: Kubernetes Pod 名称
    scope: ["application", "k8s-event"]
  - name: database_name
    desc: 数据库名称
    scope: ["database-long-transaction"]
`)
	mustWriteFile(t, filepath.Join(root, "log-sources", "overview.yaml"), `
log_sources:
  - name: k8s-event
    description: Kubernetes 事件日志
    project: demo-k8s
    logstore: k8s-event
    fields:
      - name: pod_name
  - name: application
    description: backend 应用日志
    project: demo-k8s
    logstore: backend-log
    fields:
      - name: trace_id
  - name: database-long-transaction
    description: PostgreSQL 长事务
    project: demo-pg
    logstore: pg-stat-activity
    fields:
      - name: 数据库名
`)
	mustWriteFile(t, filepath.Join(root, "k8s-event", "overview.yaml"), `
module: k8s_runtime_event_log
description: Kubernetes 运行事件分析入口
task_routing_rules:
  - condition: 用户提到 Pod重启 BackOff 探针失败
    action:
      - 跳转 ../backend/overview.yaml 做应用补证
supported_intents:
  - user_says: 分析 Pod 重启原因
    mapped_action:
      - 先查 k8s-event
important_notes:
  - 主数据源是 k8s-event
core_fields_reference:
  - pod_name
  - reason
`)
	mustWriteFile(t, filepath.Join(root, "backend", "overview.yaml"), `
module: backend_app_log
description: Backend 应用日志分析入口
task_routing_rules:
  - condition: 用户提到 backend 报错 trace_id
    action:
      - 可联动 ../postgresql/overview.yaml
supported_intents:
  - user_says: 分析 backend error
    mapped_action:
      - 先查 application
important_notes:
  - 应用侧事实来源
core_fields_reference:
  - trace_id
  - message
`)
	mustWriteFile(t, filepath.Join(root, "postgresql", "overview.yaml"), `
module: postgresql_runtime_log
description: PostgreSQL 运行态分析入口
task_routing_rules:
  - condition: 用户提到 PostgreSQL 长事务 等待事件 连接异常
    action:
      - 先读取 postgresql 数据源
supported_intents:
  - user_says: 分析 PostgreSQL 长事务
    mapped_action:
      - 先查 stat activity
important_notes:
  - 数据库侧事实来源
core_fields_reference:
  - 数据库名
  - 等待事件
`)

	return root
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("MkdirAll(%s) failed: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0644); err != nil {
		t.Fatalf("WriteFile(%s) failed: %v", path, err)
	}
}
