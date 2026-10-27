package main

const (
	// routeRelatedID 是公开读取路由：GET /related?topicId=N。
	routeRelatedID = "sforum.related-discussions.route.related"
	// relatedResponseSchema 必须与 manifest 的 responseSchema 逐字一致。
	relatedResponseSchema = "sforum.related-discussions.route.related.response@1"

	hookCreatedID = "sforum.related-discussions.hook.topic-created"
	hookUpdatedID = "sforum.related-discussions.hook.topic-updated"
	hookDeletedID = "sforum.related-discussions.hook.topic-deleted"

	// created 与 updated 的快照结构相同，共用一份输入 schema：
	// packageFiles 按路径去重，同一个文件不能声明两次。
	hookTopicInput   = "sforum.related-discussions.hook.topic.input@1"
	hookDeletedInput = "sforum.related-discussions.hook.topic-deleted.input@1"

	jobBackfillID  = "sforum.related-discussions.job.backfill"
	jobReconcileID = "sforum.related-discussions.job.reconcile"

	// 两个 job 都不需要入参，共用一份空对象 schema。
	jobPayload = "sforum.related-discussions.job.payload@1"

	jobBackfillKind  = "sforum.related-discussions.backfill"
	jobReconcileKind = "sforum.related-discussions.reconcile"

	// 设置键与 manifest settings 保持一致；宿主以 SFORUM_SETTING_* 注入。
	settingEnabled       = "sforum.related-discussions.enabled"
	settingLimit         = "sforum.related-discussions.limit"
	settingMinSimilarity = "sforum.related-discussions.min_similarity"
	settingSameCategory  = "sforum.related-discussions.include_same_category"
)
