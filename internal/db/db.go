package db

import (
	"database/sql"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"ai-proxy-gateway/internal/models"
	_ "github.com/mattn/go-sqlite3"
)

// Database 封装 SQLite 数据库连接
type Database struct {
	Conn *sql.DB
}

// New 创建新的数据库实例
func New(dbPath string) (*Database, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	conn, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	// 启用 WAL 模式以提升并发性能
	if _, err := conn.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return nil, err
	}
	if _, err := conn.Exec("PRAGMA busy_timeout=5000"); err != nil {
		return nil, err
	}

	db := &Database{Conn: conn}
	if err := db.migrate(); err != nil {
		return nil, err
	}

	return db, nil
}

// migrate 执行数据库表结构初始化
func (db *Database) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS channels (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		type TEXT NOT NULL,
		base_url TEXT NOT NULL,
		api_key TEXT NOT NULL,
		model TEXT NOT NULL,
		priority INTEGER DEFAULT 0,
		weight INTEGER DEFAULT 1,
		enabled INTEGER DEFAULT 1,
		max_retries INTEGER DEFAULT 3,
		timeout INTEGER DEFAULT 120,
		supported_models TEXT DEFAULT '',
		proxy_url TEXT DEFAULT '',
		proxy_type TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS channel_keys (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		channel_id INTEGER NOT NULL,
		api_key TEXT NOT NULL,
		status TEXT DEFAULT 'active',
		priority INTEGER DEFAULT 0,
		usage_count INTEGER DEFAULT 0,
		last_used DATETIME,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (channel_id) REFERENCES channels(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS model_routes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		pattern TEXT NOT NULL,
		channel_ids TEXT NOT NULL,
		load_balance TEXT DEFAULT 'round_robin',
		priority INTEGER DEFAULT 0,
		route_prefix TEXT DEFAULT '',
		enabled INTEGER DEFAULT 1
	);

	CREATE TABLE IF NOT EXISTS channel_stats (
		channel_id INTEGER PRIMARY KEY,
		total_requests INTEGER DEFAULT 0,
		success_count INTEGER DEFAULT 0,
		failure_count INTEGER DEFAULT 0,
		avg_latency_ms INTEGER DEFAULT 0,
		is_healthy INTEGER DEFAULT 1,
		last_error TEXT DEFAULT '',
		last_checked DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (channel_id) REFERENCES channels(id)
	);

	CREATE TABLE IF NOT EXISTS request_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		request_id TEXT NOT NULL,
		model TEXT NOT NULL,
		channel_id INTEGER,
		channel_name TEXT,
		status INTEGER,
		latency_ms INTEGER,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_channels_type ON channels(type);
	CREATE INDEX IF NOT EXISTS idx_channels_enabled ON channels(enabled);
	CREATE INDEX IF NOT EXISTS idx_routes_pattern ON model_routes(pattern);
	CREATE INDEX IF NOT EXISTS idx_logs_created ON request_logs(created_at);
	CREATE INDEX IF NOT EXISTS idx_channel_keys_channel ON channel_keys(channel_id);
	CREATE INDEX IF NOT EXISTS idx_channel_keys_status ON channel_keys(status);
	`
	_, err := db.Conn.Exec(schema)
	if err != nil {
		return err
	}
	// Migration: add custom_headers column if not exists
	_, _ = db.Conn.Exec("ALTER TABLE channels ADD COLUMN custom_headers TEXT DEFAULT ''")
	// Migration: add route_prefix column if not exists
	_, _ = db.Conn.Exec("ALTER TABLE model_routes ADD COLUMN route_prefix TEXT DEFAULT ''")
	// Migration: add log enhancement columns
	_, _ = db.Conn.Exec("ALTER TABLE request_logs ADD COLUMN source TEXT DEFAULT ''")
	_, _ = db.Conn.Exec("ALTER TABLE request_logs ADD COLUMN interface TEXT DEFAULT ''")
	_, _ = db.Conn.Exec("ALTER TABLE request_logs ADD COLUMN key_mask TEXT DEFAULT ''")
	// Migration: add key-level metrics columns
	_, _ = db.Conn.Exec("ALTER TABLE channel_keys ADD COLUMN success_count INTEGER DEFAULT 0")
	_, _ = db.Conn.Exec("ALTER TABLE channel_keys ADD COLUMN failure_count INTEGER DEFAULT 0")
	_, _ = db.Conn.Exec("ALTER TABLE channel_keys ADD COLUMN avg_latency_ms INTEGER DEFAULT 0")
	// Migration: add promotion window columns
	_, _ = db.Conn.Exec("ALTER TABLE channels ADD COLUMN promotion_start TEXT DEFAULT ''")
	_, _ = db.Conn.Exec("ALTER TABLE channels ADD COLUMN promotion_end TEXT DEFAULT ''")
	return nil
}

// CreateChannel 创建新渠道
func (db *Database) CreateChannel(ch *models.Channel) (int64, error) {
	result, err := db.Conn.Exec(`
		INSERT INTO channels (name, type, base_url, api_key, model, priority, weight, enabled, max_retries, timeout, supported_models, proxy_url, proxy_type, custom_headers, promotion_start, promotion_end)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, ch.Name, ch.Type, ch.BaseURL, ch.APIKey, ch.Model, ch.Priority, ch.Weight, ch.Enabled, ch.MaxRetries, ch.Timeout, ch.SupportedModels, ch.ProxyURL, ch.ProxyType, ch.CustomHeaders, ch.PromotionStart, ch.PromotionEnd)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// UpdateChannel 更新渠道配置
func (db *Database) UpdateChannel(ch *models.Channel) error {
	_, err := db.Conn.Exec(`
		UPDATE channels SET name=?, type=?, base_url=?, api_key=?, model=?, priority=?, weight=?, enabled=?, max_retries=?, timeout=?, supported_models=?, proxy_url=?, proxy_type=?, custom_headers=?, promotion_start=?, promotion_end=?, updated_at=CURRENT_TIMESTAMP
		WHERE id=?
	`, ch.Name, ch.Type, ch.BaseURL, ch.APIKey, ch.Model, ch.Priority, ch.Weight, ch.Enabled, ch.MaxRetries, ch.Timeout, ch.SupportedModels, ch.ProxyURL, ch.ProxyType, ch.CustomHeaders, ch.PromotionStart, ch.PromotionEnd, ch.ID)
	return err
}

// DeleteChannel 删除渠道
func (db *Database) DeleteChannel(id int64) error {
	_, err := db.Conn.Exec("DELETE FROM channels WHERE id=?", id)
	return err
}

// GetChannel 根据 ID 获取渠道
func (db *Database) GetChannel(id int64) (*models.Channel, error) {
	ch := &models.Channel{}
	err := db.Conn.QueryRow(`
		SELECT id, name, type, base_url, api_key, model, priority, weight, enabled, max_retries, timeout, supported_models, proxy_url, proxy_type, custom_headers, promotion_start, promotion_end, created_at, updated_at
		FROM channels WHERE id=?
	`, id).Scan(&ch.ID, &ch.Name, &ch.Type, &ch.BaseURL, &ch.APIKey, &ch.Model, &ch.Priority, &ch.Weight, &ch.Enabled, &ch.MaxRetries, &ch.Timeout, &ch.SupportedModels, &ch.ProxyURL, &ch.ProxyType, &ch.CustomHeaders, &ch.PromotionStart, &ch.PromotionEnd, &ch.CreatedAt, &ch.UpdatedAt)
	return ch, err
}

// ListChannels 列出所有渠道
func (db *Database) ListChannels() ([]models.Channel, error) {
	rows, err := db.Conn.Query(`
		SELECT id, name, type, base_url, api_key, model, priority, weight, enabled, max_retries, timeout, supported_models, proxy_url, proxy_type, custom_headers, promotion_start, promotion_end, created_at, updated_at
		FROM channels ORDER BY priority DESC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var channels []models.Channel
	for rows.Next() {
		var ch models.Channel
		if err := rows.Scan(&ch.ID, &ch.Name, &ch.Type, &ch.BaseURL, &ch.APIKey, &ch.Model, &ch.Priority, &ch.Weight, &ch.Enabled, &ch.MaxRetries, &ch.Timeout, &ch.SupportedModels, &ch.ProxyURL, &ch.ProxyType, &ch.CustomHeaders, &ch.PromotionStart, &ch.PromotionEnd, &ch.CreatedAt, &ch.UpdatedAt); err != nil {
			return nil, err
		}
		channels = append(channels, ch)
	}
	return channels, nil
}

// ListEnabledChannelsByType 列出指定类型的已启用渠道
func (db *Database) ListEnabledChannelsByType(chType models.ChannelType) ([]models.Channel, error) {
	rows, err := db.Conn.Query(`
		SELECT id, name, type, base_url, api_key, model, priority, weight, enabled, max_retries, timeout, supported_models, proxy_url, proxy_type, custom_headers, promotion_start, promotion_end, created_at, updated_at
		FROM channels WHERE type=? AND enabled=1 ORDER BY priority DESC, id ASC
	`, chType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var channels []models.Channel
	for rows.Next() {
		var ch models.Channel
		if err := rows.Scan(&ch.ID, &ch.Name, &ch.Type, &ch.BaseURL, &ch.APIKey, &ch.Model, &ch.Priority, &ch.Weight, &ch.Enabled, &ch.MaxRetries, &ch.Timeout, &ch.SupportedModels, &ch.ProxyURL, &ch.ProxyType, &ch.CustomHeaders, &ch.PromotionStart, &ch.PromotionEnd, &ch.CreatedAt, &ch.UpdatedAt); err != nil {
			return nil, err
		}
		channels = append(channels, ch)
	}
	return channels, nil
}

// CreateRoute 创建新的模型路由规则
func (db *Database) CreateRoute(route *models.ModelRoute) (int64, error) {
	var ids []string
	for _, id := range route.ChannelIDs {
		ids = append(ids, strconv.FormatInt(id, 10))
	}
	result, err := db.Conn.Exec(`
		INSERT INTO model_routes (pattern, channel_ids, load_balance, priority, route_prefix, enabled)
		VALUES (?, ?, ?, ?, ?, ?)
	`, route.Pattern, strings.Join(ids, ","), route.LoadBalance, route.Priority, route.RoutePrefix, route.Enabled)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// UpdateRoute 更新路由规则
func (db *Database) UpdateRoute(route *models.ModelRoute) error {
	var ids []string
	for _, id := range route.ChannelIDs {
		ids = append(ids, strconv.FormatInt(id, 10))
	}
	_, err := db.Conn.Exec(`
		UPDATE model_routes SET pattern=?, channel_ids=?, load_balance=?, priority=?, route_prefix=?, enabled=?
		WHERE id=?
	`, route.Pattern, strings.Join(ids, ","), route.LoadBalance, route.Priority, route.RoutePrefix, route.Enabled, route.ID)
	return err
}

// ListRoutes 列出所有已启用的路由规则
func (db *Database) ListRoutes() ([]models.ModelRoute, error) {
	rows, err := db.Conn.Query("SELECT id, pattern, channel_ids, load_balance, priority, route_prefix, enabled FROM model_routes ORDER BY priority DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var routes []models.ModelRoute
	for rows.Next() {
		var r models.ModelRoute
		var idsStr string
		if err := rows.Scan(&r.ID, &r.Pattern, &idsStr, &r.LoadBalance, &r.Priority, &r.RoutePrefix, &r.Enabled); err != nil {
			return nil, err
		}
		r.ChannelIDs = parseIDs(idsStr)
		routes = append(routes, r)
	}
	return routes, nil
}

// DeleteRoute 删除路由规则
func (db *Database) DeleteRoute(id int64) error {
	_, err := db.Conn.Exec("DELETE FROM model_routes WHERE id=?", id)
	return err
}

// parseIDs 将逗号分隔的 ID 字符串解析为 int64 切片
func parseIDs(s string) []int64 {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var ids []int64
	for _, p := range parts {
		if id, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

// InitChannelStats 初始化渠道统计数据
func (db *Database) InitChannelStats(channelID int64) error {
	_, err := db.Conn.Exec(`
		INSERT OR IGNORE INTO channel_stats (channel_id) VALUES (?)
	`, channelID)
	return err
}

// UpdateStats 更新渠道统计数据
func (db *Database) UpdateStats(channelID int64, success bool, latency int64, errStr string) error {
	var totalReqs, successCount, failureCount, avgLatency int64
	err := db.Conn.QueryRow("SELECT total_requests, success_count, failure_count, avg_latency_ms FROM channel_stats WHERE channel_id=?", channelID).
		Scan(&totalReqs, &successCount, &failureCount, &avgLatency)
	if err != nil {
		totalReqs = 0
		successCount = 0
		failureCount = 0
		avgLatency = 0
	}

	totalReqs++
	if success {
		successCount++
	} else {
		failureCount++
	}
	avgLatency = (avgLatency*(totalReqs-1) + latency) / totalReqs
	isHealthy := 1
	if failureCount > 10 {
		isHealthy = 0
	}

	_, err = db.Conn.Exec(`
		UPDATE channel_stats SET
			total_requests=?, success_count=?, failure_count=?, avg_latency_ms=?,
			is_healthy=?, last_error=?, last_checked=CURRENT_TIMESTAMP
		WHERE channel_id=?
	`, totalReqs, successCount, failureCount, avgLatency, isHealthy, errStr, channelID)
	return err
}

// LogRequest 记录请求日志
func (db *Database) LogRequest(log *models.RequestLog) error {
	_, err := db.Conn.Exec(`
		INSERT INTO request_logs (request_id, model, channel_id, channel_name, status, latency_ms, source, interface, key_mask)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, log.RequestID, log.Model, log.ChannelID, log.ChannelName, log.Status, log.Latency, log.Source, log.Interface, log.KeyMask)
	return err
}

// ListAllChannelStats 列出所有渠道的统计数据
func (db *Database) ListAllChannelStats() ([]map[string]interface{}, error) {
	rows, err := db.Conn.Query(`
		SELECT cs.channel_id, c.name, c.type, c.enabled,
			cs.total_requests, cs.success_count, cs.failure_count,
			cs.avg_latency_ms, cs.is_healthy, cs.last_error, cs.last_checked
		FROM channel_stats cs
		LEFT JOIN channels c ON cs.channel_id = c.id
		ORDER BY cs.total_requests DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var channelID int64
		var name, chType, lastError sql.NullString
		var enabled, totalReqs, successCount, failureCount, avgLatency, isHealthy int64
		var lastChecked sql.NullString
		if err := rows.Scan(&channelID, &name, &chType, &enabled,
			&totalReqs, &successCount, &failureCount, &avgLatency,
			&isHealthy, &lastError, &lastChecked); err != nil {
			return nil, err
		}
		n := ""
		if name.Valid {
			n = name.String
		}
		t := ""
		if chType.Valid {
			t = chType.String
		}
		e := ""
		if lastError.Valid {
			e = lastError.String
		}
		lc := ""
		if lastChecked.Valid {
			lc = lastChecked.String
		}
		results = append(results, map[string]interface{}{
			"channel_id":     channelID,
			"channel_name":   n,
			"type":           t,
			"enabled":        enabled == 1,
			"total_requests": totalReqs,
			"success_count":  successCount,
			"failure_count":  failureCount,
			"avg_latency_ms": avgLatency,
			"is_healthy":     isHealthy == 1,
			"last_error":     e,
			"last_checked":   lc,
		})
	}
	if results == nil {
		results = []map[string]interface{}{}
	}
	return results, nil
}

// GetRequestLogs 获取请求日志（分页，支持过滤）
func (db *Database) GetRequestLogs(limit, offset int, channelID, status int64, model string) ([]map[string]interface{}, error) {
	q := "SELECT request_id, model, channel_id, channel_name, status, latency_ms, source, interface, key_mask, created_at FROM request_logs WHERE 1=1"
	args := []interface{}{}

	if channelID > 0 {
		q += " AND channel_id=?"
		args = append(args, channelID)
	}
	if status > 0 {
		q += " AND status=?"
		args = append(args, status)
	}
	if model != "" {
		q += " AND model=?"
		args = append(args, model)
	}

	q += " ORDER BY created_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := db.Conn.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var reqID, model, chName, source, iface, keyMask, createdAt string
		var chID, status, latency sql.NullInt64
		if err := rows.Scan(&reqID, &model, &chID, &chName, &status, &latency, &source, &iface, &keyMask, &createdAt); err != nil {
			return nil, err
		}
		results = append(results, map[string]interface{}{
			"request_id":   reqID,
			"model":        model,
			"channel_id":   chID,
			"channel_name": chName,
			"status":       status,
			"latency_ms":   latency,
			"source":       source,
			"interface":    iface,
			"key_mask":     keyMask,
			"created_at":   createdAt,
		})
	}
	if results == nil {
		results = []map[string]interface{}{}
	}
	return results, nil
}

// GetRecentLogs 获取最近的请求日志（按小时分组统计）
func (db *Database) GetRecentLogs(hours int) ([]map[string]interface{}, error) {
	rows, err := db.Conn.Query(`
		SELECT
			strftime('%Y-%m-%d %H:00', created_at) as hour,
			COUNT(*) as total,
			SUM(CASE WHEN status >= 200 AND status < 400 THEN 1 ELSE 0 END) as success,
			SUM(CASE WHEN status >= 400 THEN 1 ELSE 0 END) as failure,
			CAST(AVG(latency_ms) AS INTEGER) as avg_latency
		FROM request_logs
		WHERE created_at >= datetime('now', ?)
		GROUP BY hour ORDER BY hour ASC
	`, "-"+strconv.Itoa(hours)+" hours")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var hour string
		var total, success, failure int64
		var avgLatency sql.NullInt64
		if err := rows.Scan(&hour, &total, &success, &failure, &avgLatency); err != nil {
			return nil, err
		}
		lat := int64(0)
		if avgLatency.Valid {
			lat = avgLatency.Int64
		}
		results = append(results, map[string]interface{}{
			"hour":        hour,
			"total":       total,
			"success":     success,
			"failure":     failure,
			"avg_latency": lat,
		})
	}
	if results == nil {
		results = []map[string]interface{}{}
	}
	return results, nil
}

// ResetChannelStats 重置渠道统计数据
func (db *Database) ResetChannelStats(channelID int64) error {
	_, err := db.Conn.Exec(`
		UPDATE channel_stats SET
			total_requests=0, success_count=0, failure_count=0,
			avg_latency_ms=0, is_healthy=1, last_error='', last_checked=CURRENT_TIMESTAMP
		WHERE channel_id=?
	`, channelID)
	return err
}

// CreateChannelKey 创建渠道 Key
func (db *Database) CreateChannelKey(ck *models.ChannelKey) (int64, error) {
	if ck.Status == "" {
		ck.Status = "active"
	}
	result, err := db.Conn.Exec(`
		INSERT INTO channel_keys (channel_id, api_key, status, priority)
		VALUES (?, ?, ?, ?)
	`, ck.ChannelID, ck.APIKey, ck.Status, ck.Priority)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// ListChannelKeys 列出渠道的所有 Key
func (db *Database) ListChannelKeys(channelID int64) ([]models.ChannelKey, error) {
	rows, err := db.Conn.Query(`
		SELECT id, channel_id, api_key, status, priority, usage_count, success_count, failure_count, avg_latency_ms,
			COALESCE(last_used, '') as last_used, created_at
		FROM channel_keys WHERE channel_id=? ORDER BY priority DESC, id ASC
	`, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []models.ChannelKey
	for rows.Next() {
		var k models.ChannelKey
		if err := rows.Scan(&k.ID, &k.ChannelID, &k.APIKey, &k.Status, &k.Priority, &k.UsageCount, &k.SuccessCount, &k.FailureCount, &k.AvgLatency, &k.LastUsed, &k.CreatedAt); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, nil
}

// GetChannelKey 获取单个 Key
func (db *Database) GetChannelKey(id int64) (*models.ChannelKey, error) {
	k := &models.ChannelKey{}
	err := db.Conn.QueryRow(`
		SELECT id, channel_id, api_key, status, priority, usage_count, success_count, failure_count, avg_latency_ms,
			COALESCE(last_used, '') as last_used, created_at
		FROM channel_keys WHERE id=?
	`, id).Scan(&k.ID, &k.ChannelID, &k.APIKey, &k.Status, &k.Priority, &k.UsageCount, &k.SuccessCount, &k.FailureCount, &k.AvgLatency, &k.LastUsed, &k.CreatedAt)
	return k, err
}

// DeleteChannelKey 删除渠道 Key
func (db *Database) DeleteChannelKey(id int64) error {
	_, err := db.Conn.Exec("DELETE FROM channel_keys WHERE id=?", id)
	return err
}

// UpdateChannelKeyPriority 更新 Key 优先级
func (db *Database) UpdateChannelKeyPriority(id int64, priority int) error {
	_, err := db.Conn.Exec("UPDATE channel_keys SET priority=? WHERE id=?", priority, id)
	return err
}

// UpdateChannelKeyStatus 更新 Key 状态
func (db *Database) UpdateChannelKeyStatus(id int64, status string) error {
	_, err := db.Conn.Exec("UPDATE channel_keys SET status=? WHERE id=?", status, id)
	return err
}

// IncrementKeyUsage 增加 Key 使用计数（成功）
func (db *Database) IncrementKeyUsage(id int64, latencyMs int64) error {
	_, err := db.Conn.Exec(`
		UPDATE channel_keys SET
			usage_count=usage_count+1,
			success_count=success_count+1,
			avg_latency_ms=CASE WHEN (success_count+failure_count)=0 THEN ? ELSE (avg_latency_ms*(success_count+failure_count)+?)/(success_count+failure_count+1) END,
			last_used=CURRENT_TIMESTAMP
		WHERE id=?
	`, latencyMs, latencyMs, id)
	return err
}

// RecordKeyFailure 记录 Key 失败
func (db *Database) RecordKeyFailure(id int64, latencyMs int64) error {
	_, err := db.Conn.Exec(`
		UPDATE channel_keys SET
			failure_count=failure_count+1,
			avg_latency_ms=CASE WHEN (success_count+failure_count)=0 THEN ? ELSE (avg_latency_ms*(success_count+failure_count)+?)/(success_count+failure_count+1) END,
			last_used=CURRENT_TIMESTAMP
		WHERE id=?
	`, latencyMs, latencyMs, id)
	return err
}

// GetActiveKeyForChannel 为渠道获取一个可用的 active key（按优先级排序后取第一个）
func (db *Database) GetActiveKeyForChannel(channelID int64) (*models.ChannelKey, error) {
	k := &models.ChannelKey{}
	err := db.Conn.QueryRow(`
		SELECT id, channel_id, api_key, status, priority, usage_count, success_count, failure_count, avg_latency_ms,
			COALESCE(last_used, '') as last_used, created_at
		FROM channel_keys WHERE channel_id=? AND status='active' ORDER BY priority DESC, id ASC LIMIT 1
	`, channelID).Scan(&k.ID, &k.ChannelID, &k.APIKey, &k.Status, &k.Priority, &k.UsageCount, &k.SuccessCount, &k.FailureCount, &k.AvgLatency, &k.LastUsed, &k.CreatedAt)
	return k, err
}

// Close 关闭数据库连接
func (db *Database) Close() error {
	return db.Conn.Close()
}
