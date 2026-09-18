package app

import (
	"fmt"
	"strconv"
	"strings"

	"tacivan/internal/dbx"
)

// FilterOp 筛选运算符。
type FilterOp string

const (
	OpEqual        FilterOp = "eq"
	OpNotEqual     FilterOp = "ne"
	OpGreater      FilterOp = "gt"
	OpGreaterEqual FilterOp = "gte"
	OpLess         FilterOp = "lt"
	OpLessEqual    FilterOp = "lte"
	OpContains     FilterOp = "contains"
	OpNotContains  FilterOp = "notContains"
	OpStartsWith   FilterOp = "startsWith"
	OpEndsWith     FilterOp = "endsWith"
	OpIn           FilterOp = "in"
	OpNotIn        FilterOp = "notIn"
	OpIsNull       FilterOp = "isNull"
	OpIsNotNull    FilterOp = "isNotNull"
	OpBetween      FilterOp = "between"
	OpIsEmpty      FilterOp = "isEmpty"
)

// FilterCondition 一条筛选条件。
type FilterCondition struct {
	Column string   `json:"column"`
	Op     FilterOp `json:"op"`
	// Values 运算符所需的值：多数运算符一个值，between 两个，in 任意多个。
	Values []string `json:"values"`
	// Enabled 为假时该条件不参与。
	Enabled bool `json:"enabled"`
}

// FilterGroup 一组筛选条件。
type FilterGroup struct {
	Conditions []FilterCondition `json:"conditions"`
	// Conjunction: and | or
	Conjunction string `json:"conjunction"`
}

// FilterOpInfo 描述一个运算符，供前端渲染下拉与决定要几个输入框。
type FilterOpInfo struct {
	Op    FilterOp `json:"op"`
	Label string   `json:"label"`
	// Arity 需要的值个数：0 无需输入，1 单值，2 区间，-1 任意多个。
	Arity int `json:"arity"`
	// Classes 适用的列类别，为空表示全部适用。
	Classes []string `json:"classes,omitempty"`
}

// ListFilterOps 返回筛选器可用的运算符。
func (a *App) ListFilterOps() []FilterOpInfo {
	return []FilterOpInfo{
		{Op: OpEqual, Label: "等于", Arity: 1},
		{Op: OpNotEqual, Label: "不等于", Arity: 1},
		{Op: OpGreater, Label: "大于", Arity: 1},
		{Op: OpGreaterEqual, Label: "大于等于", Arity: 1},
		{Op: OpLess, Label: "小于", Arity: 1},
		{Op: OpLessEqual, Label: "小于等于", Arity: 1},
		{Op: OpContains, Label: "包含", Arity: 1, Classes: []string{"string", "json", "unknown"}},
		{Op: OpNotContains, Label: "不包含", Arity: 1, Classes: []string{"string", "json", "unknown"}},
		{Op: OpStartsWith, Label: "开头是", Arity: 1, Classes: []string{"string", "json", "unknown"}},
		{Op: OpEndsWith, Label: "结尾是", Arity: 1, Classes: []string{"string", "json", "unknown"}},
		{Op: OpBetween, Label: "介于", Arity: 2},
		{Op: OpIn, Label: "属于", Arity: -1},
		{Op: OpNotIn, Label: "不属于", Arity: -1},
		{Op: OpIsNull, Label: "为空(NULL)", Arity: 0},
		{Op: OpIsNotNull, Label: "不为空(NULL)", Arity: 0},
		{Op: OpIsEmpty, Label: "为空字符串", Arity: 0, Classes: []string{"string", "json", "unknown"}},
	}
}

// BuildFilterSQL 把结构化条件翻译成 WHERE 子句。
//
// 值的转义交给方言完成，前端只传字段名、运算符和原始值——
// 让界面去拼 SQL 字符串，等于把注入面直接暴露在最容易出错的地方。
func (a *App) BuildFilterSQL(connID string, group FilterGroup) (string, error) {
	_, sc, err := a.sqlSession(connID)
	if err != nil {
		return "", err
	}
	return buildFilterWhere(sc.Dialect(), group)
}

func buildFilterWhere(d dbx.Dialect, group FilterGroup) (string, error) {
	conj := " AND "
	if strings.EqualFold(group.Conjunction, "or") {
		conj = " OR "
	}

	var parts []string
	for i, c := range group.Conditions {
		if !c.Enabled || strings.TrimSpace(c.Column) == "" {
			continue
		}
		frag, err := filterFragment(d, c)
		if err != nil {
			return "", fmt.Errorf("第 %d 个条件无效：%w", i+1, err)
		}
		if frag != "" {
			parts = append(parts, frag)
		}
	}
	if len(parts) == 0 {
		return "", nil
	}
	if len(parts) == 1 {
		return parts[0], nil
	}
	// 多个条件各自加括号，避免 AND/OR 混用时优先级出意外。
	for i := range parts {
		parts[i] = "(" + parts[i] + ")"
	}
	return strings.Join(parts, conj), nil
}

func filterFragment(d dbx.Dialect, c FilterCondition) (string, error) {
	col := d.QuoteIdent(c.Column)
	val := func(i int) string {
		if i < len(c.Values) {
			return c.Values[i]
		}
		return ""
	}

	switch c.Op {
	case OpIsNull:
		return col + " IS NULL", nil
	case OpIsNotNull:
		return col + " IS NOT NULL", nil
	case OpIsEmpty:
		return col + " = " + d.QuoteString(""), nil

	case OpEqual, OpNotEqual, OpGreater, OpGreaterEqual, OpLess, OpLessEqual:
		if len(c.Values) == 0 {
			return "", fmt.Errorf("缺少比较值")
		}
		ops := map[FilterOp]string{
			OpEqual: "=", OpNotEqual: "<>", OpGreater: ">",
			OpGreaterEqual: ">=", OpLess: "<", OpLessEqual: "<=",
		}
		return col + " " + ops[c.Op] + " " + literalOf(d, val(0)), nil

	case OpContains, OpNotContains, OpStartsWith, OpEndsWith:
		if len(c.Values) == 0 {
			return "", fmt.Errorf("缺少匹配内容")
		}
		// 用户输入里的 % 和 _ 是普通字符，不该被当成通配符。
		esc := escapeLike(val(0))
		var pattern string
		switch c.Op {
		case OpStartsWith:
			pattern = esc + "%"
		case OpEndsWith:
			pattern = "%" + esc
		default:
			pattern = "%" + esc + "%"
		}
		op := "LIKE"
		if c.Op == OpNotContains {
			op = "NOT LIKE"
		}
		return col + " " + op + " " + d.QuoteString(pattern) + " ESCAPE " + d.QuoteString("\\"), nil

	case OpBetween:
		if len(c.Values) < 2 {
			return "", fmt.Errorf("区间需要两个值")
		}
		return col + " BETWEEN " + literalOf(d, val(0)) + " AND " + literalOf(d, val(1)), nil

	case OpIn, OpNotIn:
		items := splitInValues(c.Values)
		if len(items) == 0 {
			return "", fmt.Errorf("缺少候选值")
		}
		quoted := make([]string, len(items))
		for i, v := range items {
			quoted[i] = literalOf(d, v)
		}
		op := "IN"
		if c.Op == OpNotIn {
			op = "NOT IN"
		}
		return col + " " + op + " (" + strings.Join(quoted, ", ") + ")", nil
	}
	return "", fmt.Errorf("不支持的运算符: %s", c.Op)
}

// literalOf 生成字面量。纯数字按数字写，其余一律按字符串引用。
func literalOf(d dbx.Dialect, v string) string {
	t := strings.TrimSpace(v)
	if t == "" {
		return d.QuoteString(v)
	}
	if _, err := strconv.ParseFloat(t, 64); err == nil {
		return t
	}
	return d.QuoteString(v)
}

// escapeLike 转义 LIKE 模式里的特殊字符。
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// splitInValues 允许用户在一个输入框里用逗号或换行分隔多个值。
func splitInValues(values []string) []string {
	var out []string
	for _, v := range values {
		for _, part := range strings.FieldsFunc(v, func(r rune) bool {
			return r == ',' || r == '\n' || r == '\r'
		}) {
			if t := strings.TrimSpace(part); t != "" {
				out = append(out, t)
			}
		}
	}
	return out
}
