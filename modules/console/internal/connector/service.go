package connector

import (
	"time"
)

// Service 是连接器能力的控制台侧门面：凭证表（落盘）+ 会话观测表（内存）。
type Service struct {
	keys     *KeyStore
	sessions *SessionStore
}

// Open 打开凭证表并创建会话观测表。
func Open(path string, now func() time.Time) (*Service, error) {
	keys, err := OpenKeys(path, now)
	if err != nil {
		return nil, err
	}
	return &Service{keys: keys, sessions: NewSessionStore(now)}, nil
}

// Keys / Sessions 暴露子存储。
func (s *Service) Keys() *KeyStore         { return s.keys }
func (s *Service) Sessions() *SessionStore { return s.sessions }
