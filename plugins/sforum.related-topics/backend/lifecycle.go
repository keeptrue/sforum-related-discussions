package main

import (
	"context"
	"fmt"
	"strings"

	pluginv2 "github.com/keeptrue/sforum/apps/api/sdk/plugin/v2"
	protocolwire "github.com/keeptrue/sforum/apps/api/sdk/plugin/v2/gen/sforum/protocol/v2"
)

// runLifecycle 覆盖 install/enable/upgrade/rollback/disable/uninstall 的
// plan/execute 步骤。本插件没有需要迁移的持久状态：索引表由 store 在首次
// 连上库时 `CREATE TABLE IF NOT EXISTS` 建好，缓存可以随时丢。
// 所以所有步骤都是幂等 no-op，只按宿主生命周期机的要求回报
// PLANNED → RUNNING → SUCCEEDED。
//
// 声明 lifecycle V2 不只是形式：没有它，宿主 dev watcher 无法把新摘要
// 自动升级到已启用的插件（只能走管理端 restart bridge）。
func runLifecycle(
	ctx context.Context,
	request *protocolwire.LifecycleRequest,
	progress *pluginv2.ProgressStream,
) error {
	if request == nil || progress == nil {
		return fmt.Errorf("missing lifecycle request")
	}
	stepID := strings.TrimSpace(request.GetStepId())
	if stepID == "" {
		stepID = lifecycleStepID(request.GetAction(), request.GetDryRun())
	}
	checkpoint := "plan"
	if !request.GetDryRun() {
		checkpoint = "execute"
	}

	if err := progress.Send(&protocolwire.ProgressUpdate{
		StepId: stepID, State: protocolwire.ProgressState_PROGRESS_STATE_PLANNED,
		CompletedUnits: 0, TotalUnits: 2, Checkpoint: checkpoint + ".start",
	}); err != nil {
		return err
	}
	if err := progress.Send(&protocolwire.ProgressUpdate{
		StepId: stepID, State: protocolwire.ProgressState_PROGRESS_STATE_RUNNING,
		CompletedUnits: 1, TotalUnits: 2, Checkpoint: checkpoint + ".running",
	}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	default:
	}
	return progress.Send(&protocolwire.ProgressUpdate{
		StepId: stepID, State: protocolwire.ProgressState_PROGRESS_STATE_SUCCEEDED,
		CompletedUnits: 2, TotalUnits: 2, Checkpoint: checkpoint + ".done",
	})
}

func lifecycleActionName(action protocolwire.LifecycleAction) string {
	switch action {
	case protocolwire.LifecycleAction_LIFECYCLE_ACTION_INSTALL_PLAN:
		return "install.plan"
	case protocolwire.LifecycleAction_LIFECYCLE_ACTION_INSTALL:
		return "install.execute"
	case protocolwire.LifecycleAction_LIFECYCLE_ACTION_ENABLE:
		return "enable.execute"
	case protocolwire.LifecycleAction_LIFECYCLE_ACTION_DISABLE:
		return "disable.execute"
	case protocolwire.LifecycleAction_LIFECYCLE_ACTION_UPGRADE_PLAN:
		return "upgrade.plan"
	case protocolwire.LifecycleAction_LIFECYCLE_ACTION_UPGRADE_BEFORE:
		return "upgrade.before"
	case protocolwire.LifecycleAction_LIFECYCLE_ACTION_UPGRADE_AFTER:
		return "upgrade.after"
	case protocolwire.LifecycleAction_LIFECYCLE_ACTION_ROLLBACK:
		return "rollback.execute"
	case protocolwire.LifecycleAction_LIFECYCLE_ACTION_UNINSTALL_PLAN:
		return "uninstall.plan"
	case protocolwire.LifecycleAction_LIFECYCLE_ACTION_UNINSTALL:
		return "uninstall.execute"
	case protocolwire.LifecycleAction_LIFECYCLE_ACTION_UNINSTALL_AFTER:
		return "uninstall.after"
	default:
		return "unknown"
	}
}

func lifecycleStepID(action protocolwire.LifecycleAction, dryRun bool) string {
	name := lifecycleActionName(action)
	if dryRun && !strings.HasSuffix(name, ".plan") {
		return name + ".plan"
	}
	return name
}
