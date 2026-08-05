package updater

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Service talks to the private Watchtower HTTP API. The Docker socket is
// intentionally mounted only in the updater container, never in PocketDrive.
type Service struct {
	url    string
	token  string
	client *http.Client
	mu     sync.Mutex
	last   time.Time
	busy   bool
}

func New(url, token string) *Service {
	return &Service{
		url:    strings.TrimRight(strings.TrimSpace(url), "/"),
		token:  strings.TrimSpace(token),
		client: &http.Client{Timeout: 8 * time.Second},
	}
}

func (s *Service) Enabled() bool { return s.url != "" && s.token != "" }

func (s *Service) Trigger(ctx context.Context) error {
	if !s.Enabled() {
		return errors.New("在线升级未配置,请使用 1Panel 拉取镜像并重建")
	}
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return errors.New("升级请求正在处理中")
	}
	if time.Since(s.last) < time.Minute {
		s.mu.Unlock()
		return errors.New("升级请求过于频繁,请稍后再试")
	}
	s.busy = true
	s.mu.Unlock()
	succeeded := false
	defer func() {
		s.mu.Lock()
		s.busy = false
		if succeeded {
			s.last = time.Now()
		}
		s.mu.Unlock()
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url+"/v1/update", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("连接升级服务失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("升级服务返回 HTTP %d", resp.StatusCode)
	}
	succeeded = true
	return nil
}
