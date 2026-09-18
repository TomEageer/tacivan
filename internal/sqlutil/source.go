package sqlutil

import (
	"regexp"
	"strings"
)

// identPart 匹配一个标识符片段：裸标识符，或被反引号/双引号/方括号包裹的片段
// （引号内允许空格、点号等任意字符）。
const identPart = "(?:`[^`]*`|\"[^\"]*\"|\\[[^\\]]*\\]|[\\w$]+)"

// qualifiedName 形如 db.table 的限定名。
const qualifiedName = identPart + "(?:\\." + identPart + ")*"

// singleTableRe 匹配「一张表的简单 SELECT」。
//
// 刻意收得很紧：只认 SELECT <列> FROM <表> 后面接 WHERE/ORDER BY/LIMIT 的形式。
// 一旦出现 JOIN、子查询、GROUP BY、UNION、DISTINCT、聚合函数，就不再认为
// 结果集能安全地映射回单表的行——认错了会把用户的修改写到错误的地方。
// 逗号分隔的多表写法也因为尾部锚定而自然落选。
var singleTableRe = regexp.MustCompile(
	`(?is)^\s*SELECT\s+(?:ALL\s+)?(.+?)\s+FROM\s+(` + qualifiedName + `)\s*(?:(?:AS\s+)?([a-z_]\w*)\s*)?(WHERE|ORDER\s+BY|LIMIT|OFFSET|FETCH|FOR\b|$)`)

// 出现这些片段就一律视为不可就地编辑。
var notSingleTableRe = regexp.MustCompile(`(?is)\b(JOIN|UNION|INTERSECT|EXCEPT|GROUP\s+BY|HAVING|DISTINCT|OVER\s*\(|INTO\b)\b`)

var aggregateRe = regexp.MustCompile(`(?is)\b(COUNT|SUM|AVG|MIN|MAX|GROUP_CONCAT|STRING_AGG|ARRAY_AGG)\s*\(`)

// TableSource 从简单 SELECT 中识别出唯一的来源表。
//
// 返回的表名可能带库名前缀与引号，由调用方按方言解析。
func TableSource(stmt string, d Dialect) (table string, alias string, ok bool) {
	s := strings.TrimSpace(StripComments(stmt, d))
	if s == "" {
		return "", "", false
	}
	if Classify(s) != KindQuery {
		return "", "", false
	}
	if notSingleTableRe.MatchString(s) {
		return "", "", false
	}
	m := singleTableRe.FindStringSubmatch(s)
	if m == nil {
		return "", "", false
	}
	// 选择列里有聚合函数时，结果行与表行不是一一对应的。
	if aggregateRe.MatchString(m[1]) {
		return "", "", false
	}
	// 逗号分隔的多表（老式隐式 join）同样不可编辑。
	if strings.Contains(m[2], ",") {
		return "", "", false
	}
	alias = m[3]
	if isKeyword(alias) {
		alias = ""
	}
	return m[2], alias, true
}

func isKeyword(s string) bool {
	switch strings.ToUpper(s) {
	case "WHERE", "ORDER", "GROUP", "LIMIT", "OFFSET", "HAVING", "FOR", "UNION", "FETCH", "":
		return true
	}
	return false
}

// UnquoteIdent 去掉标识符两侧的引号，并还原双写转义。
func UnquoteIdent(s string) string {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return s
	}
	switch s[0] {
	case '`':
		if s[len(s)-1] == '`' {
			return strings.ReplaceAll(s[1:len(s)-1], "``", "`")
		}
	case '"':
		if s[len(s)-1] == '"' {
			return strings.ReplaceAll(s[1:len(s)-1], `""`, `"`)
		}
	case '[':
		if s[len(s)-1] == ']' {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// SplitQualifiedName 把 db.table 形式拆开，正确处理带引号的片段。
func SplitQualifiedName(s string) (qualifier, name string) {
	depth := 0
	var quote byte
	last := -1
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '`', '"':
			quote = c
		case '[':
			depth++
		case ']':
			depth--
		case '.':
			if depth == 0 {
				last = i
			}
		}
	}
	if last < 0 {
		return "", UnquoteIdent(s)
	}
	return UnquoteIdent(s[:last]), UnquoteIdent(s[last+1:])
}
