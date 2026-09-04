# CheckSpam

文本内容审核 API：接收一段文本，调用大模型判断是否命中 **涉政 / 色情 / 违禁 / 暴恐 / 广告** 五类风险，返回通过/不通过结论，并对命中词汇做 `*` 脱敏。

## 运行

```bash
go build -o checkspam .
export CHECKSPAM_BASE_URL="https://liuboping.win:8443/v1"   # 必填
./checkspam
```

### 环境变量

| 变量 | 默认值 | 说明 |
|---|---|---|
| `CHECKSPAM_BASE_URL` | **无（必填）** | OpenAI 兼容 base_url，未设置则启动失败 |
| `CHECKSPAM_ADDR` | `:8080` | 监听地址 |
| `CHECKSPAM_MODEL` | `default` | 模型名 |
| `CHECKSPAM_API_KEY` | 空 | 需要鉴权时填 Bearer token |
| `CHECKSPAM_TIMEOUT_SEC` | `60` | 单次模型调用超时（秒） |

## API

线上演示地址：`https://liuboping.win:8443/spam/text/check`（nginx 转发到 `127.0.0.1:8000`）。

### POST /spam/text/check

```bash
curl -X POST https://liuboping.win:8443/spam/text/check \
  -H 'Content-Type: application/json' \
  -d '{"text":"加微信 abc123 领取优惠券"}'
```

响应：

```json
{
  "pass": false,
  "categories": ["广告"],
  "redacted_text": "加*微*信* abc123 领*取*优*惠*券"
}
```

- `pass`：是否通过（无命中类别即 true）
- `categories`：命中的类别名数组，可多个；通过时为空
- `redacted_text`：把命中词汇替换为等长 `*` 的文本；通过时为空字符串

错误码：`400` 请求体非法 JSON；`502` 模型调用/解析失败；`504` 模型超时。

### GET /spam/health

健康检查（线上同 `/spam/` 前缀，nginx 一并转发）：

```bash
curl https://liuboping.win:8443/spam/health
# {"status":"ok"}
```

## 备注

- 类别判定由大模型完成，偶有误判（如把"上门服务"外的普通文本误标），prompt 已按"宁可漏判不可误判"调优；如需更严格/更宽松可调整 `internal/checker/checker.go` 里的 `systemPrompt`。
- 脱敏是模型逐词替换，个别字可能被多打或少打星号。
