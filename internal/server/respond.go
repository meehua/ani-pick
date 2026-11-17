package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

// writeJSONBytes 直接写出已序列化的 JSON（响应缓存命中路径）。
func writeJSONBytes(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func fail(w http.ResponseWriter, status int, code, message, hint string) {
	writeJSON(w, status, map[string]apiError{"error": {Code: code, Message: message, Hint: hint}})
}

func badRequest(w http.ResponseWriter, message string) {
	fail(w, http.StatusBadRequest, "bad_request", message, "")
}

func internalError(w http.ResponseWriter, err error) {
	// 细节只写日志，返回给客户端的信息保持克制。
	slog.Error("请求处理失败", "err", err)
	fail(w, http.StatusInternalServerError, "internal", "服务内部错误", "")
}
