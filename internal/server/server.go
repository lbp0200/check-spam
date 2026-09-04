// Package server 提供 HTTP API。
package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"checkspam/internal/checker"
)

// Request 是 /spam/text/check 的请求体。
type Request struct {
	Text string `json:"text"`
}

// Response 是 /spam/text/check 的响应体。
type Response struct {
	Pass          bool     `json:"pass"`
	Categories    []string `json:"categories"`
	RedactedText string   `json:"redacted_text"`
}

type errBody struct {
	Error string `json:"error"`
}

// New 创建 HTTP handler。
func New(ctx context.Context, c *checker.Client) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/spam/health", handleHealth)
	mux.HandleFunc("/spam/text/check", handleCheck(ctx, c))
	return mux
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleCheck(ctx context.Context, c *checker.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, errBody{Error: "method not allowed, use POST"})
			return
		}

		var req Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errBody{Error: "请求体不是合法 JSON"})
			return
		}
		res, err := c.Check(ctx, req.Text)
		if err != nil {
			var code int
			switch {
			case errors.Is(err, context.DeadlineExceeded):
				code = http.StatusGatewayTimeout
			default:
				code = http.StatusBadGateway
			}
			writeJSON(w, code, errBody{Error: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, Response{
			Pass:          res.Pass,
			Categories:    res.Categories,
			RedactedText:  res.RedactedText,
		})
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	if err := enc.Encode(v); err != nil {
		slog.Error("写入响应失败", "err", err)
	}
}
