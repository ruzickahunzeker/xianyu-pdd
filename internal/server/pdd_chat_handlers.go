package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"xianyu-go/internal/auth"
)

type pddChatCaptureInput struct {
	PDDAccountID string `json:"pdd_account_id"`
	MallSN       string `json:"mall_sn"`
	MallID       string `json:"mall_id"`
	GoodsID      string `json:"goods_id"`
	PDDOrderID   string `json:"pdd_order_id"`
	PageURL      string `json:"page_url"`
	TitanWakeups int64  `json:"titan_wakeups"`
	Captures     []struct {
		Endpoint string          `json:"endpoint"`
		Response json.RawMessage `json:"response"`
		At       int64           `json:"at"`
	} `json:"captures"`
}

func (s *Server) mountPDDChatInbox(r chiRouter) {
	r.Post("/api/pdd/chat/capture", s.capturePDDChat)
	r.Get("/api/pdd/chat/conversations", s.listPDDChatConversations)
	r.Get("/api/pdd/chat/messages", s.listPDDChatMessages)
}

// chiRouter keeps this module easy to test without exposing chi implementation
// details to the capture/extraction helpers.
type chiRouter interface {
	Post(string, http.HandlerFunc)
	Get(string, http.HandlerFunc)
}

func (s *Server) capturePDDChat(w http.ResponseWriter, r *http.Request) {
	var in pddChatCaptureInput
	if decodeJSON(r, &in) != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	in.PDDAccountID, in.MallSN = strings.TrimSpace(in.PDDAccountID), strings.TrimSpace(in.MallSN)
	if in.PDDAccountID == "" || in.MallSN == "" || len(in.Captures) > 50 {
		writeErr(w, 422, "缺少拼多多账号、mall_sn，或捕获数量过多")
		return
	}
	uid, now := auth.SessionFromContext(r.Context()).UserID, time.Now().Unix()
	var accountCount int
	if s.Store.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM pdd_accounts WHERE id=? AND user_id=? AND enabled=1`, in.PDDAccountID, uid).Scan(&accountCount) != nil || accountCount != 1 {
		writeErr(w, 422, "拼多多账号不可用")
		return
	}

	s.purchaseMu.Lock()
	defer s.purchaseMu.Unlock()
	var conversationID string
	err := s.Store.DB.QueryRowContext(r.Context(), `SELECT id FROM pdd_chat_conversations WHERE user_id=? AND pdd_account_id=? AND mall_sn=?`, uid, in.PDDAccountID, in.MallSN).Scan(&conversationID)
	if err != nil {
		conversationID = uuid.NewString()
		_, err = s.Store.DB.ExecContext(r.Context(), `INSERT INTO pdd_chat_conversations(id,user_id,pdd_account_id,mall_sn,mall_id,goods_id,pdd_order_id,page_url,last_sync_at,titan_wakeup_at,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?, 'active',?,?)`, conversationID, uid, in.PDDAccountID, in.MallSN, strings.TrimSpace(in.MallID), strings.TrimSpace(in.GoodsID), strings.TrimSpace(in.PDDOrderID), strings.TrimSpace(in.PageURL), now, titanWakeupAt(in.TitanWakeups, now), now, now)
	} else {
		_, err = s.Store.DB.ExecContext(r.Context(), `UPDATE pdd_chat_conversations SET mall_id=CASE WHEN ?<>'' THEN ? ELSE mall_id END,goods_id=CASE WHEN ?<>'' THEN ? ELSE goods_id END,pdd_order_id=CASE WHEN ?<>'' THEN ? ELSE pdd_order_id END,page_url=CASE WHEN ?<>'' THEN ? ELSE page_url END,last_sync_at=?,titan_wakeup_at=CASE WHEN ?>0 THEN ? ELSE titan_wakeup_at END,updated_at=? WHERE id=? AND user_id=?`, strings.TrimSpace(in.MallID), strings.TrimSpace(in.MallID), strings.TrimSpace(in.GoodsID), strings.TrimSpace(in.GoodsID), strings.TrimSpace(in.PDDOrderID), strings.TrimSpace(in.PDDOrderID), strings.TrimSpace(in.PageURL), strings.TrimSpace(in.PageURL), now, in.TitanWakeups, now, now, conversationID, uid)
	}
	if err != nil {
		writeErr(w, 500, "保存拼多多会话失败")
		return
	}

	inserted := 0
	lastMessageID := ""
	for _, capture := range in.Captures {
		if len(capture.Response) == 0 || len(capture.Response) > 2<<20 {
			continue
		}
		at := capture.At / 1000
		if at <= 0 {
			at = now
		}
		raw := compactRawJSON(capture.Response)
		_, _ = s.Store.DB.ExecContext(r.Context(), `INSERT INTO pdd_chat_captures(id,user_id,conversation_id,endpoint,response_json,captured_at) VALUES(?,?,?,?,?,?)`, uuid.NewString(), uid, conversationID, trimLimit(capture.Endpoint, 500), raw, at)
		var payload any
		if json.Unmarshal([]byte(raw), &payload) != nil {
			continue
		}
		for _, message := range extractPDDChatMessages(payload) {
			messageRaw, _ := json.Marshal(message.Raw)
			res, insertErr := s.Store.DB.ExecContext(r.Context(), `INSERT INTO pdd_chat_messages(id,user_id,conversation_id,platform_message_id,direction,message_type,content,platform_created_at,raw_json,created_at) SELECT ?,?,?,?,?,?,?,?,?,? WHERE NOT EXISTS (SELECT 1 FROM pdd_chat_messages WHERE conversation_id=? AND platform_message_id=?)`, uuid.NewString(), uid, conversationID, message.ID, message.Direction, message.Type, message.Content, message.CreatedAt, string(messageRaw), now, conversationID, message.ID)
			if insertErr == nil {
				if n, _ := res.RowsAffected(); n == 1 {
					inserted++
				}
			}
			if message.ID > lastMessageID {
				lastMessageID = message.ID
			}
		}
	}
	if lastMessageID != "" {
		_, _ = s.Store.DB.ExecContext(r.Context(), `UPDATE pdd_chat_conversations SET last_message_id=?,updated_at=? WHERE id=?`, lastMessageID, now, conversationID)
	}
	writeJSON(w, 200, map[string]any{"success": true, "conversation_id": conversationID, "messages_inserted": inserted})
}

func titanWakeupAt(count, now int64) int64 {
	if count > 0 {
		return now
	}
	return 0
}

func compactRawJSON(raw json.RawMessage) string {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return "{}"
	}
	compact, _ := json.Marshal(value)
	return string(compact)
}

type extractedPDDChatMessage struct {
	ID, Direction, Type, Content string
	CreatedAt                    int64
	Raw                          map[string]any
}

func extractPDDChatMessages(root any) []extractedPDDChatMessage {
	out := []extractedPDDChatMessage{}
	seen := map[string]bool{}
	var walk func(any)
	walk = func(value any) {
		switch current := value.(type) {
		case []any:
			for _, item := range current {
				walk(item)
			}
		case map[string]any:
			id := firstText(current, "message_id", "msg_id", "msgId", "mid", "key")
			content := firstText(current, "content", "text", "message", "msg")
			if id != "" && content != "" && !seen[id] {
				seen[id] = true
				direction := messageDirection(current)
				kind := firstText(current, "message_type", "msg_type", "type")
				if kind == "" {
					kind = "unknown"
				}
				out = append(out, extractedPDDChatMessage{ID: trimLimit(id, 190), Direction: direction, Type: trimLimit(kind, 31), Content: trimLimit(content, 20000), CreatedAt: firstInt64(current, "timestamp", "ts", "send_time", "created_at"), Raw: current})
			}
			for _, child := range current {
				walk(child)
			}
		}
	}
	walk(root)
	return out
}

func firstText(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			switch typed := value.(type) {
			case string:
				return strings.TrimSpace(typed)
			case json.Number:
				return typed.String()
			case float64:
				return strconv.FormatInt(int64(typed), 10)
			}
		}
	}
	return ""
}

func firstInt64(values map[string]any, keys ...string) int64 {
	text := firstText(values, keys...)
	value, _ := strconv.ParseInt(text, 10, 64)
	if value > 1_000_000_000_000 {
		value /= 1000
	}
	return value
}

func messageDirection(values map[string]any) string {
	if sender, ok := values["from"].(map[string]any); ok {
		role := strings.ToLower(firstText(sender, "role", "type"))
		if role == "user" || role == "buyer" {
			return "outgoing"
		}
		if role == "mall" || role == "merchant" {
			return "incoming"
		}
	}
	return "unknown"
}

func trimLimit(value string, max int) string {
	runes := []rune(value)
	if len(runes) > max {
		return string(runes[:max])
	}
	return value
}

func (s *Server) listPDDChatConversations(w http.ResponseWriter, r *http.Request) {
	uid := auth.SessionFromContext(r.Context()).UserID
	rows, err := s.Store.DB.QueryContext(r.Context(), `SELECT id,pdd_account_id,mall_sn,mall_id,goods_id,pdd_order_id,page_url,last_message_id,last_sync_at,titan_wakeup_at,status,created_at,updated_at FROM pdd_chat_conversations WHERE user_id=? ORDER BY updated_at DESC LIMIT 200`, uid)
	if err != nil {
		writeErr(w, 500, "读取拼多多会话失败")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, account, mallSN, mallID, goods, orderID, pageURL, lastID, status string
		var lastSync, titan, created, updated int64
		if rows.Scan(&id, &account, &mallSN, &mallID, &goods, &orderID, &pageURL, &lastID, &lastSync, &titan, &status, &created, &updated) == nil {
			out = append(out, map[string]any{"id": id, "pdd_account_id": account, "mall_sn": mallSN, "mall_id": mallID, "goods_id": goods, "pdd_order_id": orderID, "page_url": pageURL, "last_message_id": lastID, "last_sync_at": lastSync, "titan_wakeup_at": titan, "status": status, "created_at": created, "updated_at": updated})
		}
	}
	writeJSON(w, 200, out)
}

func (s *Server) listPDDChatMessages(w http.ResponseWriter, r *http.Request) {
	uid := auth.SessionFromContext(r.Context()).UserID
	query, args := `SELECT m.id,m.conversation_id,m.platform_message_id,m.direction,m.message_type,m.content,m.platform_created_at,m.created_at,c.mall_sn,c.mall_id,c.goods_id,c.pdd_order_id FROM pdd_chat_messages m JOIN pdd_chat_conversations c ON c.id=m.conversation_id WHERE m.user_id=?`, []any{uid}
	if conversationID := strings.TrimSpace(r.URL.Query().Get("conversation_id")); conversationID != "" {
		query += ` AND m.conversation_id=?`
		args = append(args, conversationID)
	}
	rows, err := s.Store.DB.QueryContext(r.Context(), query+` ORDER BY CASE WHEN m.platform_created_at>0 THEN m.platform_created_at ELSE m.created_at END DESC LIMIT 500`, args...)
	if err != nil {
		writeErr(w, 500, "读取拼多多消息失败")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, conversation, platformID, direction, kind, content, mallSN, mallID, goods, orderID string
		var platformAt, created int64
		if rows.Scan(&id, &conversation, &platformID, &direction, &kind, &content, &platformAt, &created, &mallSN, &mallID, &goods, &orderID) == nil {
			out = append(out, map[string]any{"id": id, "conversation_id": conversation, "platform_message_id": platformID, "direction": direction, "message_type": kind, "content": content, "platform_created_at": platformAt, "created_at": created, "mall_sn": mallSN, "mall_id": mallID, "goods_id": goods, "pdd_order_id": orderID})
		}
	}
	writeJSON(w, 200, out)
}
