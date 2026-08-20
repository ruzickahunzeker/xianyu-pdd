package server

import "testing"

func TestExtractPDDChatMessages(t *testing.T) {
	payload := map[string]any{"result": map[string]any{"messages": []any{
		map[string]any{"msg_id": "101", "content": "商家回复", "type": float64(0), "ts": float64(1787040503000), "from": map[string]any{"role": "mall"}},
		map[string]any{"msg_id": "102", "content": "我方消息", "type": "0", "ts": "1787040504", "from": map[string]any{"role": "user"}},
		map[string]any{"msg_id": "101", "content": "重复消息", "from": map[string]any{"role": "mall"}},
	}}}
	messages := extractPDDChatMessages(payload)
	if len(messages) != 2 {
		t.Fatalf("messages=%d, want 2", len(messages))
	}
	if messages[0].Direction != "incoming" || messages[0].CreatedAt != 1787040503 {
		t.Fatalf("incoming=%+v", messages[0])
	}
	if messages[1].Direction != "outgoing" || messages[1].Content != "我方消息" {
		t.Fatalf("outgoing=%+v", messages[1])
	}
}

func TestExtractPDDChatMessagesIgnoresNonMessages(t *testing.T) {
	payload := map[string]any{"tagList": []any{map[string]any{"type": "text", "content": "官方"}}, "status": "ok"}
	if got := extractPDDChatMessages(payload); len(got) != 0 {
		t.Fatalf("messages=%+v, want none", got)
	}
}
