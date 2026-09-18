// Package redisdrv 实现 Redis 驱动。
//
// Redis 的数据模型与关系库差别很大，因此走 dbx.KVConn 而不是 SQLConn。
// 但键列表仍然以 dbx.RowStream 的形式产出，这样前端的虚拟滚动表格、
// 内存预算、磁盘溢出等机制可以原样复用——一个几百万键的库也能平滑浏览。
package redisdrv

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"tacivan/internal/dbx"
	"tacivan/internal/sshtun"
)

func init() {
	dbx.Register(dialer{})
}

type dialer struct{}

func (dialer) Engine() dbx.Engine { return dbx.EngineRedis }

func (dialer) Open(ctx context.Context, cfg dbx.ConnectionConfig) (dbx.Conn, error) {
	c := &Conn{cfg: cfg, clients: map[int]*redis.Client{}}
	if cfg.SSH.Enabled {
		port := cfg.Port
		if port == 0 {
			port = 6379
		}
		t, err := sshtun.Open(cfg.SSH, net.JoinHostPort(cfg.Host, strconv.Itoa(port)))
		if err != nil {
			return nil, err
		}
		c.tunnel = t
	}
	if cfg.TLS.Enabled {
		tc, err := buildTLS(cfg)
		if err != nil {
			c.Close()
			return nil, err
		}
		c.tlsCfg = tc
	}
	if err := c.Ping(ctx); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

// Conn 一条 Redis 连接。每个逻辑库一个客户端。
type Conn struct {
	cfg dbx.ConnectionConfig

	mu      sync.Mutex
	clients map[int]*redis.Client

	tunnel *sshtun.Tunnel
	tlsCfg *tls.Config
	closed bool
}

func (c *Conn) Engine() dbx.Engine { return dbx.EngineRedis }

func (c *Conn) Capability() dbx.Capability {
	return dbx.Capability{KeyValue: true, MultiDatabase: true, ServerVariable: true}
}

// parseDB 把界面上的库名（"0" 或 "db0"）解析成库序号。
func parseDB(database string) int {
	s := strings.TrimSpace(strings.TrimPrefix(strings.ToLower(database), "db"))
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func (c *Conn) client(database string) (*redis.Client, error) {
	db := parseDB(database)
	if database == "" {
		db = parseDB(c.cfg.Database)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, fmt.Errorf("连接已关闭")
	}
	if cl, ok := c.clients[db]; ok {
		return cl, nil
	}

	port := c.cfg.Port
	if port == 0 {
		port = 6379
	}
	timeout := time.Duration(c.cfg.ConnectTimeout) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	opt := &redis.Options{
		Addr:         net.JoinHostPort(c.cfg.Host, strconv.Itoa(port)),
		Username:     c.cfg.User,
		Password:     c.cfg.Password,
		DB:           db,
		DialTimeout:  timeout,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		PoolSize:     4,
		MinIdleConns: 0,
		// 空闲连接自动回收，不长期占着服务端的连接数。
		ConnMaxIdleTime: 5 * time.Minute,
	}
	if c.tlsCfg != nil {
		opt.TLSConfig = c.tlsCfg
	}
	if c.tunnel != nil {
		t := c.tunnel
		opt.Dialer = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return t.DialContext(ctx, network, addr)
		}
	}
	cl := redis.NewClient(opt)
	c.clients[db] = cl
	return cl, nil
}

func (c *Conn) Ping(ctx context.Context) error {
	cl, err := c.client("")
	if err != nil {
		return err
	}
	if err := cl.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("无法连接到 Redis %s: %w", c.cfg.Host, err)
	}
	return nil
}

func (c *Conn) ServerInfo(ctx context.Context) (dbx.ServerInfo, error) {
	cl, err := c.client("")
	if err != nil {
		return dbx.ServerInfo{}, err
	}
	raw, err := cl.Info(ctx, "server", "memory", "clients").Result()
	if err != nil {
		return dbx.ServerInfo{}, err
	}
	kv := parseInfo(raw)
	version := kv["redis_version"]
	return dbx.ServerInfo{
		Engine: dbx.EngineRedis, Version: version,
		VersionNum: parseRedisVersion(version),
		Extra: map[string]string{
			"mode":              kv["redis_mode"],
			"os":                kv["os"],
			"used_memory_human": kv["used_memory_human"],
			"connected_clients": kv["connected_clients"],
			"uptime_in_seconds": kv["uptime_in_seconds"],
		},
	}, nil
}

func parseRedisVersion(v string) int {
	parts := strings.Split(v, ".")
	if len(parts) < 2 {
		return 0
	}
	maj, _ := strconv.Atoi(parts[0])
	min, _ := strconv.Atoi(parts[1])
	patch := 0
	if len(parts) > 2 {
		patch, _ = strconv.Atoi(parts[2])
	}
	return maj*10000 + min*100 + patch
}

func parseInfo(raw string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i := strings.IndexByte(line, ':'); i > 0 {
			out[line[:i]] = line[i+1:]
		}
	}
	return out
}

// Databases 列出各逻辑库及其键数量。
func (c *Conn) Databases(ctx context.Context) ([]dbx.DatabaseInfo, error) {
	cl, err := c.client("")
	if err != nil {
		return nil, err
	}
	count := 16
	if res, err := cl.ConfigGet(ctx, "databases").Result(); err == nil {
		if v, ok := res["databases"]; ok {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				count = n
			}
		}
	}
	keyspace := map[string]string{}
	if raw, err := cl.Info(ctx, "keyspace").Result(); err == nil {
		keyspace = parseInfo(raw)
	}

	current := parseDB(c.cfg.Database)
	out := make([]dbx.DatabaseInfo, 0, count)
	for i := 0; i < count; i++ {
		name := "db" + strconv.Itoa(i)
		info := dbx.DatabaseInfo{Name: name, Current: i == current}
		// keyspace 段形如 db0:keys=12,expires=0,avg_ttl=0
		if v, ok := keyspace[name]; ok {
			for _, part := range strings.Split(v, ",") {
				if strings.HasPrefix(part, "keys=") {
					info.Collation = part[len("keys="):] + " keys"
				}
			}
		}
		out = append(out, info)
	}
	return out, nil
}

// --- 键列表流 ---

// keyStream 用 SCAN 游标分批产出键，并按批用 pipeline 补齐类型/TTL/大小。
//
// 关键是不调用 KEYS *：那条命令会阻塞整个 Redis 实例，
// 在有几百万键的生产实例上执行一次就是一次事故。
type keyStream struct {
	ctx    context.Context
	cl     *redis.Client
	match  string
	cursor uint64
	// batch 当前批次中尚未产出的行。
	batch  []dbx.Row
	done   bool
	closed bool
	cols   []dbx.ColumnMeta
	opts   dbx.ScanOptions
}

func (s *keyStream) Columns() []dbx.ColumnMeta { return s.cols }

func (s *keyStream) Next() (dbx.Row, error) {
	if s.closed {
		return nil, io.EOF
	}
	for len(s.batch) == 0 {
		if s.done {
			return nil, io.EOF
		}
		if err := s.fill(); err != nil {
			return nil, err
		}
	}
	row := s.batch[0]
	s.batch = s.batch[1:]
	return row, nil
}

func (s *keyStream) fill() error {
	keys, next, err := s.cl.Scan(s.ctx, s.cursor, s.match, 300).Result()
	if err != nil {
		return err
	}
	s.cursor = next
	if next == 0 {
		s.done = true
	}
	if len(keys) == 0 {
		return nil
	}

	// 一次往返查完整批键的类型与 TTL，避免每键两次 RTT。
	pipe := s.cl.Pipeline()
	typeCmds := make([]*redis.StatusCmd, len(keys))
	ttlCmds := make([]*redis.DurationCmd, len(keys))
	for i, k := range keys {
		typeCmds[i] = pipe.Type(s.ctx, k)
		ttlCmds[i] = pipe.TTL(s.ctx, k)
	}
	if _, err := pipe.Exec(s.ctx); err != nil && err != redis.Nil {
		return err
	}

	types := make([]string, len(keys))
	for i := range keys {
		types[i] = typeCmds[i].Val()
	}

	// 再一次往返按类型查大小。
	sizePipe := s.cl.Pipeline()
	sizeCmds := make([]*redis.IntCmd, len(keys))
	for i, k := range keys {
		switch types[i] {
		case "string":
			sizeCmds[i] = sizePipe.StrLen(s.ctx, k)
		case "list":
			sizeCmds[i] = sizePipe.LLen(s.ctx, k)
		case "set":
			sizeCmds[i] = sizePipe.SCard(s.ctx, k)
		case "zset":
			sizeCmds[i] = sizePipe.ZCard(s.ctx, k)
		case "hash":
			sizeCmds[i] = sizePipe.HLen(s.ctx, k)
		case "stream":
			sizeCmds[i] = sizePipe.XLen(s.ctx, k)
		}
	}
	if _, err := sizePipe.Exec(s.ctx); err != nil && err != redis.Nil {
		return err
	}

	for i, k := range keys {
		ttl := ttlCmds[i].Val()
		ttlText := "-1"
		if ttl > 0 {
			ttlText = strconv.FormatInt(int64(ttl.Seconds()), 10)
		} else if ttl == -2 {
			// 取 TTL 时键刚好过期了，跳过这条。
			continue
		}
		var size int64
		if sizeCmds[i] != nil {
			size = sizeCmds[i].Val()
		}
		s.batch = append(s.batch, dbx.Row{
			{Text: truncate(k, s.opts.PreviewLimit), Size: len(k)},
			{Text: types[i]},
			{Text: ttlText},
			{Text: strconv.FormatInt(size, 10)},
		})
	}
	return nil
}

func truncate(s string, limit int) string {
	if limit > 0 && len(s) > limit {
		return s[:limit]
	}
	return s
}

func (s *keyStream) Close() error { s.closed = true; return nil }

func (c *Conn) ScanKeys(ctx context.Context, database, pattern string, opts dbx.ScanOptions) (dbx.RowStream, error) {
	cl, err := c.client(database)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(pattern) == "" {
		pattern = "*"
	}
	return &keyStream{
		ctx: ctx, cl: cl, match: pattern, opts: opts.Normalize(),
		cols: []dbx.ColumnMeta{
			{Name: "键", Type: "key", Class: dbx.ClassString},
			{Name: "类型", Type: "type", Class: dbx.ClassString},
			{Name: "TTL(秒)", Type: "ttl", Class: dbx.ClassNumber},
			{Name: "大小", Type: "size", Class: dbx.ClassNumber},
		},
	}, nil
}

// entryPageSize 集合类型一次返回的成员数上限。
const entryPageSize = 500

func (c *Conn) GetKey(ctx context.Context, database, key string) (*dbx.KeyValue, error) {
	cl, err := c.client(database)
	if err != nil {
		return nil, err
	}
	typ, err := cl.Type(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	if typ == "none" {
		return nil, fmt.Errorf("键不存在: %s", key)
	}
	kv := &dbx.KeyValue{Key: key, Type: typ}
	if ttl, err := cl.TTL(ctx, key).Result(); err == nil && ttl > 0 {
		kv.TTL = int64(ttl.Seconds())
	} else {
		kv.TTL = -1
	}
	if enc, err := cl.ObjectEncoding(ctx, key).Result(); err == nil {
		kv.Encoding = enc
	}
	if mem, err := cl.MemoryUsage(ctx, key).Result(); err == nil {
		kv.MemoryUsage = mem
	}

	switch typ {
	case "string":
		v, err := cl.Get(ctx, key).Result()
		if err != nil {
			return nil, err
		}
		kv.Value = v
		kv.Size = int64(len(v))

	case "list":
		n, _ := cl.LLen(ctx, key).Result()
		kv.Size = n
		items, err := cl.LRange(ctx, key, 0, entryPageSize-1).Result()
		if err != nil {
			return nil, err
		}
		for i, v := range items {
			kv.Entries = append(kv.Entries, dbx.KeyEntry{Field: strconv.Itoa(i), Value: v})
		}
		kv.Truncated = n > int64(len(items))

	case "set":
		n, _ := cl.SCard(ctx, key).Result()
		kv.Size = n
		items, _, err := cl.SScan(ctx, key, 0, "*", entryPageSize).Result()
		if err != nil {
			return nil, err
		}
		for _, v := range items {
			kv.Entries = append(kv.Entries, dbx.KeyEntry{Value: v})
		}
		kv.Truncated = n > int64(len(items))

	case "zset":
		n, _ := cl.ZCard(ctx, key).Result()
		kv.Size = n
		items, err := cl.ZRangeWithScores(ctx, key, 0, entryPageSize-1).Result()
		if err != nil {
			return nil, err
		}
		for _, z := range items {
			kv.Entries = append(kv.Entries, dbx.KeyEntry{Value: fmt.Sprint(z.Member), Score: z.Score})
		}
		kv.Truncated = n > int64(len(items))

	case "hash":
		n, _ := cl.HLen(ctx, key).Result()
		kv.Size = n
		// HGETALL 在大 hash 上会阻塞，用 HSCAN 只取一页。
		items, _, err := cl.HScan(ctx, key, 0, "*", entryPageSize*2).Result()
		if err != nil {
			return nil, err
		}
		for i := 0; i+1 < len(items); i += 2 {
			kv.Entries = append(kv.Entries, dbx.KeyEntry{Field: items[i], Value: items[i+1]})
		}
		kv.Truncated = n > int64(len(kv.Entries))

	case "stream":
		n, _ := cl.XLen(ctx, key).Result()
		kv.Size = n
		msgs, err := cl.XRangeN(ctx, key, "-", "+", entryPageSize).Result()
		if err != nil {
			return nil, err
		}
		for _, m := range msgs {
			kv.Entries = append(kv.Entries, dbx.KeyEntry{Field: m.ID, Value: fmt.Sprint(m.Values)})
		}
		kv.Truncated = n > int64(len(msgs))
	}
	return kv, nil
}

func (c *Conn) SetKey(ctx context.Context, database string, kv dbx.KeyValue) error {
	cl, err := c.client(database)
	if err != nil {
		return err
	}
	switch kv.Type {
	case "", "string":
		ttl := time.Duration(0)
		if kv.TTL > 0 {
			ttl = time.Duration(kv.TTL) * time.Second
		}
		return cl.Set(ctx, kv.Key, kv.Value, ttl).Err()
	case "hash":
		if len(kv.Entries) == 0 {
			return fmt.Errorf("哈希键至少需要一个字段")
		}
		pairs := make([]any, 0, len(kv.Entries)*2)
		for _, e := range kv.Entries {
			pairs = append(pairs, e.Field, e.Value)
		}
		return cl.HSet(ctx, kv.Key, pairs...).Err()
	case "list":
		vals := make([]any, 0, len(kv.Entries))
		for _, e := range kv.Entries {
			vals = append(vals, e.Value)
		}
		if len(vals) == 0 {
			return fmt.Errorf("列表键至少需要一个元素")
		}
		return cl.RPush(ctx, kv.Key, vals...).Err()
	case "set":
		vals := make([]any, 0, len(kv.Entries))
		for _, e := range kv.Entries {
			vals = append(vals, e.Value)
		}
		if len(vals) == 0 {
			return fmt.Errorf("集合键至少需要一个成员")
		}
		return cl.SAdd(ctx, kv.Key, vals...).Err()
	case "zset":
		members := make([]redis.Z, 0, len(kv.Entries))
		for _, e := range kv.Entries {
			members = append(members, redis.Z{Score: e.Score, Member: e.Value})
		}
		if len(members) == 0 {
			return fmt.Errorf("有序集合键至少需要一个成员")
		}
		return cl.ZAdd(ctx, kv.Key, members...).Err()
	}
	return fmt.Errorf("暂不支持写入该类型: %s", kv.Type)
}

func (c *Conn) DeleteKeys(ctx context.Context, database string, keys []string) (int64, error) {
	cl, err := c.client(database)
	if err != nil {
		return 0, err
	}
	if len(keys) == 0 {
		return 0, nil
	}
	// 分批删除，避免一条命令里塞进上万个键把服务端卡住。
	var total int64
	const batch = 500
	for i := 0; i < len(keys); i += batch {
		end := i + batch
		if end > len(keys) {
			end = len(keys)
		}
		n, err := cl.Del(ctx, keys[i:end]...).Result()
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func (c *Conn) RenameKey(ctx context.Context, database, from, to string) error {
	cl, err := c.client(database)
	if err != nil {
		return err
	}
	return cl.Rename(ctx, from, to).Err()
}

func (c *Conn) ExpireKey(ctx context.Context, database, key string, ttlSeconds int64) error {
	cl, err := c.client(database)
	if err != nil {
		return err
	}
	if ttlSeconds <= 0 {
		return cl.Persist(ctx, key).Err()
	}
	return cl.Expire(ctx, key, time.Duration(ttlSeconds)*time.Second).Err()
}

// dangerousCommands 会阻塞实例或造成不可逆后果的命令，在内置命令行里拦掉。
var dangerousCommands = map[string]string{
	"flushall": "会清空所有库的全部数据",
	"flushdb":  "会清空当前库的全部数据",
	"keys":     "会阻塞整个实例，请改用左侧的键筛选（内部走 SCAN）",
	"shutdown": "会关闭 Redis 服务",
	"debug":    "可能导致服务不可用",
}

func (c *Conn) Command(ctx context.Context, database string, args []string) (string, error) {
	cl, err := c.client(database)
	if err != nil {
		return "", err
	}
	if len(args) == 0 {
		return "", fmt.Errorf("命令为空")
	}
	name := strings.ToLower(args[0])
	if reason, bad := dangerousCommands[name]; bad {
		return "", fmt.Errorf("已拦截命令 %s：%s。确需执行请使用 redis-cli", strings.ToUpper(name), reason)
	}
	if c.cfg.ReadOnly && !readOnlyCommands[name] {
		return "", fmt.Errorf("当前连接为只读，已拦截写命令 %s", strings.ToUpper(name))
	}
	iargs := make([]any, len(args))
	for i, a := range args {
		iargs[i] = a
	}
	res, err := cl.Do(ctx, iargs...).Result()
	if err != nil {
		if err == redis.Nil {
			return "(nil)", nil
		}
		return "", err
	}
	return formatReply(res, 0), nil
}

var readOnlyCommands = map[string]bool{
	"get": true, "mget": true, "exists": true, "ttl": true, "pttl": true, "type": true,
	"strlen": true, "llen": true, "lrange": true, "scard": true, "smembers": true,
	"sismember": true, "zcard": true, "zrange": true, "zscore": true, "hget": true,
	"hgetall": true, "hlen": true, "hkeys": true, "hvals": true, "scan": true,
	"sscan": true, "hscan": true, "zscan": true, "info": true, "dbsize": true,
	"ping": true, "object": true, "memory": true, "xlen": true, "xrange": true,
	"config": true, "client": true, "command": true, "time": true, "lolwut": true,
}

func formatReply(v any, depth int) string {
	indent := strings.Repeat("  ", depth)
	switch t := v.(type) {
	case nil:
		return "(nil)"
	case string:
		return t
	case int64:
		return "(integer) " + strconv.FormatInt(t, 10)
	case []any:
		if len(t) == 0 {
			return "(empty list or set)"
		}
		var b strings.Builder
		for i, item := range t {
			if i > 0 {
				b.WriteString("\n")
			}
			fmt.Fprintf(&b, "%s%d) %s", indent, i+1, formatReply(item, depth+1))
		}
		return b.String()
	case map[any]any:
		var b strings.Builder
		i := 0
		for k, val := range t {
			if i > 0 {
				b.WriteString("\n")
			}
			fmt.Fprintf(&b, "%s%v: %s", indent, k, formatReply(val, depth+1))
			i++
		}
		return b.String()
	}
	return fmt.Sprint(v)
}

func (c *Conn) ServerStats(ctx context.Context) (map[string]string, error) {
	cl, err := c.client("")
	if err != nil {
		return nil, err
	}
	raw, err := cl.Info(ctx).Result()
	if err != nil {
		return nil, err
	}
	return parseInfo(raw), nil
}

func (c *Conn) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	clients := make([]*redis.Client, 0, len(c.clients))
	for _, cl := range c.clients {
		clients = append(clients, cl)
	}
	c.clients = map[int]*redis.Client{}
	c.mu.Unlock()

	for _, cl := range clients {
		_ = cl.Close()
	}
	if c.tunnel != nil {
		c.tunnel.Close()
		c.tunnel = nil
	}
	return nil
}

func buildTLS(cfg dbx.ConnectionConfig) (*tls.Config, error) {
	tc := &tls.Config{
		ServerName:         cfg.Host,
		InsecureSkipVerify: cfg.TLS.InsecureSkipVerify,
	}
	if cfg.TLS.CAFile != "" {
		pem, err := os.ReadFile(cfg.TLS.CAFile)
		if err != nil {
			return nil, fmt.Errorf("读取 CA 证书失败: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("CA 证书格式无效: %s", cfg.TLS.CAFile)
		}
		tc.RootCAs = pool
	}
	if cfg.TLS.CertFile != "" && cfg.TLS.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.TLS.CertFile, cfg.TLS.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("读取客户端证书失败: %w", err)
		}
		tc.Certificates = []tls.Certificate{cert}
	}
	return tc, nil
}
