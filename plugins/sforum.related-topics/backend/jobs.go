package main

import (
	"context"

	pluginv2 "github.com/keeptrue/sforum/apps/api/sdk/plugin/v2"
	protocolwire "github.com/keeptrue/sforum/apps/api/sdk/plugin/v2/gen/sforum/protocol/v2"
)

// runBackfillJob 把 Core 视图分批搬进索引。
// 首次查询发现索引为空时会入队一次；管理端的「重建索引」动作也走这里。
func (s *relatedServer) runBackfillJob(ctx context.Context, call *pluginv2.JobCall) error {
	var processed int64
	if err := s.store.runBackfill(ctx, func(done int64) { processed = done }); err != nil {
		return err
	}
	return reportProgress(call, protocolwire.ProgressState_PROGRESS_STATE_SUCCEEDED, processed, processed, "backfilled")
}

// runReconcileJob 是每日对账：删掉已不可见的帖子、补齐落后或缺失的帖子。
func (s *relatedServer) runReconcileJob(ctx context.Context, call *pluginv2.JobCall) error {
	removed, added, err := s.store.reconcile(ctx)
	if err != nil {
		return err
	}
	return reportProgress(call, protocolwire.ProgressState_PROGRESS_STATE_SUCCEEDED, removed+added, removed+added, "reconciled")
}

// reportProgress 只在真有进度流时上报。插件侧不得因为缺少流而失败：
// job 的业务结果比进度条重要。
func reportProgress(call *pluginv2.JobCall, state protocolwire.ProgressState, done, total int64, checkpoint string) error {
	if call == nil || call.Progress == nil {
		return nil
	}
	return call.Progress.Send(&protocolwire.ProgressUpdate{
		StepId:         call.JobID,
		State:          state,
		CompletedUnits: clampUnit(done),
		TotalUnits:     clampUnit(total),
		Checkpoint:     checkpoint,
	})
}

// clampUnit 把行数收进 uint32：进度只是观测量，超出上限时饱和即可。
func clampUnit(value int64) uint32 {
	if value <= 0 {
		return 0
	}
	if value > int64(^uint32(0)) {
		return ^uint32(0)
	}
	return uint32(value)
}
