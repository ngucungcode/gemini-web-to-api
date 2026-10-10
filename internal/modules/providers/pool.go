package providers

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

type AccountStatus struct {
	Name       string   `json:"name"`
	Healthy    bool     `json:"healthy"`
	Models     []string `json:"models"`
	MaskedPSID string   `json:"psid"`
}

type Pool struct {
	mu        sync.RWMutex
	clients   []*Client
	rr        atomic.Uint64
	log       *zap.Logger
	backoff   map[string]time.Time
	backoffMu sync.Mutex
}

func NewPool(clients []*Client, log *zap.Logger) *Pool {
	if log == nil {
		log = zap.NewNop()
	}
	cp := make([]*Client, 0, len(clients))
	for _, c := range clients {
		if c != nil {
			cp = append(cp, c)
		}
	}
	return &Pool{clients: cp, log: log, backoff: make(map[string]time.Time)}
}

func (p *Pool) Size() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.clients)
}

func (p *Pool) Add(c *Client) {
	if c == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clients = append(p.clients, c)
}

func (p *Pool) Remove(name string) *Client {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, c := range p.clients {
		if c.AccountName() == name {
			p.clients = append(p.clients[:i], p.clients[i+1:]...)
			return c
		}
	}
	return nil
}

func (p *Pool) Get(name string) *Client {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, c := range p.clients {
		if c.AccountName() == name {
			return c
		}
	}
	return nil
}

func (p *Pool) Snapshot() []*Client {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]*Client, len(p.clients))
	copy(out, p.clients)
	return out
}

func (p *Pool) Statuses() []AccountStatus {
	out := make([]AccountStatus, 0)
	for _, c := range p.Snapshot() {
		out = append(out, c.AccountStatus())
	}
	return out
}

func (p *Pool) backoffUntil(name string) time.Time {
	p.backoffMu.Lock()
	defer p.backoffMu.Unlock()
	return p.backoff[name]
}

func (p *Pool) markFailed(name string, d time.Duration) {
	p.backoffMu.Lock()
	defer p.backoffMu.Unlock()
	p.backoff[name] = time.Now().Add(d)
}

func (p *Pool) candidates() []*Client {
	now := time.Now()
	all := p.Snapshot()
	healthy := make([]*Client, 0, len(all))
	for _, c := range all {
		if !c.IsHealthy() {
			continue
		}
		if until := p.backoffUntil(c.AccountName()); now.Before(until) {
			continue
		}
		healthy = append(healthy, c)
	}
	if len(healthy) > 0 {
		return healthy
	}
	for _, c := range all {
		if c.IsHealthy() {
			return []*Client{c}
		}
	}
	return all
}

func (p *Pool) Next() *Client {
	cands := p.candidates()
	if len(cands) == 0 {
		return nil
	}
	i := int(p.rr.Add(1)-1) % len(cands)
	return cands[i]
}

func (p *Pool) GenerateContent(ctx context.Context, prompt string, options ...GenerateOption) (*Response, error) {
	cands := p.candidates()
	if len(cands) == 0 {
		return nil, fmt.Errorf("no Gemini accounts configured; add cookies via cookies/*.txt or POST /admin/accounts (see docs/multi-accounts.md)")
	}
	start := int(p.rr.Add(1)-1) % len(cands)
	var lastErr error
	for k := 0; k < len(cands); k++ {
		c := cands[(start+k)%len(cands)]
		resp, err := c.GenerateContent(ctx, prompt, options...)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		p.markFailed(c.AccountName(), backoffFor(err))
		p.log.Warn("Account request failed, trying next account",
			zap.String("account", c.AccountName()), zap.Error(err))
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("all Gemini accounts failed")
	}
	return nil, lastErr
}

func backoffFor(err error) time.Duration {
	if err == nil {
		return 0
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "429") || strings.Contains(msg, "quota") || strings.Contains(msg, "rate"):
		return 5 * time.Minute
	case strings.Contains(msg, "401") || strings.Contains(msg, "403") || strings.Contains(msg, "auth"):
		return 10 * time.Minute
	default:
		return 30 * time.Second
	}
}

func (p *Pool) ResolveModel(requested string) (ModelInfo, error) {
	for _, c := range p.candidates() {
		if m, err := c.ResolveModel(requested); err == nil {
			return m, nil
		}
	}
	for _, c := range p.Snapshot() {
		if m, err := c.ResolveModel(requested); err == nil {
			return m, nil
		}
	}
	return ModelInfo{}, &ModelSelectionError{Message: fmt.Sprintf("unsupported Gemini model %q on every configured account", strings.TrimSpace(requested))}
}

func (p *Pool) ListModels() []ModelInfo {
	seen := make(map[string]bool)
	var out []ModelInfo
	for _, c := range p.Snapshot() {
		for _, m := range c.ListModels() {
			if !seen[m.ID] {
				seen[m.ID] = true
				out = append(out, m)
			}
		}
	}
	return out
}

func (p *Pool) StartChat(options ...ChatOption) ChatSession {
	c := p.Next()
	if c == nil {
		return &stuckChatSession{err: fmt.Errorf("no Gemini accounts configured")}
	}
	return &poolChatSession{pool: p, first: c.StartChat(options...), opts: options}
}

func (p *Pool) DeepResearch(ctx context.Context, query string, opts ...DeepResearchOption) (*DeepResearchResult, error) {
	cands := p.candidates()
	if len(cands) == 0 {
		return nil, fmt.Errorf("no Gemini accounts configured")
	}
	var lastErr error
	for _, c := range cands {
		r, err := c.DeepResearch(ctx, query, opts...)
		if err == nil {
			return r, nil
		}
		lastErr = err
		p.markFailed(c.AccountName(), backoffFor(err))
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("all Gemini accounts failed")
	}
	return nil, lastErr
}

func (p *Pool) DeepResearchStream(ctx context.Context, query string, cb ProgressCallback, opts ...DeepResearchOption) error {
	cands := p.candidates()
	if len(cands) == 0 {
		return fmt.Errorf("no Gemini accounts configured")
	}
	var lastErr error
	for _, c := range cands {
		err := c.DeepResearchStream(ctx, query, cb, opts...)
		if err == nil {
			return nil
		}
		lastErr = err
		p.markFailed(c.AccountName(), backoffFor(err))
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("all Gemini accounts failed")
	}
	return lastErr
}

func (p *Pool) Close() error {
	for _, c := range p.Snapshot() {
		_ = c.Close()
	}
	return nil
}

func (p *Pool) GetName() string { return "gemini" }

func (p *Pool) IsHealthy() bool {
	for _, c := range p.Snapshot() {
		if c.IsHealthy() {
			return true
		}
	}
	return false
}

func (p *Pool) Init(ctx context.Context) error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(p.Snapshot()))
	for _, c := range p.Snapshot() {
		wg.Add(1)
		go func(cl *Client) {
			defer wg.Done()
			if err := cl.Init(ctx); err != nil {
				p.log.Error("Account init failed", zap.String("account", cl.AccountName()), zap.Error(err))
				errCh <- err
			}
		}(c)
	}
	wg.Wait()
	close(errCh)
	fails := 0
	for range errCh {
		fails++
	}
	if fails == p.Size() && p.Size() > 0 {
		return fmt.Errorf("all %d Gemini account(s) failed to initialize", fails)
	}
	return nil
}

type poolChatSession struct {
	pool  *Pool
	first ChatSession
	opts  []ChatOption
}

func (s *poolChatSession) SendMessage(ctx context.Context, message string, options ...GenerateOption) (*Response, error) {
	if s.first != nil {
		if resp, err := s.first.SendMessage(ctx, message, options...); err == nil {
			return resp, nil
		}
		s.first = nil
	}
	c := s.pool.Next()
	if c == nil {
		return nil, fmt.Errorf("no Gemini accounts configured")
	}
	sess := c.StartChat(s.opts...)
	resp, err := sess.SendMessage(ctx, message, options...)
	if err == nil {
		s.first = sess
	}
	return resp, err
}

func (s *poolChatSession) GetMetadata() *SessionMetadata {
	if s.first != nil {
		return s.first.GetMetadata()
	}
	return &SessionMetadata{}
}

func (s *poolChatSession) GetHistory() []Message {
	if s.first != nil {
		return s.first.GetHistory()
	}
	return nil
}

func (s *poolChatSession) Clear() {
	if s.first != nil {
		s.first.Clear()
	}
}

type stuckChatSession struct{ err error }

func (s *stuckChatSession) SendMessage(context.Context, string, ...GenerateOption) (*Response, error) {
	return nil, s.err
}
func (s *stuckChatSession) GetMetadata() *SessionMetadata { return &SessionMetadata{} }
func (s *stuckChatSession) GetHistory() []Message         { return nil }
func (s *stuckChatSession) Clear()                        {}
