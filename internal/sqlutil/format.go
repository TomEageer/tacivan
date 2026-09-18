package sqlutil

import (
	"strings"
	"unicode"
)

// 顶层子句关键字，各自另起一行。
var clauseKeywords = map[string]bool{
	"SELECT": true, "FROM": true, "WHERE": true, "GROUP": true, "HAVING": true,
	"ORDER": true, "LIMIT": true, "OFFSET": true, "UNION": true, "INSERT": true,
	"UPDATE": true, "DELETE": true, "SET": true, "VALUES": true, "RETURNING": true,
	"WITH": true, "FETCH": true, "INTERSECT": true, "EXCEPT": true,
}

// join 系关键字，缩进一级后另起一行。
var joinKeywords = map[string]bool{
	"JOIN": true, "INNER": true, "LEFT": true, "RIGHT": true, "FULL": true,
	"CROSS": true, "STRAIGHT_JOIN": true,
}

// 出现在 WHERE/ON 里的连接词，换行但缩进。
var boolKeywords = map[string]bool{"AND": true, "OR": true}

// token 词法单元。
type token struct {
	text string
	// kind: word | string | ident | comment | punct | number
	kind string
}

// tokenize 把 SQL 切成词法单元，字符串与注释整体保留。
func tokenize(src string, d Dialect) []token {
	var out []token
	i, n := 0, len(src)
	for i < n {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++

		case c == '-' && i+1 < n && src[i+1] == '-',
			d.HashComment && c == '#':
			start := i
			for i < n && src[i] != '\n' {
				i++
			}
			out = append(out, token{src[start:i], "comment"})

		case c == '/' && i+1 < n && src[i+1] == '*':
			start := i
			i += 2
			for i+1 < n && !(src[i] == '*' && src[i+1] == '/') {
				i++
			}
			i = min(i+2, n)
			out = append(out, token{src[start:i], "comment"})

		case c == '\'' || c == '"':
			quote := c
			start := i
			i++
			for i < n {
				if d.BackslashEscape && src[i] == '\\' && i+1 < n {
					i += 2
					continue
				}
				if src[i] == quote {
					if i+1 < n && src[i+1] == quote {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			kind := "string"
			if quote == '"' && !d.BackslashEscape {
				kind = "ident"
			}
			out = append(out, token{src[start:i], kind})

		case d.BacktickIdent && c == '`':
			start := i
			i++
			for i < n && src[i] != '`' {
				i++
			}
			i = min(i+1, n)
			out = append(out, token{src[start:i], "ident"})

		case unicode.IsLetter(rune(c)) || c == '_' || c == '@' || c == '$':
			start := i
			for i < n && (unicode.IsLetter(rune(src[i])) || unicode.IsDigit(rune(src[i])) ||
				src[i] == '_' || src[i] == '@' || src[i] == '$' || src[i] == '.') {
				i++
			}
			out = append(out, token{src[start:i], "word"})

		case unicode.IsDigit(rune(c)):
			start := i
			for i < n && (unicode.IsDigit(rune(src[i])) || src[i] == '.' || src[i] == 'e' || src[i] == 'E') {
				i++
			}
			out = append(out, token{src[start:i], "number"})

		default:
			out = append(out, token{string(c), "punct"})
			i++
		}
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Format 对一段 SQL 做基础格式化。
//
// 目标不是做一个完整的 SQL 解析器，而是让顺手粘进来的一行长语句变得能读：
// 子句换行、JOIN 与布尔连接词对齐、括号内缩进。语义完全不变，
// 字符串与注释原样保留。
func Format(script string, d Dialect) string {
	stmts := Split(script, d)
	if len(stmts) == 0 {
		return script
	}
	parts := make([]string, 0, len(stmts))
	for _, s := range stmts {
		parts = append(parts, formatStatement(s.Text, d))
	}
	return strings.Join(parts, ";\n\n") + ";"
}

func formatStatement(stmt string, d Dialect) string {
	tokens := tokenize(stmt, d)
	if len(tokens) == 0 {
		return stmt
	}

	var b strings.Builder
	depth := 0
	lineEmpty := true
	// selectDepth 记录 SELECT 列表所在的括号层级，用于决定逗号是否换行。
	selectDepth := -1

	writeNewline := func(extraIndent int) {
		if !lineEmpty {
			b.WriteString("\n")
		}
		b.WriteString(strings.Repeat("  ", depth+extraIndent))
		lineEmpty = true
	}
	writeToken := func(s string, spaceBefore bool) {
		if spaceBefore && !lineEmpty {
			b.WriteString(" ")
		}
		b.WriteString(s)
		lineEmpty = false
	}

	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		up := strings.ToUpper(t.text)

		switch {
		case t.kind == "comment":
			writeNewline(0)
			writeToken(t.text, false)
			writeNewline(0)
			continue

		case t.kind == "punct" && t.text == "(":
			writeToken("(", !isFunctionCall(tokens, i))
			depth++
			continue

		case t.kind == "punct" && t.text == ")":
			depth--
			if depth < 0 {
				depth = 0
			}
			writeToken(")", false)
			continue

		case t.kind == "punct" && t.text == ",":
			writeToken(",", false)
			// 只有 SELECT 列表这一层的逗号才换行，函数参数保持在一行。
			if depth == selectDepth {
				writeNewline(1)
			}
			continue

		case t.kind == "word" && clauseKeywords[up]:
			// GROUP / ORDER 后面跟 BY，一起输出。
			writeNewline(0)
			if up == "SELECT" {
				selectDepth = depth
			}
			if (up == "GROUP" || up == "ORDER") && i+1 < len(tokens) &&
				strings.EqualFold(tokens[i+1].text, "BY") {
				writeToken(up+" BY", false)
				i++
			} else {
				writeToken(up, false)
			}
			if up == "SELECT" || up == "SET" {
				writeNewline(1)
			}
			continue

		case t.kind == "word" && joinKeywords[up]:
			writeNewline(0)
			writeToken(up, false)
			continue

		case t.kind == "word" && boolKeywords[up]:
			writeNewline(1)
			writeToken(up, false)
			continue

		case t.kind == "word" && isReservedWord(up):
			writeToken(up, true)
			continue
		}

		// 紧跟在左括号或点号后面的词不加空格。
		spaceBefore := true
		if i > 0 {
			prev := tokens[i-1]
			if prev.kind == "punct" && (prev.text == "(" || prev.text == ".") {
				spaceBefore = false
			}
		}
		if t.kind == "punct" && (t.text == "." || t.text == ";") {
			spaceBefore = false
		}
		writeToken(t.text, spaceBefore)
	}

	out := b.String()
	// 清掉尾随空白与多余空行。
	lines := strings.Split(out, "\n")
	kept := lines[:0]
	for _, l := range lines {
		l = strings.TrimRight(l, " \t")
		if strings.TrimSpace(l) == "" {
			continue
		}
		kept = append(kept, l)
	}
	return strings.TrimSuffix(strings.Join(kept, "\n"), ";")
}

// isFunctionCall 判断左括号是否紧跟在函数名后面。
func isFunctionCall(tokens []token, i int) bool {
	if i == 0 {
		return false
	}
	prev := tokens[i-1]
	return prev.kind == "word" && !clauseKeywords[strings.ToUpper(prev.text)]
}

var reservedWords = map[string]bool{
	"AS": true, "ON": true, "IN": true, "IS": true, "NOT": true, "NULL": true,
	"LIKE": true, "BETWEEN": true, "EXISTS": true, "CASE": true, "WHEN": true,
	"THEN": true, "ELSE": true, "END": true, "ASC": true, "DESC": true,
	"DISTINCT": true, "ALL": true, "BY": true, "INTO": true, "TABLE": true,
	"CREATE": true, "ALTER": true, "DROP": true, "PRIMARY": true, "KEY": true,
	"FOREIGN": true, "REFERENCES": true, "DEFAULT": true, "UNIQUE": true,
	"INDEX": true, "CONSTRAINT": true, "TRUE": true, "FALSE": true, "USING": true,
	"OUTER": true, "NATURAL": true, "CAST": true, "IF": true,
}

func isReservedWord(up string) bool { return reservedWords[up] }
