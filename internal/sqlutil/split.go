// Package sqlutil 提供不依赖具体引擎的 SQL 文本处理：语句切分与语句分类。
//
// SQL 编辑器要支持「执行全部 / 执行选中 / 执行当前语句」，前提是能正确地把一段文本
// 切成语句。难点全在字面量与注释：分号出现在字符串、反引号标识符、行注释、块注释、
// dollar-quoted 块（PostgreSQL 函数体）里时都不是语句分隔符。
package sqlutil

import (
	"strings"
	"unicode"
)

// Statement 一条切分出来的语句。
type Statement struct {
	// Text 语句文本，已去除首尾空白，不含结尾分号。
	Text string `json:"text"`
	// Offset/End 在原始文本中的字节区间，供编辑器高亮当前语句。
	Offset int `json:"offset"`
	End    int `json:"end"`
	// Line 起始行号，从 1 开始。
	Line int `json:"line"`
}

// Kind 语句类别。
type Kind string

const (
	KindQuery   Kind = "query"   // 产生结果集
	KindDML     Kind = "dml"     // insert/update/delete/replace/merge
	KindDDL     Kind = "ddl"     // create/alter/drop/truncate/rename
	KindTCL     Kind = "tcl"     // begin/commit/rollback/savepoint
	KindUtility Kind = "utility" // use/set/show/explain 等
	KindUnknown Kind = "unknown"
)

// Dialect 切分时需要知道的方言差异。
type Dialect struct {
	// BacktickIdent 反引号作为标识符引用（MySQL）。
	BacktickIdent bool
	// DollarQuote 支持 $tag$...$tag$ 字面量（PostgreSQL）。
	DollarQuote bool
	// HashComment 支持 # 行注释（MySQL）。
	HashComment bool
	// BackslashEscape 字符串中反斜杠转义（MySQL 默认开启）。
	BackslashEscape bool
	// SupportsDelimiter 支持 DELIMITER 指令（MySQL 客户端扩展）。
	SupportsDelimiter bool
}

// MySQLDialect 返回 MySQL/MariaDB 的切分规则。
func MySQLDialect() Dialect {
	return Dialect{BacktickIdent: true, HashComment: true, BackslashEscape: true, SupportsDelimiter: true}
}

// PostgresDialect 返回 PostgreSQL 的切分规则。
func PostgresDialect() Dialect {
	return Dialect{DollarQuote: true}
}

// SQLiteDialect 返回 SQLite 的切分规则。
func SQLiteDialect() Dialect {
	return Dialect{BacktickIdent: true}
}

// Split 把一段 SQL 文本切成语句列表。
func Split(src string, d Dialect) []Statement {
	var out []Statement
	delimiter := ";"
	i := 0
	n := len(src)
	stmtStart := 0
	line := 1
	stmtLine := 1
	// 记录当前语句起始处的行号；跳过前导空白后再确定。
	startPending := true

	flush := func(end int, delimLen int) {
		text := strings.TrimSpace(src[stmtStart:end])
		if text != "" {
			out = append(out, Statement{Text: text, Offset: stmtStart, End: end, Line: stmtLine})
		}
		stmtStart = end + delimLen
		startPending = true
	}

	for i < n {
		c := src[i]

		if startPending {
			if c == '\n' || c == '\r' || c == ' ' || c == '\t' {
				// 前导空白不计入语句
			} else {
				stmtStart = i
				stmtLine = line
				startPending = false
			}
		}

		switch {
		case c == '\n':
			line++
			i++
			continue

		// 行注释
		case c == '-' && i+1 < n && src[i+1] == '-':
			for i < n && src[i] != '\n' {
				i++
			}
			continue
		case d.HashComment && c == '#':
			for i < n && src[i] != '\n' {
				i++
			}
			continue

		// 块注释（不处理嵌套，与主流引擎一致）
		case c == '/' && i+1 < n && src[i+1] == '*':
			i += 2
			for i+1 < n && !(src[i] == '*' && src[i+1] == '/') {
				if src[i] == '\n' {
					line++
				}
				i++
			}
			i += 2
			continue

		// 单引号 / 双引号字符串
		case c == '\'' || c == '"':
			quote := c
			i++
			for i < n {
				if d.BackslashEscape && src[i] == '\\' && i+1 < n {
					i += 2
					continue
				}
				if src[i] == quote {
					// 连续两个引号表示转义后的引号本身
					if i+1 < n && src[i+1] == quote {
						i += 2
						continue
					}
					i++
					break
				}
				if src[i] == '\n' {
					line++
				}
				i++
			}
			continue

		// 反引号标识符
		case d.BacktickIdent && c == '`':
			i++
			for i < n && src[i] != '`' {
				if src[i] == '\n' {
					line++
				}
				i++
			}
			i++
			continue

		// PostgreSQL dollar-quoted 字符串：$$ ... $$ 或 $tag$ ... $tag$
		case d.DollarQuote && c == '$':
			if tag, ok := readDollarTag(src, i); ok {
				end := strings.Index(src[i+len(tag):], tag)
				if end < 0 {
					i = n
					continue
				}
				seg := src[i : i+len(tag)+end+len(tag)]
				line += strings.Count(seg, "\n")
				i += len(tag) + end + len(tag)
				continue
			}
			i++
			continue
		}

		// MySQL 的 DELIMITER 指令：只在语句开头位置生效
		if d.SupportsDelimiter && (c == 'd' || c == 'D') && i == stmtStart && hasPrefixFold(src[i:], "delimiter") {
			j := i + len("delimiter")
			for j < n && (src[j] == ' ' || src[j] == '\t') {
				j++
			}
			k := j
			for k < n && !unicode.IsSpace(rune(src[k])) {
				k++
			}
			if k > j {
				delimiter = src[j:k]
			}
			for k < n && src[k] != '\n' {
				k++
			}
			// DELIMITER 本身不是要发给服务器的语句，直接跳过。
			stmtStart = k
			startPending = true
			i = k
			continue
		}

		// 分隔符
		if strings.HasPrefix(src[i:], delimiter) {
			flush(i, len(delimiter))
			i += len(delimiter)
			continue
		}

		i++
	}

	if stmtStart < n {
		text := strings.TrimSpace(src[stmtStart:])
		if text != "" {
			out = append(out, Statement{Text: text, Offset: stmtStart, End: n, Line: stmtLine})
		}
	}
	return out
}

// StatementAt 返回光标偏移处所在的语句，用于「执行当前语句」。
func StatementAt(src string, offset int, d Dialect) (Statement, bool) {
	stmts := Split(src, d)
	for _, s := range stmts {
		if offset >= s.Offset && offset <= s.End {
			return s, true
		}
	}
	// 光标落在末尾空白处时取最后一条。
	if len(stmts) > 0 && offset > stmts[len(stmts)-1].End {
		return stmts[len(stmts)-1], true
	}
	return Statement{}, false
}

func readDollarTag(src string, i int) (string, bool) {
	if src[i] != '$' {
		return "", false
	}
	j := i + 1
	for j < len(src) {
		c := src[j]
		if c == '$' {
			return src[i : j+1], true
		}
		if !(c == '_' || unicode.IsLetter(rune(c)) || (j > i+1 && unicode.IsDigit(rune(c)))) {
			return "", false
		}
		j++
	}
	return "", false
}

func hasPrefixFold(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	return strings.EqualFold(s[:len(prefix)], prefix)
}

// firstKeyword 取语句的第一个关键字（跳过注释与括号）。
func firstKeyword(stmt string) string {
	s := stripLeadingNoise(stmt)
	i := 0
	for i < len(s) && (unicode.IsLetter(rune(s[i])) || s[i] == '_') {
		i++
	}
	return strings.ToUpper(s[:i])
}

func stripLeadingNoise(s string) string {
	for {
		s = strings.TrimLeftFunc(s, unicode.IsSpace)
		switch {
		case strings.HasPrefix(s, "--"):
			if k := strings.IndexByte(s, '\n'); k >= 0 {
				s = s[k+1:]
				continue
			}
			return ""
		case strings.HasPrefix(s, "#"):
			if k := strings.IndexByte(s, '\n'); k >= 0 {
				s = s[k+1:]
				continue
			}
			return ""
		case strings.HasPrefix(s, "/*"):
			if k := strings.Index(s, "*/"); k >= 0 {
				s = s[k+2:]
				continue
			}
			return ""
		case strings.HasPrefix(s, "("):
			// 形如 (SELECT ...) UNION (SELECT ...)
			s = s[1:]
			continue
		}
		return s
	}
}

var (
	queryKeywords   = map[string]bool{"SELECT": true, "WITH": true, "TABLE": true, "VALUES": true, "DESC": true, "DESCRIBE": true, "EXPLAIN": true, "SHOW": true, "PRAGMA": true}
	dmlKeywords     = map[string]bool{"INSERT": true, "UPDATE": true, "DELETE": true, "REPLACE": true, "MERGE": true, "UPSERT": true, "LOAD": true, "COPY": true}
	ddlKeywords     = map[string]bool{"CREATE": true, "ALTER": true, "DROP": true, "TRUNCATE": true, "RENAME": true, "COMMENT": true, "VACUUM": true, "ANALYZE": true, "OPTIMIZE": true, "REINDEX": true}
	tclKeywords     = map[string]bool{"BEGIN": true, "START": true, "COMMIT": true, "ROLLBACK": true, "SAVEPOINT": true, "RELEASE": true, "LOCK": true, "UNLOCK": true}
	utilityKeywords = map[string]bool{"USE": true, "SET": true, "CALL": true, "GRANT": true, "REVOKE": true, "FLUSH": true, "KILL": true, "RESET": true, "CHECKPOINT": true, "LISTEN": true, "NOTIFY": true}
)

// Classify 判断语句类别。
func Classify(stmt string) Kind {
	kw := firstKeyword(stmt)
	switch {
	case queryKeywords[kw]:
		// WITH ... 可能以 INSERT/UPDATE/DELETE 收尾（PostgreSQL 的可写 CTE）。
		if kw == "WITH" && withIsWriting(stmt) {
			return KindDML
		}
		return KindQuery
	case dmlKeywords[kw]:
		return KindDML
	case ddlKeywords[kw]:
		return KindDDL
	case tclKeywords[kw]:
		return KindTCL
	case utilityKeywords[kw]:
		return KindUtility
	}
	if kw == "" {
		return KindUnknown
	}
	return KindUnknown
}

// withIsWriting 检查 WITH 语句是否以写操作收尾。
func withIsWriting(stmt string) bool {
	up := strings.ToUpper(stmt)
	// 取最后一个顶层关键字的粗略判断：找最后出现的 INSERT/UPDATE/DELETE 是否在最后一个 ) 之后。
	lastParen := strings.LastIndex(up, ")")
	tail := up
	if lastParen >= 0 {
		tail = up[lastParen:]
	}
	for _, kw := range []string{"INSERT", "UPDATE", "DELETE", "MERGE"} {
		if strings.Contains(tail, kw) {
			return true
		}
	}
	return false
}

// IsReadOnly 判断语句是否为只读。只读连接用它拦截一切写操作。
//
// 判定刻意保守：拿不准的一律视为写操作，宁可误拦也不让写语句溜进生产库。
func IsReadOnly(stmt string) bool {
	switch Classify(stmt) {
	case KindQuery:
		// SELECT ... INTO OUTFILE / FOR UPDATE 会写，排除掉。
		up := strings.ToUpper(stmt)
		if strings.Contains(up, " INTO OUTFILE") || strings.Contains(up, " INTO DUMPFILE") {
			return false
		}
		if strings.Contains(up, " FOR UPDATE") {
			return false
		}
		kw := firstKeyword(stmt)
		// EXPLAIN ANALYZE 在 PostgreSQL 下会真的执行语句。
		if kw == "EXPLAIN" && strings.Contains(up, "ANALYZE") {
			return false
		}
		return true
	case KindUtility:
		kw := firstKeyword(stmt)
		return kw == "USE" || kw == "SHOW" || kw == "SET"
	default:
		return false
	}
}

// StripComments 去掉注释，用于提取语句主体做启发式分析。
func StripComments(src string, d Dialect) string {
	var b strings.Builder
	b.Grow(len(src))
	i, n := 0, len(src)
	for i < n {
		c := src[i]
		switch {
		case c == '-' && i+1 < n && src[i+1] == '-':
			for i < n && src[i] != '\n' {
				i++
			}
		case d.HashComment && c == '#':
			for i < n && src[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < n && src[i+1] == '*':
			i += 2
			for i+1 < n && !(src[i] == '*' && src[i+1] == '/') {
				i++
			}
			i += 2
		case c == '\'' || c == '"' || (d.BacktickIdent && c == '`'):
			quote := c
			b.WriteByte(c)
			i++
			for i < n {
				if d.BackslashEscape && quote != '`' && src[i] == '\\' && i+1 < n {
					b.WriteString(src[i : i+2])
					i += 2
					continue
				}
				b.WriteByte(src[i])
				if src[i] == quote {
					i++
					break
				}
				i++
			}
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}
