// Package checker 调用大模型 API 对文本做内容审核。
package checker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// 五个审核类别，顺序固定。
var Categories = []string{"涉政", "色情", "违禁", "暴恐", "广告"}

const systemPrompt = `你是内容审核引擎。判断给定文本是否命中以下类别，只有明确证据才标记，宁可漏判不可误判：
- 涉政：涉及国家领导人、政治人物、政治事件、反动言论
- 色情：露骨性描写、卖淫招嫖
- 违禁：毒品、枪支弹药等违禁品交易
- 暴恐：暴力恐怖内容、煽动暴力、血腥威胁
- 广告：营销推广、引流（加微信/QQ/联系方式）、促销广告
普通脏话、骂人不构成任何类别。
脱敏规则：把命中的具体敏感词整体替换为 ***（三个星号，固定），不得改动未命中的字。例如「联系微信 abc123」应输出「联系微信 ***」，而不是把每个汉字都打星。
对命中的具体词汇在 redacted_text 中按上述规则脱敏；pass=true 时 redacted_text 为空字符串。categories 只放确实命中的类别名。
只输出一个 JSON 对象，不要 markdown：
{"pass": true|false, "categories": [命中的类别名数组], "redacted_text": "脱敏后全文"}`

// Result 是审核结论。
type Result struct {
	Pass         bool     `json:"pass"`
	Categories   []string `json:"categories"` // 命中的类别名，可多个；通过时为空
	RedactedText string   `json:"redacted_text"`
}

// Config 描述 LLM 接入方式。
type Config struct {
	BaseURL string // OpenAI 兼容 base_url，如 https://host:8443/v1
	Model   string // 模型名
	APIKey  string // 可为空（无需鉴权时）
	Timeout time.Duration
}

// Client 是审核客户端。
type Client struct {
	cfg   Config
	httpc *http.Client
}

// New 创建客户端。
func New(cfg Config) *Client {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}
	return &Client{
		cfg:   cfg,
		httpc: &http.Client{Timeout: cfg.Timeout},
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type chatRequest struct {
	Model          string         `json:"model"`
	Temperature    float64        `json:"temperature"`
	Messages       []chatMessage  `json:"messages"`
	ResponseFormat responseFormat `json:"response_format"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

// Check 审核一段文本。
func (c *Client) Check(ctx context.Context, text string) (*Result, error) {
	if strings.TrimSpace(text) == "" {
		return &Result{Pass: true, Categories: []string{}}, nil
	}

	var body bytes.Buffer
	_ = json.NewEncoder(&body).Encode(chatRequest{
		Model:          c.cfg.Model,
		Temperature:    0,
		ResponseFormat: responseFormat{Type: "json_object"},
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: text},
		},
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.cfg.BaseURL, "/")+"/chat/completions", &body)
	if err != nil {
		return nil, fmt.Errorf("构建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}

	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("调用模型失败: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("模型返回 HTTP %d: %s", resp.StatusCode, truncate(string(data), 200))
	}

	var cr chatResponse
	if err := json.Unmarshal(data, &cr); err != nil {
		return nil, fmt.Errorf("解析模型响应失败: %w", err)
	}
	if cr.Error != nil && cr.Error.Message != "" {
		return nil, errors.New("模型错误: " + cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return nil, errors.New("模型未返回任何内容")
	}

	raw := strings.TrimSpace(cr.Choices[0].Message.Content)
	res, err := parseResult(raw)
	if err != nil {
		return nil, fmt.Errorf("模型输出不是合法审核结果: %w (原始: %s)", err, truncate(raw, 200))
	}
	return res, nil
}

// parseResult 解析并规范化模型输出的 JSON。
func parseResult(raw string) (*Result, error) {
	raw = stripCodeFence(raw)
	var r Result
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return nil, err
	}
	// 只保留五个合法类别，去重。
	seen := map[string]bool{}
	cats := []string{}
	for _, cat := range r.Categories {
		cat = strings.TrimSpace(cat)
		if cat == "" || seen[cat] {
			continue
		}
		valid := false
		for _, c := range Categories {
			if cat == c {
				valid = true
				break
			}
		}
		if !valid {
			continue
		}
		seen[cat] = true
		cats = append(cats, cat)
	}
	r.Categories = cats
	// 有命中类别则必然不通过，保持一致。
	r.Pass = len(r.Categories) == 0
	return &r, nil
}

func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		lines := strings.Split(s, "\n")
		// 去掉首行 ```json 和末尾 ```
		if len(lines) >= 2 {
			lines = lines[1:]
		}
		for i, l := range lines {
			if strings.TrimSpace(l) == "```" && i == len(lines)-1 {
				lines = lines[:i]
				break
			}
		}
		s = strings.Join(lines, "\n")
	}
	return strings.TrimSpace(s)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
