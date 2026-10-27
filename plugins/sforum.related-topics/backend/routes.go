package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"time"

	pluginv2 "github.com/keeptrue/sforum/apps/api/sdk/plugin/v2"
	pluginwire "github.com/keeptrue/sforum/apps/api/sdk/plugin/v2/gen/sforum/plugin/v2"
	protocolwire "github.com/keeptrue/sforum/apps/api/sdk/plugin/v2/gen/sforum/protocol/v2"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	// sameCategoryBonus 是「同分类」的加权。0.15 足以让同分类的弱相关排到
	// 跨分类的中等相关之前，又不足以让完全不相干的同分类帖子越过阈值。
	sameCategoryBonus = 0.15
	// recencyWeight 是时间衰减的最大加成，半年半衰。
	recencyWeight   = 0.10
	recencyHalfLife = 180.0
)

func (s *relatedServer) InvokeRoute(callCtx context.Context, request *pluginwire.RouteRequest) (response *pluginwire.RouteResponse, err error) {
	ctx := request.GetContext()
	// panic 在插件里等于整条路由不可用；转成带原因的失败并留一行日志，
	// 比让宿主只报「runtime failed」有用得多。
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf("related discussions: route panic: %v", recovered)
			response, err = nil, fmt.Errorf("related discussions: route panic: %v", recovered)
		}
	}()
	switch request.GetRouteId() {
	case routeRelatedID:
		return s.handleRelated(callCtx, request)
	default:
		return &pluginwire.RouteResponse{
			Context: routeResponseContext(ctx),
			Error: &protocolwire.ErrorDetail{
				Code:    protocolwire.ErrorCode_ERROR_CODE_NOT_FOUND,
				Reason:  "related.route_unknown",
				Message: "unknown related-discussions route",
			},
		}, nil
	}
}

func (s *relatedServer) handleRelated(callCtx context.Context, request *pluginwire.RouteRequest) (*pluginwire.RouteResponse, error) {
	ctx := request.GetContext()
	topicID := parseInt64(request.GetQueryParameters()["topicId"])
	if topicID <= 0 {
		return &pluginwire.RouteResponse{
			Context: routeResponseContext(ctx),
			Error: &protocolwire.ErrorDetail{
				Code:    protocolwire.ErrorCode_ERROR_CODE_INVALID_ARGUMENT,
				Reason:  "related.topic_id_required",
				Message: "topicId must be a positive integer",
			},
		}, nil
	}
	cfg := s.config
	if !cfg.Enabled {
		return s.respondRelated(ctx, nil, false)
	}

	// 索引为空时顺手排一次回填，本次仍然返回空。判据必须在 ensureIndexed
	// 之前取：后者会先把源帖子自己写进索引，让「空索引」永远不成立。
	if total, countErr := s.store.indexCount(callCtx); countErr == nil && total == 0 {
		s.enqueueBackfill(callCtx, ctx)
	}

	source, ok, err := s.store.ensureIndexed(callCtx, topicID)
	if err != nil {
		log.Printf("related discussions: index lookup failed: %v", err)
		return routeUnavailable(ctx, "related.index_unavailable", "index lookup failed"), nil
	}
	if !ok {
		// 源帖子不在公开视图里（已删/私密），没有可比较的内容。
		return s.respondRelated(ctx, nil, false)
	}

	candidates, cached, err := s.store.candidatesFor(callCtx, source, cfg.IncludeSameCategory)
	if err != nil {
		log.Printf("related discussions: candidate query failed: %v", err)
		return routeUnavailable(ctx, "related.candidates_unavailable", "candidate query failed"), nil
	}
	if len(candidates) == 0 {
		return s.respondRelated(ctx, nil, cached)
	}

	ids := make([]int64, 0, len(candidates))
	for _, item := range candidates {
		ids = append(ids, item.ID)
	}
	live, err := s.store.liveRows(callCtx, ids)
	if err != nil {
		log.Printf("related discussions: core view lookup failed: %v", err)
		return routeUnavailable(ctx, "related.live_rows_unavailable", "core view lookup failed"), nil
	}

	items := rankItems(source, candidates, live, cfg, time.Now().UTC())
	return s.respondRelated(ctx, items, cached)
}

// rankItems 在 Go 侧做最终排序：SQL 只负责「按字面相似度取前 20 个候选」，
// 同分类加权与时间衰减都在这里，避免把打分逻辑塞进 SQL。
func rankItems(source indexRow, candidates []candidate, live map[int64]liveRow, cfg Config, now time.Time) []relatedItem {
	type scored struct {
		item  relatedItem
		score float64
	}
	ranked := make([]scored, 0, len(candidates))
	for _, item := range candidates {
		// 阈值判在「字面相似度」上，而不是加了同分类与时间加成之后的总分：
		// 否则一个毫不相干但同分类的帖子会靠 0.15 的加成混进结果。
		if item.Sim < cfg.MinSimilarity {
			continue
		}
		row, ok := live[item.ID]
		if !ok {
			// 视图里已经没有它：已删除或转私密，直接丢弃而不是给出死链。
			continue
		}
		score := item.Sim
		if cfg.IncludeSameCategory && source.CategorySlug != "" && row.CategorySlug == source.CategorySlug {
			score += sameCategoryBonus
		}
		days := now.Sub(row.LastActivityAt).Hours() / 24
		if days < 0 {
			days = 0
		}
		score += recencyWeight * math.Exp(-days/recencyHalfLife)
		ranked = append(ranked, scored{
			score: score,
			item: relatedItem{
				TopicID:        row.TopicID,
				Title:          row.Title,
				Slug:           row.Slug,
				CategorySlug:   row.CategorySlug,
				CommentCount:   row.CommentCount,
				LastActivityAt: row.LastActivityAt.UTC().Format(time.RFC3339),
			},
		})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score == ranked[j].score {
			return ranked[i].item.TopicID < ranked[j].item.TopicID
		}
		return ranked[i].score > ranked[j].score
	})
	if len(ranked) > cfg.Limit {
		ranked = ranked[:cfg.Limit]
	}
	out := make([]relatedItem, 0, len(ranked))
	for _, entry := range ranked {
		out = append(out, entry.item)
	}
	return out
}

func (s *relatedServer) respondRelated(ctx *protocolwire.RequestContext, items []relatedItem, cached bool) (*pluginwire.RouteResponse, error) {
	values := make([]any, 0, len(items))
	for _, item := range items {
		values = append(values, map[string]any{
			"topicId":        float64(item.TopicID),
			"title":          item.Title,
			"slug":           item.Slug,
			"categorySlug":   item.CategorySlug,
			"commentCount":   float64(item.CommentCount),
			"lastActivityAt": item.LastActivityAt,
		})
	}
	body, err := pluginv2.NewTypedDocument(relatedResponseSchema, map[string]any{
		"items":  values,
		"cached": cached,
	})
	if err != nil {
		log.Printf("related discussions: response document failed: %v", err)
		return nil, err
	}
	return &pluginwire.RouteResponse{
		Context:    routeResponseContext(ctx),
		StatusCode: http.StatusOK,
		Body:       body,
	}, nil
}

// enqueueBackfill 是「首次访问时索引为空」的补救：排一个后台 job 去搬数据，
// 本次请求不等待。失败不报错——下一次请求会再排一次。
func (s *relatedServer) enqueueBackfill(ctx context.Context, parent *protocolwire.RequestContext) {
	host, err := s.Host()
	if err != nil || host == nil {
		return
	}
	payload, err := pluginv2.NewTypedDocument(jobPayload, map[string]any{})
	if err != nil {
		return
	}
	_, _ = host.EnqueueJob(ctx, parent, jobBackfillKind, payload)
}

func routeUnavailable(ctx *protocolwire.RequestContext, reason, message string) *pluginwire.RouteResponse {
	return &pluginwire.RouteResponse{
		Context: routeResponseContext(ctx),
		Error: &protocolwire.ErrorDetail{
			Code:    protocolwire.ErrorCode_ERROR_CODE_UNAVAILABLE,
			Reason:  reason,
			Message: message,
		},
	}
}

func routeResponseContext(request *protocolwire.RequestContext) *protocolwire.ResponseContext {
	if request == nil {
		return &protocolwire.ResponseContext{ServerTime: timestamppb.New(time.Now().UTC())}
	}
	return &protocolwire.ResponseContext{
		RequestId:  request.GetRequestId(),
		Trace:      proto.Clone(request.GetTrace()).(*protocolwire.TraceContext),
		Extension:  proto.Clone(request.GetExtension()).(*protocolwire.ExtensionIdentity),
		ServerTime: timestamppb.New(time.Now().UTC()),
	}
}

func parseInt64(value string) int64 {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}
