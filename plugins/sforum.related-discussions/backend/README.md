# Backend Stub

Build a HashiCorp go-plugin compatible executable named `plugin` in this directory before enabling `sforum.related-discussions`.

Use the Protocol V2 public Go SDK:

```go
package main

import pluginv2 "github.com/keeptrue/sforum/apps/api/sdk/plugin/v2"

func main() { pluginv2.Serve(pluginv2.NewServer()) }
```

After replacing the generated stub, refresh the exact file digests:

```bash
cd apps/api && go run ./cmd/sforum extension digest --write <package-root>
```

Contract test (no binary required while scaffolding):

```bash
cd apps/api && go run ./cmd/sforum extension test --allow-scaffold <package-root>
```
