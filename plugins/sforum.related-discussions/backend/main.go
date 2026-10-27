package main

import (
	"log"

	pluginv2 "github.com/keeptrue/sforum/apps/api/sdk/plugin/v2"
)

// relatedServer 持有 Protocol V2 Server 与插件自己的状态。
// InvokeRoute 在 routes.go 覆盖，hooks / jobs 在各自文件里。
type relatedServer struct {
	*pluginv2.Server
	store  *store
	config Config
}

func main() {
	server, err := newRelatedServer()
	if err != nil {
		log.Fatalf("configure related discussions: %v", err)
	}
	pluginv2.Serve(server)
}

func newRelatedServer() (*relatedServer, error) {
	server := &relatedServer{store: newStore(), config: loadConfig()}

	// 三条 observe hook 共用同一套快照结构；失败策略 fail_open，
	// 索引写不进去不能反过来挡住发帖。
	hooks, err := pluginv2.NewHookRegistry(
		pluginv2.HookDefinition{
			ID: hookCreatedID, Name: "topic.created", Kind: "observe",
			ContractVersion: hookCreatedID + "@1", Handler: hookCreatedID,
			InputSchema: hookTopicInput,
			Execution:   "async", FailurePolicy: "fail_open", TimeoutMS: 5000,
			Execute: server.observeTopicUpsert,
		},
		pluginv2.HookDefinition{
			ID: hookUpdatedID, Name: "topic.updated", Kind: "observe",
			ContractVersion: hookUpdatedID + "@1", Handler: hookUpdatedID,
			InputSchema: hookTopicInput,
			Execution:   "async", FailurePolicy: "fail_open", TimeoutMS: 5000,
			Execute: server.observeTopicUpsert,
		},
		pluginv2.HookDefinition{
			ID: hookDeletedID, Name: "topic.deleted", Kind: "observe",
			ContractVersion: hookDeletedID + "@1", Handler: hookDeletedID,
			InputSchema: hookDeletedInput,
			Execution:   "async", FailurePolicy: "fail_open", TimeoutMS: 5000,
			Execute: server.observeTopicDeleted,
		},
	)
	if err != nil {
		return nil, err
	}

	jobs, err := pluginv2.NewJobRegistry(
		pluginv2.JobDefinition{
			ID: jobBackfillID, ContractVersion: jobBackfillID + "@1",
			Name: jobBackfillKind, Handler: jobBackfillID,
			PayloadSchema: jobPayload,
			RetryPolicy:   "exponential", MaxAttempts: 3, ConcurrencyLimit: 1,
			Execute: server.runBackfillJob,
		},
		pluginv2.JobDefinition{
			ID: jobReconcileID, ContractVersion: jobReconcileID + "@1",
			Name: jobReconcileKind, Handler: jobReconcileID,
			PayloadSchema: jobPayload,
			RetryPolicy:   "exponential", MaxAttempts: 3, ConcurrencyLimit: 1,
			Execute: server.runReconcileJob,
		},
	)
	if err != nil {
		return nil, err
	}

	server.Server = pluginv2.NewServer().
		WithHookRegistry(hooks).
		WithJobRegistry(jobs)
	return server, nil
}
