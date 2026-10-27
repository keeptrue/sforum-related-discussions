# SForum Related Discussions

帖子页正文下方的相关讨论：把被埋没的旧帖重新带回流量。

- ID: `sforum.related-topics`
- Type: `plugin`
- Manifest contract: `sforum.manifest@3`
- Website: https://github.com/keeptrue/sforum-related-discussions

## Optional Contribution Example

The generated manifest does not enable demo contributions by default. After you implement and declare the matching extension route, you can add a host-rendered topic action like this:

```json
{
  "routes": [{"id": "sforum.related-topics.route.bookmark", "contractVersion": "sforum.related-topics.route.bookmark@1", "action": "add", "path": "/topic-actions/bookmark", "methods": ["POST"], "guard": "core.guard.login", "fallback": "closed", "mode": "http", "handler": "route.bookmark", "requestSchema": "sforum.related-topics.route.bookmark.request@1", "responseSchema": "sforum.related-topics.route.bookmark.response@1"}],
  "contributions": [
    {
      "point": "forum.topic.actions",
      "id": "sforum.related-topics.bookmark",
      "order": 200,
      "label": {"zh-CN": "收藏", "en-US": "Bookmark"},
      "icon": "i-lucide-bookmark",
      "payload": {"type": "extensionRoute", "method": "POST", "path": "/topic-actions/bookmark", "confirm": true}
    }
  ]
}
```
