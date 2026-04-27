package session

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"

	cmsclient "github.com/alibabacloud-go/cms-20240330/v6/client"
	openapiutil "github.com/alibabacloud-go/darabonba-openapi/v2/utils"
	"github.com/alibabacloud-go/tea/tea"

	"sop-chat/internal/config"
	"sop-chat/pkg/sopchat"
)

// ClientCache reuses SDK clients for the same credential set.
type ClientCache struct {
	mu         sync.Mutex
	rawClients sync.Map
}

var defaultClientCache = NewClientCache()

func NewClientCache() *ClientCache {
	return &ClientCache{}
}

func CachedRawCMSClient(cfg *config.ClientConfig) (*cmsclient.Client, error) {
	return defaultClientCache.RawCMSClient(cfg)
}

func CachedSopClient(cfg *config.ClientConfig) (*sopchat.Client, error) {
	return defaultClientCache.SopClient(cfg)
}

func (c *ClientCache) SopClient(cfg *config.ClientConfig) (*sopchat.Client, error) {
	rawClient, err := c.RawCMSClient(cfg)
	if err != nil {
		return nil, err
	}
	return &sopchat.Client{
		CmsClient:       rawClient,
		AccessKeyId:     cfg.AccessKeyId,
		AccessKeySecret: cfg.AccessKeySecret,
		Endpoint:        cfg.Endpoint,
	}, nil
}

func (c *ClientCache) RawCMSClient(cfg *config.ClientConfig) (*cmsclient.Client, error) {
	if cfg == nil || strings.TrimSpace(cfg.AccessKeyId) == "" || strings.TrimSpace(cfg.AccessKeySecret) == "" {
		return nil, fmt.Errorf("cloud credentials are empty")
	}
	key := clientCacheKey(cfg)
	if v, ok := c.rawClients.Load(key); ok {
		return v.(*cmsclient.Client), nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if v, ok := c.rawClients.Load(key); ok {
		return v.(*cmsclient.Client), nil
	}
	rawClient, err := newRawCMSClient(cfg)
	if err != nil {
		return nil, err
	}
	c.rawClients.Store(key, rawClient)
	return rawClient, nil
}

func newRawCMSClient(cfg *config.ClientConfig) (*cmsclient.Client, error) {
	if cfg == nil || strings.TrimSpace(cfg.AccessKeyId) == "" || strings.TrimSpace(cfg.AccessKeySecret) == "" {
		return nil, fmt.Errorf("cloud credentials are empty")
	}
	cmsConfig := &openapiutil.Config{
		AccessKeyId:      tea.String(cfg.AccessKeyId),
		AccessKeySecret:  tea.String(cfg.AccessKeySecret),
		Endpoint:         tea.String(cfg.Endpoint),
		SignatureVersion: tea.String("v3"),
	}
	return cmsclient.NewClient(cmsConfig)
}

func clientCacheKey(cfg *config.ClientConfig) string {
	h := sha256.New()
	writeCacheField(h, cfg.AccessKeyId)
	writeCacheField(h, cfg.AccessKeySecret)
	writeCacheField(h, cfg.Endpoint)
	return hex.EncodeToString(h.Sum(nil))
}

func writeCacheField(h interface{ Write([]byte) (int, error) }, value string) {
	_, _ = h.Write([]byte(value))
	_, _ = h.Write([]byte{0})
}
