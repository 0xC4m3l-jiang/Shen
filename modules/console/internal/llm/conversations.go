package llm

import (
	"sort"
	"sync"
	"time"

	"shen/modules/console/internal/filestore"
)

const (
	maxConversationsPerUser = 50
	maxMessagesPerConv      = 80
)

// ChatMessage 是对话里的一条消息。
type ChatMessage struct {
	Role      string    `json:"role"` // user / assistant
	Content   string    `json:"content"`
	At        time.Time `json:"at"`
	Model     string    `json:"model,omitempty"`
	Usage     *Usage    `json:"usage,omitempty"`
	LatencyMs int64     `json:"latency_ms,omitempty"`
	Truncated bool      `json:"truncated,omitempty"`
	Error     string    `json:"error,omitempty"` // 调用失败：这条不进后续上下文
}

// Conversation 是一次「选定流量 → 大模型深度分析」的会话。
type Conversation struct {
	ID           string        `json:"id"`
	Owner        string        `json:"owner"`
	Title        string        `json:"title"`
	ProviderID   string        `json:"provider_id"`
	ProviderName string        `json:"provider_name"`
	Model        string        `json:"model"`
	DecisionIDs  []string      `json:"decision_ids"`
	RedactIP     bool          `json:"redact_ip"`
	Context      string        `json:"context"` // 发给模型的流量快照（创建时冻结：之后核心缓冲滚动也不影响追问）
	Messages     []ChatMessage `json:"messages"`
	Tokens       int           `json:"total_tokens"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
}

// ConversationSummary 是列表视图（不带上下文与消息正文）。
type ConversationSummary struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	ProviderName string    `json:"provider_name"`
	Model        string    `json:"model"`
	Traffic      int       `json:"traffic"`
	Messages     int       `json:"messages"`
	Tokens       int       `json:"total_tokens"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (c *Conversation) clone() Conversation {
	cp := *c
	cp.DecisionIDs = append([]string(nil), c.DecisionIDs...)
	cp.Messages = append([]ChatMessage(nil), c.Messages...)
	return cp
}

// ConversationStore 保存会话（按账号隔离：只能看、改、删自己的）。RWMutex + 整表原子落盘。
type ConversationStore struct {
	mu    sync.RWMutex
	path  string
	items map[string]*Conversation
}

type conversationFile struct {
	Conversations []*Conversation `json:"conversations"`
}

// OpenConversations 打开（或新建）会话存储。
func OpenConversations(path string) (*ConversationStore, error) {
	s := &ConversationStore{path: path, items: map[string]*Conversation{}}
	var f conversationFile
	if _, err := filestore.ReadJSON(path, &f); err != nil {
		return nil, err
	}
	for _, c := range f.Conversations {
		if c != nil && c.ID != "" {
			s.items[c.ID] = c
		}
	}
	return s, nil
}

func (s *ConversationStore) persistLocked() error {
	out := conversationFile{Conversations: make([]*Conversation, 0, len(s.items))}
	for _, c := range s.items {
		out.Conversations = append(out.Conversations, c)
	}
	sort.Slice(out.Conversations, func(i, j int) bool { return out.Conversations[i].CreatedAt.Before(out.Conversations[j].CreatedAt) })
	return filestore.WriteJSON(s.path, out)
}

// List 返回某账号的会话摘要（最近更新的在前）。
func (s *ConversationStore) List(owner string) []ConversationSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []ConversationSummary{}
	for _, c := range s.items {
		if c.Owner == owner {
			out = append(out, ConversationSummary{ID: c.ID, Title: c.Title, ProviderName: c.ProviderName, Model: c.Model,
				Traffic: len(c.DecisionIDs), Messages: len(c.Messages), Tokens: c.Tokens, UpdatedAt: c.UpdatedAt})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out
}

// Get 返回会话拷贝；不是本人的会话按「不存在」处理（不泄露他人会话是否存在）。
func (s *ConversationStore) Get(owner, id string) (Conversation, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.items[id]
	if !ok || c.Owner != owner {
		return Conversation{}, false
	}
	return c.clone(), true
}

// Create 保存新会话；超过每人上限时淘汰该账号最久未更新的一条。
func (s *ConversationStore) Create(c Conversation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var mine []*Conversation
	for _, x := range s.items {
		if x.Owner == c.Owner {
			mine = append(mine, x)
		}
	}
	if len(mine) >= maxConversationsPerUser {
		sort.Slice(mine, func(i, j int) bool { return mine[i].UpdatedAt.Before(mine[j].UpdatedAt) })
		delete(s.items, mine[0].ID)
	}
	cp := c.clone()
	s.items[c.ID] = &cp
	return s.persistLocked()
}

// Append 追加消息（超过上限时丢最早的消息对；上下文快照不受影响）。
func (s *ConversationStore) Append(owner, id string, at time.Time, msgs ...ChatMessage) (Conversation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.items[id]
	if !ok || c.Owner != owner {
		return Conversation{}, ErrNotFound
	}
	prev := c.clone()
	for _, m := range msgs {
		c.Messages = append(c.Messages, m)
		if m.Usage != nil {
			c.Tokens += m.Usage.Total
		}
	}
	if over := len(c.Messages) - maxMessagesPerConv; over > 0 {
		c.Messages = append([]ChatMessage(nil), c.Messages[over:]...)
	}
	c.UpdatedAt = at
	if err := s.persistLocked(); err != nil {
		*c = prev
		return Conversation{}, err
	}
	return c.clone(), nil
}

// Delete 删除本人的会话。
func (s *ConversationStore) Delete(owner, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.items[id]
	if !ok || c.Owner != owner {
		return ErrNotFound
	}
	delete(s.items, id)
	if err := s.persistLocked(); err != nil {
		s.items[id] = c
		return err
	}
	return nil
}
