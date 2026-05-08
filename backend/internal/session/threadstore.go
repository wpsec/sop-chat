package session

import (
	"crypto/md5"
	"fmt"
	"log"
	"sync"

	"sop-chat/internal/config"
	"sop-chat/pkg/sopchat"
)

// ThreadStore 封装 thread 的内存缓存 + 远端查找/创建逻辑。
type ThreadStore struct {
	cache      sync.Map
	label      string // 日志前缀，如 "[DingTalk]"
	inflightMu sync.Mutex
	inflight   map[string]*threadLookupCall
}

type threadLookupCall struct {
	done     chan struct{}
	threadID string
	err      error
}

// NewThreadStore 创建 ThreadStore，label 用于日志标识平台。
func NewThreadStore(label string) *ThreadStore {
	return &ThreadStore{label: label}
}

// ThreadParams 描述一次 GetOrCreate 所需的参数。
type ThreadParams struct {
	CacheKey     string // 内存缓存 key（不含 suffix，由 GetOrCreate 自动追加）
	SessionRaw   string // 计算 session hash 的原始字符串（不含 suffix）
	EmployeeName string
	Title        string
	Project      string
	Workspace    string
	Region       string
}

// GetOrCreate 查找或创建 thread，返回 threadId。
func (s *ThreadStore) GetOrCreate(client *sopchat.Client, p ThreadParams) (threadID string, err error) {
	suffix := ProcessStartHashSuffix()
	key := p.CacheKey + suffix

	// 1. 内存缓存
	if v, ok := s.cache.Load(key); ok {
		return v.(string), nil
	}

	call, owner := s.beginLookup(key)
	if !owner {
		<-call.done
		return call.threadID, call.err
	}
	defer func() {
		s.finishLookup(key, call, threadID, err)
	}()

	if v, ok := s.cache.Load(key); ok {
		threadID = v.(string)
		return threadID, nil
	}

	// 2. 计算 session hash
	h := md5.Sum([]byte(p.SessionRaw + suffix))
	sessionAttr := fmt.Sprintf("%x", h)

	// 3. 远端查找
	// 进程绑定模式下 session hash 带启动 token，历史线程不会命中，直接新建可少一次远端调用。
	if !BindThreadToProcessEnabled() {
		listResp, listErr := client.ListThreads(p.EmployeeName, []sopchat.ThreadFilter{
			{Key: "session", Value: sessionAttr},
		})
		if listErr != nil {
			log.Printf("%s 列出线程失败（将尝试新建）: %v", s.label, listErr)
		} else if listResp.Body != nil {
			for _, t := range listResp.Body.Threads {
				if t == nil || t.ThreadId == nil || *t.ThreadId == "" {
					continue
				}
				if v, ok := t.Attributes["session"]; ok && v != nil && *v == sessionAttr {
					threadID = *t.ThreadId
					log.Printf("%s 找到已有线程 [employee=%s]: %s", s.label, p.EmployeeName, threadID)
					s.cache.Store(key, threadID)
					return threadID, nil
				}
			}
		}
	}

	// 4. 创建新线程
	log.Printf("%s 创建新线程 [employee=%s] ...", s.label, p.EmployeeName)
	resp, err := client.CreateThread(&sopchat.ThreadConfig{
		EmployeeName: p.EmployeeName,
		Title:        p.Title,
		Attributes:   map[string]interface{}{"session": sessionAttr},
		Project:      p.Project,
		Workspace:    p.Workspace,
		Region:       p.Region,
	})
	if err != nil {
		return "", fmt.Errorf("调用 CreateThread 失败: %w", err)
	}
	if resp.Body == nil || resp.Body.ThreadId == nil || *resp.Body.ThreadId == "" {
		return "", fmt.Errorf("CreateThread 返回了空的 ThreadId")
	}

	threadID = *resp.Body.ThreadId
	log.Printf("%s 新线程创建成功 [employee=%s]: %s", s.label, p.EmployeeName, threadID)
	s.cache.Store(key, threadID)
	return threadID, nil
}

func (s *ThreadStore) beginLookup(key string) (*threadLookupCall, bool) {
	s.inflightMu.Lock()
	defer s.inflightMu.Unlock()
	if s.inflight == nil {
		s.inflight = make(map[string]*threadLookupCall)
	}
	if call, ok := s.inflight[key]; ok {
		return call, false
	}
	call := &threadLookupCall{done: make(chan struct{})}
	s.inflight[key] = call
	return call, true
}

func (s *ThreadStore) finishLookup(key string, call *threadLookupCall, threadID string, err error) {
	s.inflightMu.Lock()
	defer s.inflightMu.Unlock()
	call.threadID = threadID
	call.err = err
	if s.inflight[key] == call {
		delete(s.inflight, key)
	}
	close(call.done)
}

// Store 更新缓存（thread ID 变更时使用）。cacheKey 不含 suffix。
func (s *ThreadStore) Store(cacheKey, threadId string) {
	s.cache.Store(cacheKey+ProcessStartHashSuffix(), threadId)
}

// Delete 删除指定缓存项。cacheKey 不含 suffix。
func (s *ThreadStore) Delete(cacheKey string) {
	s.cache.Delete(cacheKey + ProcessStartHashSuffix())
}

// Load 从缓存读取。cacheKey 不含 suffix。
func (s *ThreadStore) Load(cacheKey string) (string, bool) {
	v, ok := s.cache.Load(cacheKey + ProcessStartHashSuffix())
	if !ok {
		return "", false
	}
	return v.(string), true
}

// NewSopClient 根据 ClientConfig 创建 sopchat.Client。
// 各 bot 原先各自实现的 newSopClient() 逻辑完全相同，统一到此处。
func NewSopClient(cfg *config.ClientConfig) (*sopchat.Client, error) {
	rawClient, err := newRawCMSClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("创建 CMS 客户端失败: %w", err)
	}
	return &sopchat.Client{
		CmsClient:       rawClient,
		AccessKeyId:     cfg.AccessKeyId,
		AccessKeySecret: cfg.AccessKeySecret,
		Endpoint:        cfg.Endpoint,
	}, nil
}

// ThreadVariable 根据渠道和全局 product 配置，返回 thread 所需的 project/workspace/region。
// channelProduct、channelProject、channelWorkspace、channelRegion 来自各渠道自身配置，
// globalProduct 来自全局配置（config.ClientConfig.Product）。
func ThreadVariable(channelProduct, globalProduct, channelProject, channelWorkspace, channelRegion string) (project, workspace, region string) {
	productType := channelProduct
	if productType == "" {
		productType = globalProduct
	}
	if config.IsSlsProduct(productType) {
		return channelProject, "", ""
	}
	return "", channelWorkspace, channelRegion
}
