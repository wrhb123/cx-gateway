package channel

import (
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"ai-proxy-gateway/internal/db"
	"ai-proxy-gateway/internal/models"
)

// Manager 负责渠道选择与负载均衡
type Manager struct {
	db         *db.Database
	mu         sync.RWMutex
	channels   map[int64]*models.Channel
	byType     map[models.ChannelType][]int64
	roundRobin map[models.ChannelType]*atomic.Int64
}

// New 创建新的渠道管理器
func New(database *db.Database) *Manager {
	m := &Manager{
		db:         database,
		channels:   make(map[int64]*models.Channel),
		byType:     make(map[models.ChannelType][]int64),
		roundRobin: make(map[models.ChannelType]*atomic.Int64),
	}
	m.Reload()
	return m
}

// Reload 从数据库重新加载渠道配置
func (m *Manager) Reload() error {
	channels, err := m.db.ListChannels()
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.channels = make(map[int64]*models.Channel)
	m.byType = make(map[models.ChannelType][]int64)

	for i := range channels {
		ch := &channels[i]
		m.channels[ch.ID] = ch
		if ch.Enabled {
			m.byType[ch.Type] = append(m.byType[ch.Type], ch.ID)
		}
	}

	for chType := range m.byType {
		m.roundRobin[chType] = &atomic.Int64{}
	}

	return nil
}

// GetChannel 根据 ID 获取渠道
func (m *Manager) GetChannel(id int64) (*models.Channel, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ch, ok := m.channels[id]
	return ch, ok
}

// SelectChannel 使用负载均衡策略为指定类型选择渠道
func (m *Manager) SelectChannel(chType models.ChannelType, strategy string) (*models.Channel, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ids, ok := m.byType[chType]
	if !ok || len(ids) == 0 {
		return nil, ErrNoChannelAvailable
	}

	var selectedID int64
	switch strategy {
	case "round_robin":
		counter := m.roundRobin[chType]
		idx := counter.Add(1) % int64(len(ids))
		selectedID = ids[idx]
	case "weighted":
		selectedID = weightedSelect(ids, m.channels, true)
	case "random":
		selectedID = ids[rand.Intn(len(ids))]
	default:
		selectedID = ids[0]
	}

	return m.channels[selectedID], nil
}

// IsInPromotion 检查渠道是否在促销期内
func IsInPromotion(ch *models.Channel) bool {
	if ch.PromotionStart == "" || ch.PromotionEnd == "" {
		return false
	}
	start, err1 := time.Parse(time.RFC3339, ch.PromotionStart)
	end, err2 := time.Parse(time.RFC3339, ch.PromotionEnd)
	if err1 != nil || err2 != nil {
		return false
	}
	now := time.Now()
	return now.After(start) && now.Before(end)
}

// ApplyPromotionBoost 如果渠道在促销期内，提升其优先级
func ApplyPromotionBoost(ch *models.Channel) int {
	if IsInPromotion(ch) {
		return ch.Priority + 100 // 促销期内提升 100 优先级
	}
	return ch.Priority
}

// weightedSelect 根据权重选择渠道
func weightedSelect(ids []int64, channels map[int64]*models.Channel, applyPromotion bool) int64 {
	totalWeight := 0
	for _, id := range ids {
		if ch, ok := channels[id]; ok {
			w := ch.Weight
			if applyPromotion && IsInPromotion(ch) {
				w *= 2 // 促销期内权重翻倍
			}
			totalWeight += w
		}
	}
	if totalWeight == 0 {
		return ids[0]
	}

	r := rand.Intn(totalWeight)
	current := 0
	for _, id := range ids {
		if ch, ok := channels[id]; ok {
			w := ch.Weight
			if applyPromotion && IsInPromotion(ch) {
				w *= 2
			}
			current += w
			if r < current {
				return id
			}
		}
	}
	return ids[len(ids)-1]
}

// GetAllChannels 返回所有渠道
func (m *Manager) GetAllChannels() []*models.Channel {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*models.Channel
	for _, ch := range m.channels {
		result = append(result, ch)
	}
	return result
}
