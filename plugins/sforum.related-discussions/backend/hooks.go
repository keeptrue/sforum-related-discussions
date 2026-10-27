package main

import (
	"context"
	"time"

	pluginv2 "github.com/keeptrue/sforum/apps/api/sdk/plugin/v2"
)

// observeTopicUpsert 处理 topic.created / topic.updated。
//
// 两个事件共用一套快照结构（topicId/title/topic/category），所以合并成一个
// handler。状态直接取快照里的 topic.status：locked 仍然公开，hidden 会在
// 读取时被 Core 视图挡掉，不需要在这里区分。
func (s *relatedServer) observeTopicUpsert(ctx context.Context, call *pluginv2.HookCall) (*pluginv2.HookResult, error) {
	row, ok := indexRowFromSnapshot(pluginv2.TypedDocumentValues(call.Payload))
	if !ok {
		return &pluginv2.HookResult{Accepted: true}, nil
	}
	if err := s.store.upsert(ctx, row); err != nil {
		return nil, err
	}
	// 标题变了，缓存的候选集就过期了。
	s.store.invalidateCache(ctx, row.TopicID)
	return &pluginv2.HookResult{Accepted: true}, nil
}

func (s *relatedServer) observeTopicDeleted(ctx context.Context, call *pluginv2.HookCall) (*pluginv2.HookResult, error) {
	topicID := int64Value(pluginv2.TypedDocumentValues(call.Payload)["topicId"])
	if topicID <= 0 {
		return &pluginv2.HookResult{Accepted: true}, nil
	}
	if err := s.store.remove(ctx, topicID); err != nil {
		return nil, err
	}
	return &pluginv2.HookResult{Accepted: true}, nil
}

// indexRowFromSnapshot 把事件快照投影成索引行。
// 字段路径见 Models/Forum/topic_events.go 的 buildTopicEventPayload。
func indexRowFromSnapshot(values map[string]any) (indexRow, bool) {
	topicID := int64Value(values["topicId"])
	if topicID <= 0 {
		return indexRow{}, false
	}
	row := indexRow{
		TopicID:        topicID,
		Title:          stringValue(values["title"]),
		Status:         "active",
		LastActivityAt: time.Now().UTC(),
	}
	if topic, ok := mapValue(values["topic"]); ok {
		if slug := stringValue(topic["slug"]); slug != "" {
			row.Slug = slug
		}
		if status := stringValue(topic["status"]); status != "" {
			row.Status = status
		}
	}
	if category, ok := mapValue(values["category"]); ok {
		row.CategoryID = int64Value(category["id"])
		row.CategorySlug = stringValue(category["slug"])
	}
	if row.Title == "" {
		return indexRow{}, false
	}
	return row, true
}

func mapValue(value any) (map[string]any, bool) {
	typed, ok := value.(map[string]any)
	return typed, ok
}

// int64Value 处理 structpb 的数字：JSON 解出来一律是 float64。
func int64Value(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case int64:
		return typed
	case int:
		return int64(typed)
	case string:
		return parseInt64(typed)
	default:
		return 0
	}
}

func stringValue(value any) string {
	if typed, ok := value.(string); ok {
		return typed
	}
	return ""
}
