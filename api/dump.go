package api

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/maxi-format/maxi-go/core"
	"github.com/maxi-format/maxi-go/internal"
)

// DumpOptions controls the output of DumpMaxi.
type DumpOptions struct {
	// Multiline formats type definitions and records with one field per line.
	Multiline bool
	// IncludeTypes emits type definitions in the schema section (default true).
	IncludeTypes bool
	// Version emits an @version directive if non-empty and != "1.0.0".
	Version string
	// SchemaFile emits an @schema directive for an external schema file path/URL.
	SchemaFile string
	// Types is an optional slice of type definitions to include in the schema section.
	// Each element may be a *core.MaxiTypeDef or a map[string]any with the same keys.
	Types []*core.MaxiTypeDef
	// DefaultAlias is required when dumping a plain []map or single map without an alias key.
	DefaultAlias string
	// CollectReferences controls whether nested objects with an id field are
	// hoisted to their own top-level records and replaced by their ID value.
	// Defaults to true.
	CollectReferences bool

}

// DefaultDumpOptions returns a DumpOptions with spec-default values.
func DefaultDumpOptions() DumpOptions {
	return DumpOptions{
		Multiline:         false,
		IncludeTypes:      true,
		CollectReferences: true,
	}
}

// DumpMaxi serialises data into a MAXI string.
//
// data may be one of:
//   - *core.MaxiParseResult  – round-trip a full parse result
//   - []map[string]any       – slice of objects (requires opts.DefaultAlias)
//   - map[string]any         – single object   (requires opts.DefaultAlias)
//   - map[string][]map[string]any – alias → rows map
//
// opts is optional; pass DefaultDumpOptions() or a zero-value struct.
func DumpMaxi(data any, opts ...DumpOptions) (string, error) {
	o := DefaultDumpOptions()
	if len(opts) > 0 {
		o = opts[0]
	}

	if pr, ok := data.(*core.MaxiParseResult); ok {
		return dumpFromParseResult(pr, o)
	}

	dataMap, err := normalizeDataMap(data, o.DefaultAlias)
	if err != nil {
		return "", err
	}

	input := dumpInput{
		version:    o.Version,
		schemaFile: o.SchemaFile,
		types:      o.Types,
		data:       dataMap,
	}
	return dumpFromObjects(input, o)
}

type dumpInput struct {
	version    string
	schemaFile string
	types      []*core.MaxiTypeDef
	data       map[string][]map[string]any
}

func normalizeDataMap(data any, defaultAlias string) (map[string][]map[string]any, error) {
	switch v := data.(type) {
	case []map[string]any:
		if defaultAlias == "" {
			return nil, fmt.Errorf("DumpMaxi requires DefaultAlias when dumping a slice")
		}
		return map[string][]map[string]any{defaultAlias: v}, nil

	case map[string]any:
		firstIsSlice := false
		for _, fv := range v {
			if _, ok := fv.([]map[string]any); ok {
				firstIsSlice = true
			} else if _, ok := fv.([]any); ok {
				firstIsSlice = true
			}
			break
		}
		if firstIsSlice {
			out := make(map[string][]map[string]any, len(v))
			for k, rows := range v {
				switch r := rows.(type) {
				case []map[string]any:
					out[k] = r
				case []any:
					ms := make([]map[string]any, 0, len(r))
					for _, e := range r {
						if m, ok := e.(map[string]any); ok {
							ms = append(ms, m)
						}
					}
					out[k] = ms
				}
			}
			return out, nil
		}
		if defaultAlias == "" {
			return nil, fmt.Errorf("DumpMaxi requires DefaultAlias when dumping a single object")
		}
		return map[string][]map[string]any{defaultAlias: {v}}, nil

	case map[string][]map[string]any:
		return v, nil

	case []any:
		if defaultAlias == "" {
			return nil, fmt.Errorf("DumpMaxi requires DefaultAlias when dumping a slice")
		}
		ms := make([]map[string]any, 0, len(v))
		for _, e := range v {
			if m, ok := e.(map[string]any); ok {
				ms = append(ms, m)
			}
		}
		return map[string][]map[string]any{defaultAlias: ms}, nil

	default:
		return nil, fmt.Errorf("DumpMaxi: unsupported data type %T", data)
	}
}

func dumpFromParseResult(result *core.MaxiParseResult, o DumpOptions) (string, error) {
	var out []string

	schema := result.Schema
	if schema != nil {
		if schema.Version != "" && schema.Version != "1.0.0" {
			out = append(out, fmt.Sprintf("@version:%s", schema.Version))
		}
		for _, imp := range schema.Imports {
			out = append(out, fmt.Sprintf("@schema:%s", imp))
		}
		if o.IncludeTypes {
			types := schema.Types()
			if len(types) > 0 {
				if len(out) > 0 {
					out = append(out, "")
				}
				for _, td := range types {
					out = append(out, dumpTypeDef(td, o.Multiline))
				}
			}
		}
	}

	if len(out) > 0 {
		out = append(out, "###")
	}

	for _, rec := range result.Records {
		out = append(out, dumpRecord(rec, schema, o.Multiline))
	}

	if len(out) == 0 {
		return "", nil
	}
	totalLen := len(out) - 1
	for _, s := range out {
		totalLen += len(s)
	}
	var sb strings.Builder
	sb.Grow(totalLen)
	for i, s := range out {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(s)
	}
	return sb.String(), nil
}

func dumpFromObjects(input dumpInput, o DumpOptions) (string, error) {
	var out []string

	allTypes := make(map[string]*core.MaxiTypeDef, len(input.types))
	for _, td := range input.types {
		if td != nil {
			allTypes[td.Alias] = td
		}
	}

	resolveInheritanceForDump(allTypes)
	populateEnumAliases(allTypes)

	if input.version != "" && input.version != "1.0.0" {
		out = append(out, fmt.Sprintf("@version:%s", input.version))
	}
	if input.schemaFile != "" {
		out = append(out, fmt.Sprintf("@schema:%s", input.schemaFile))
	}

	if o.IncludeTypes && len(allTypes) > 0 {
		if len(out) > 0 {
			out = append(out, "")
		}
		for _, td := range input.types {
			if td != nil {
				out = append(out, dumpTypeDef(td, o.Multiline))
			}
		}
	}

	if len(out) > 0 {
		out = append(out, "###")
	}

	orderedAliases := make([]string, 0)
	seen := make(map[string]bool)
	for _, td := range input.types {
		if td != nil && !seen[td.Alias] {
			orderedAliases = append(orderedAliases, td.Alias)
			seen[td.Alias] = true
		}
	}
	for alias := range input.data {
		if !seen[alias] {
			orderedAliases = append(orderedAliases, alias)
			seen[alias] = true
		}
	}

	recordsToDump := make(map[string][]map[string]any, len(input.data))
	for alias, rows := range input.data {
		cp := make([]map[string]any, len(rows))
		copy(cp, rows)
		recordsToDump[alias] = cp
	}

	if o.CollectReferences {
		collectReferencedObjects(allTypes, recordsToDump, orderedAliases)
	}

	for _, alias := range orderedAliases {
		rows := recordsToDump[alias]
		td := allTypes[alias]
		for _, obj := range rows {
			out = append(out, dumpObjectAsRecord(alias, obj, td, allTypes, o.Multiline, o.CollectReferences))
		}
	}

	if len(out) == 0 {
		return "", nil
	}
	totalLen := len(out) - 1
	for _, s := range out {
		totalLen += len(s)
	}
	var sb strings.Builder
	sb.Grow(totalLen)
	for i, s := range out {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(s)
	}
	return sb.String(), nil
}

func collectReferencedObjects(
	allTypes map[string]*core.MaxiTypeDef,
	recordsToDump map[string][]map[string]any,
	orderedAliases []string,
) {
	type workItem struct {
		alias string
		obj   map[string]any
	}

	seen := make(map[string]bool)
	work := make([]workItem, 0)

	for alias, rows := range recordsToDump {
		for _, obj := range rows {
			key := fmt.Sprintf("%p", obj)
			if !seen[key] {
				seen[key] = true
				work = append(work, workItem{alias, obj})
			}
		}
	}

	for len(work) > 0 {
		item := work[len(work)-1]
		work = work[:len(work)-1]

		td := allTypes[item.alias]
		if td == nil {
			continue
		}

		for _, field := range td.Fields {
			v := item.obj[field.Name]
			if v == nil {
				continue
			}

			baseType := field.TypeExpr
			if idx := strings.LastIndex(baseType, "[]"); idx >= 0 {
				baseType = baseType[:idx]
			}
			nestedTd := allTypes[baseType]
			if nestedTd == nil {
				continue
			}
			idField := nestedTd.IDField()
			if idField == nil {
				continue
			}

			var items []map[string]any
			switch vt := v.(type) {
			case map[string]any:
				items = []map[string]any{vt}
			case []any:
				for _, e := range vt {
					if m, ok := e.(map[string]any); ok {
						items = append(items, m)
					}
				}
			case []map[string]any:
				items = vt
			}

			for _, nestedObj := range items {
				if nestedObj[idField.Name] == nil {
					continue
				}
				key := fmt.Sprintf("%p", nestedObj)
				if seen[key] {
					continue
				}
				seen[key] = true
				recordsToDump[nestedTd.Alias] = append(recordsToDump[nestedTd.Alias], nestedObj)
				alreadyOrdered := false
				for _, a := range orderedAliases {
					if a == nestedTd.Alias {
						alreadyOrdered = true
						break
					}
				}
				if !alreadyOrdered {
					orderedAliases = append(orderedAliases, nestedTd.Alias)
				}
				work = append(work, workItem{nestedTd.Alias, nestedObj})
			}
		}
	}
}

func resolveInheritanceForDump(allTypes map[string]*core.MaxiTypeDef) {
	resolved := make(map[string]bool)
	var resolve func(string)
	resolve = func(alias string) {
		if resolved[alias] {
			return
		}
		td := allTypes[alias]
		if td == nil || len(td.Parents) == 0 {
			resolved[alias] = true
			return
		}
		ownNames := make(map[string]bool, len(td.Fields))
		for _, f := range td.Fields {
			ownNames[f.Name] = true
		}
		var inherited []*core.MaxiFieldDef
		for _, parentAlias := range td.Parents {
			resolve(parentAlias)
			parent := allTypes[parentAlias]
			if parent == nil {
				continue
			}
			for _, pf := range parent.Fields {
				if !ownNames[pf.Name] {
					cp := *pf
					inherited = append(inherited, &cp)
					ownNames[pf.Name] = true
				}
			}
		}
		if len(inherited) > 0 {
			td.Fields = append(inherited, td.Fields...)
		}
		resolved[alias] = true
	}
	for alias := range allTypes {
		resolve(alias)
	}
}

func dumpTypeDef(td *core.MaxiTypeDef, multiline bool) string {
	header := td.Alias
	if td.Name != "" {
		header = td.Alias + ":" + td.Name
	}
	parents := ""
	if len(td.Parents) > 0 {
		parents = "<" + strings.Join(td.Parents, ",") + ">"
	}

	if !multiline {
		fields := make([]string, len(td.Fields))
		for i, f := range td.Fields {
			fields[i] = dumpFieldDef(f)
		}
		return fmt.Sprintf("%s%s(%s)", header, parents, strings.Join(fields, "|"))
	}

	lines := make([]string, len(td.Fields))
	for i, f := range td.Fields {
		lines[i] = "  " + dumpFieldDef(f)
	}
	return fmt.Sprintf("%s%s(\n%s\n)", header, parents, strings.Join(lines, "|\n"))
}

func dumpFieldDef(f *core.MaxiFieldDef) string {
	result := f.Name

	if f.TypeExpr != "" && len(f.ElementConstraints) > 0 && strings.Contains(f.TypeExpr, "[]") {
		lastBracket := strings.LastIndex(f.TypeExpr, "[]")
		base := f.TypeExpr[:lastBracket]
		suffix := f.TypeExpr[lastBracket:]
		result += ":" + base
		ec := dumpConstraints(f.ElementConstraints)
		if len(ec) > 0 {
			result += "(" + strings.Join(ec, ",") + ")"
		}
		result += suffix
		if len(f.Constraints) > 0 {
			cs := dumpConstraints(f.Constraints)
			if len(cs) > 0 {
				result += "(" + strings.Join(cs, ",") + ")"
			}
		}
		if f.Annotation != "" {
			result += "@" + f.Annotation
		}
	} else {
		if f.TypeExpr != "" {
			result += ":" + f.TypeExpr
		}
		if f.Annotation != "" {
			result += "@" + f.Annotation
		}
		if len(f.Constraints) > 0 {
			cs := dumpConstraints(f.Constraints)
			if len(cs) > 0 {
				result += "(" + strings.Join(cs, ",") + ")"
			}
		}
	}

	if f.DefaultValue != nil {
		var defStr string
		if s, ok := f.DefaultValue.(string); ok && needsQuoting(s) {
			defStr = `"` + escapeString(s) + `"`
		} else {
			defStr = fmt.Sprintf("%v", f.DefaultValue)
		}
		result += "=" + defStr
	}

	return result
}

func dumpConstraints(cs []core.ParsedConstraint) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		s := dumpConstraint(c)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func dumpConstraint(c core.ParsedConstraint) string {
	switch c.Type {
	case core.ConstraintRequired:
		return "!"
	case core.ConstraintID:
		return "id"
	case core.ConstraintComparison:
		return fmt.Sprintf("%s%v", c.Operator, c.Value)
	case core.ConstraintPattern:
		return fmt.Sprintf("pattern:%v", c.Value)
	case core.ConstraintMime:
		if vals, ok := c.Value.([]string); ok && len(vals) > 1 {
			return "mime:[" + strings.Join(vals, ",") + "]"
		}
		return fmt.Sprintf("mime:%v", c.Value)
	case core.ConstraintDecimalPrecision:
		return fmt.Sprintf("%v", c.Value)
	case core.ConstraintExactLength:
		return fmt.Sprintf("=%v", c.Value)
	}
	return ""
}

func dumpRecord(rec *core.MaxiRecord, schema *core.MaxiSchema, multiline bool) string {
	var td *core.MaxiTypeDef
	if schema != nil {
		td = schema.GetType(rec.Alias)
	}
	var sb strings.Builder
	sb.Grow(len(rec.Alias) + len(rec.Values)*16)
	sb.WriteString(rec.Alias)
	if !multiline {
		sb.WriteByte('(')
		for i, v := range rec.Values {
			if i > 0 {
				sb.WriteByte('|')
			}
			var fi *core.MaxiFieldDef
			if td != nil && i < len(td.Fields) {
				fi = td.Fields[i]
			}
			sb.WriteString(dumpValue(v, fi, nil, true))
		}
		sb.WriteByte(')')
	} else {
		sb.WriteString("(\n")
		for i, v := range rec.Values {
			if i > 0 {
				sb.WriteString("|\n")
			}
			sb.WriteString("  ")
			var fi *core.MaxiFieldDef
			if td != nil && i < len(td.Fields) {
				fi = td.Fields[i]
			}
			sb.WriteString(dumpValue(v, fi, nil, true))
		}
		sb.WriteString("\n)")
	}
	return sb.String()
}

func dumpObjectAsRecord(
	alias string,
	obj map[string]any,
	td *core.MaxiTypeDef,
	allTypes map[string]*core.MaxiTypeDef,
	multiline bool,
	collectRefs bool,
) string {
	var vals []string
	if td != nil {
		vals = make([]string, 0, len(td.Fields))
		for _, f := range td.Fields {
			v, ok := obj[f.Name]
			if !ok {
				vals = append(vals, "")
				continue
			}
			if v == nil {
				vals = append(vals, "~")
			} else {
				vals = append(vals, dumpValue(v, f, allTypes, collectRefs))
			}
		}
	} else {
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := obj[k]
			if v == nil {
				vals = append(vals, "~")
			} else {
				vals = append(vals, dumpValue(v, nil, allTypes, collectRefs))
			}
		}
	}

	last := len(vals) - 1
	for last >= 0 && isTrailingEmpty(vals[last]) {
		last--
	}
	vals = vals[:last+1]

	var sb strings.Builder
	sb.Grow(len(alias) + len(vals)*16)
	sb.WriteString(alias)
	if !multiline {
		sb.WriteByte('(')
		for i, v := range vals {
			if i > 0 {
				sb.WriteByte('|')
			}
			sb.WriteString(v)
		}
		sb.WriteByte(')')
	} else {
		sb.WriteString("(\n")
		for i, v := range vals {
			if i > 0 {
				sb.WriteString("|\n")
			}
			sb.WriteString("  ")
			sb.WriteString(v)
		}
		sb.WriteString("\n)")
	}
	return sb.String()
}

func populateEnumAliases(allTypes map[string]*core.MaxiTypeDef) {
	for _, td := range allTypes {
		for _, f := range td.Fields {
			if f.EnumAliases == nil && strings.HasPrefix(f.TypeExpr, "enum") {
				f.EnumAliases = internal.ParseEnumAliases(f.TypeExpr)
			}
		}
	}
}

func dumpValue(v any, fieldInfo *core.MaxiFieldDef, allTypes map[string]*core.MaxiTypeDef, collectRefs bool) string {
	if v == nil {
		return "~"
	}

	if fieldInfo != nil && fieldInfo.EnumAliases != nil {
		strVal := fmt.Sprintf("%v", v)
		if _, isAlias := fieldInfo.EnumAliases[strVal]; isAlias {
			return strVal
		}
		for alias, fullVal := range fieldInfo.EnumAliases {
			if fullVal == strVal {
				return alias
			}
		}
	}

	switch val := v.(type) {
	case bool:
		if val {
			return "1"
		}
		return "0"

	case string:
		if needsQuoting(val) {
			return `"` + escapeString(val) + `"`
		}
		return val

	case int:
		return strconv.Itoa(val)
	case int8:
		return strconv.FormatInt(int64(val), 10)
	case int16:
		return strconv.FormatInt(int64(val), 10)
	case int32:
		return strconv.FormatInt(int64(val), 10)
	case int64:
		return strconv.FormatInt(val, 10)
	case uint:
		return strconv.FormatUint(uint64(val), 10)
	case uint64:
		return strconv.FormatUint(val, 10)
	case float32:
		return strconv.FormatFloat(float64(val), 'g', -1, 32)
	case float64:
		return strconv.FormatFloat(val, 'g', -1, 64)

	case []byte:
		ann := ""
		if fieldInfo != nil {
			ann = fieldInfo.Annotation
		}
		if ann == "hex" {
			return fmt.Sprintf("%x", val)
		}
		return encodeBase64(val)

	case []any:
		return dumpSlice(val, fieldInfo, allTypes, collectRefs)

	case []map[string]any:
		anySlice := make([]any, len(val))
		for i, m := range val {
			anySlice[i] = m
		}
		return dumpSlice(anySlice, fieldInfo, allTypes, collectRefs)

	case map[string]any:
		return dumpMapValue(val, fieldInfo, allTypes, collectRefs)
	}

	return fmt.Sprintf("%v", v)
}

func dumpSlice(items []any, fieldInfo *core.MaxiFieldDef, allTypes map[string]*core.MaxiTypeDef, collectRefs bool) string {
	var elemInfo *core.MaxiFieldDef
	if fieldInfo != nil && strings.HasSuffix(fieldInfo.TypeExpr, "[]") {
		cp := *fieldInfo
		cp.TypeExpr = fieldInfo.TypeExpr[:len(fieldInfo.TypeExpr)-2]
		elemInfo = &cp
	} else {
		elemInfo = fieldInfo
	}

	parts := make([]string, len(items))
	for i, item := range items {
		if item == nil {
			parts[i] = "~"
		} else {
			parts[i] = dumpValue(item, elemInfo, allTypes, collectRefs)
		}
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func dumpMapValue(obj map[string]any, fieldInfo *core.MaxiFieldDef, allTypes map[string]*core.MaxiTypeDef, collectRefs bool) string {
	if allTypes == nil {
		allTypes = map[string]*core.MaxiTypeDef{}
	}

	refTypeAlias := ""
	if fieldInfo != nil && fieldInfo.TypeExpr != "" {
		base := strings.TrimSuffix(fieldInfo.TypeExpr, "[]")
		if _, ok := allTypes[base]; ok {
			refTypeAlias = base
		}
	}

	if refTypeAlias != "" {
		nestedTd := allTypes[refTypeAlias]
		idField := nestedTd.IDField()
		if idField != nil && obj[idField.Name] != nil {
			if !collectRefs {
				return dumpInlineObject(obj, nestedTd, allTypes, collectRefs)
			}
			return dumpValue(obj[idField.Name], nil, allTypes, collectRefs)
		}
		return dumpInlineObject(obj, nestedTd, allTypes, collectRefs)
	}

	parts := make([]string, 0, len(obj))
	for k, val := range obj {
		keyStr := k
		if needsQuoting(k) {
			keyStr = `"` + escapeString(k) + `"`
		}
		parts = append(parts, keyStr+":"+dumpValue(val, nil, allTypes, collectRefs))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func dumpInlineObject(obj map[string]any, td *core.MaxiTypeDef, allTypes map[string]*core.MaxiTypeDef, collectRefs bool) string {
	if td == nil {
		return dumpMapValue(obj, nil, allTypes, collectRefs)
	}
	vals := make([]string, 0, len(td.Fields))
	for _, f := range td.Fields {
		v, ok := obj[f.Name]
		if !ok {
			vals = append(vals, "")
			continue
		}
		if v == nil {
			vals = append(vals, "~")
		} else {
			vals = append(vals, dumpValue(v, f, allTypes, collectRefs))
		}
	}
	last := len(vals) - 1
	for last >= 0 && isTrailingEmpty(vals[last]) {
		last--
	}
	return "(" + strings.Join(vals[:last+1], "|") + ")"
}

func isTrailingEmpty(s string) bool {
	return s == "" || s == `""`
}

func needsQuoting(s string) bool {
	if s == "" || s == "~" {
		return true
	}
	if s[0] <= ' ' || s[len(s)-1] <= ' ' {
		return true
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 {
			return true
		}
		switch c {
		case '|', '(', ')', '[', ']', '{', '}', '~', ',', ':', '\\', '"':
			return true
		}
	}
	return false
}

func escapeString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}

func encodeBase64(b []byte) string {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	n := len(b)
	out := make([]byte, 0, ((n+2)/3)*4)
	for i := 0; i < n; i += 3 {
		b0 := b[i]
		var b1, b2 byte
		if i+1 < n {
			b1 = b[i+1]
		}
		if i+2 < n {
			b2 = b[i+2]
		}
		out = append(out, chars[b0>>2])
		out = append(out, chars[((b0&3)<<4)|(b1>>4)])
		if i+1 < n {
			out = append(out, chars[((b1&0xf)<<2)|(b2>>6)])
		} else {
			out = append(out, '=')
		}
		if i+2 < n {
			out = append(out, chars[b2&0x3f])
		} else {
			out = append(out, '=')
		}
	}
	return string(out)
}
