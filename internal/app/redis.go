package app

import (
	"context"
	"fmt"
	"strings"

	"tacivan/internal/dbx"
	"tacivan/internal/rs"
)

// RedisScanResult 键浏览器的一页结果。
type RedisScanResult struct {
	ResultID string   `json:"resultId"`
	Page     *rs.Page `json:"page"`
	Pattern  string   `json:"pattern"`
}

// RedisScanKeys 按模式浏览键。
//
// 走的是 SCAN 游标而不是 KEYS：后者会阻塞整个实例，
// 在有几百万键的线上库执行一次就是一次事故。
func (a *App) RedisScanKeys(connID, database, pattern string, firstPageRows int) (*RedisScanResult, error) {
	_, kv, err := a.kvSession(connID)
	if err != nil {
		return nil, err
	}
	if firstPageRows <= 0 {
		firstPageRows = a.store.Settings().GridPageSize
	}
	ctx, cancel := a.requestCtx(120)
	defer cancel()

	stream, err := longLivedQuery(ctx, func(qctx context.Context) (dbx.RowStream, error) {
		return kv.ScanKeys(qctx, database, pattern, a.scanOptions())
	})
	if err != nil {
		return nil, err
	}
	cursor := a.rs.Open(stream, rs.CursorMeta{
		Query: "SCAN MATCH " + pattern, ConnectionID: connID, Database: database,
	})
	page, err := cursor.Fetch(0, int64(firstPageRows))
	if err != nil {
		_ = a.rs.Close(cursor.ID())
		return nil, err
	}
	return &RedisScanResult{ResultID: cursor.ID(), Page: page, Pattern: pattern}, nil
}

// RedisGetKey 读取一个键的完整内容。
func (a *App) RedisGetKey(connID, database, key string) (*dbx.KeyValue, error) {
	_, kv, err := a.kvSession(connID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := a.requestCtx(30)
	defer cancel()
	return kv.GetKey(ctx, database, key)
}

// RedisSetKey 写入一个键。
func (a *App) RedisSetKey(connID, database string, value dbx.KeyValue) error {
	s, kv, err := a.kvSession(connID)
	if err != nil {
		return err
	}
	if s.cfg.ReadOnly {
		return fmt.Errorf("当前连接为只读，已拦截写操作")
	}
	if strings.TrimSpace(value.Key) == "" {
		return fmt.Errorf("键名不能为空")
	}
	ctx, cancel := a.requestCtx(30)
	defer cancel()
	return kv.SetKey(ctx, database, value)
}

// RedisDeleteKeys 删除一批键。
func (a *App) RedisDeleteKeys(connID, database string, keys []string) (int64, error) {
	s, kv, err := a.kvSession(connID)
	if err != nil {
		return 0, err
	}
	if s.cfg.ReadOnly {
		return 0, fmt.Errorf("当前连接为只读，已拦截删除操作")
	}
	ctx, cancel := a.requestCtx(120)
	defer cancel()
	return kv.DeleteKeys(ctx, database, keys)
}

// RedisRenameKey 重命名一个键。
func (a *App) RedisRenameKey(connID, database, from, to string) error {
	s, kv, err := a.kvSession(connID)
	if err != nil {
		return err
	}
	if s.cfg.ReadOnly {
		return fmt.Errorf("当前连接为只读，已拦截重命名操作")
	}
	ctx, cancel := a.requestCtx(30)
	defer cancel()
	return kv.RenameKey(ctx, database, from, to)
}

// RedisExpireKey 设置或清除键的过期时间，ttlSeconds <= 0 表示永不过期。
func (a *App) RedisExpireKey(connID, database, key string, ttlSeconds int64) error {
	s, kv, err := a.kvSession(connID)
	if err != nil {
		return err
	}
	if s.cfg.ReadOnly {
		return fmt.Errorf("当前连接为只读，已拦截该操作")
	}
	ctx, cancel := a.requestCtx(30)
	defer cancel()
	return kv.ExpireKey(ctx, database, key, ttlSeconds)
}

// RedisCommand 在内置命令行里执行一条命令。
func (a *App) RedisCommand(connID, database, line string) (string, error) {
	_, kv, err := a.kvSession(connID)
	if err != nil {
		return "", err
	}
	args := splitCommandLine(line)
	if len(args) == 0 {
		return "", fmt.Errorf("命令为空")
	}
	ctx, cancel := a.requestCtx(60)
	defer cancel()
	return kv.Command(ctx, database, args)
}

// splitCommandLine 按空格切分命令行，支持引号包裹带空格的参数。
func splitCommandLine(line string) []string {
	var out []string
	var cur strings.Builder
	var quote byte
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
				// 引号闭合即成词，支持传空字符串参数。
				out = append(out, cur.String())
				cur.Reset()
			} else if c == '\\' && i+1 < len(line) {
				i++
				cur.WriteByte(line[i])
			} else {
				cur.WriteByte(c)
			}
		case c == '\'' || c == '"':
			flush()
			quote = c
		case c == ' ' || c == '\t':
			flush()
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return out
}
