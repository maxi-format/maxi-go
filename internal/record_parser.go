package internal

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/maxi-format/maxi-go/core"
)

var explicitNull = struct{}{}

var patternCache sync.Map

var enumParseRe = regexp.MustCompile(`^enum(?:<\w+>)?\[([^\]]*)\]$`)

type RecordParser struct {
	recordsText string
	result      *core.MaxiParseResult
	opts        core.ParseOptions
	filename    string
	seenIDs     map[string]map[string]bool
}

func NewRecordParser(recordsText string, result *core.MaxiParseResult, opts core.ParseOptions, filename string) *RecordParser {
	return &RecordParser{
		recordsText: recordsText,
		result:      result,
		opts:        opts,
		filename:    filename,
		seenIDs:     make(map[string]map[string]bool),
	}
}

func (p *RecordParser) Parse() error {
	text := p.recordsText
	if strings.TrimSpace(text) == "" {
		return nil
	}

	n := len(text)
	i := 0
	lineNumber := 1
	atLineStart := true

	for i < n {
		ch := text[i]

		if ch == '\n' {
			lineNumber++
			i++
			atLineStart = true
			continue
		}

		if ch == ' ' || ch == '\t' || ch == '\r' {
			i++
			continue
		}

		if ch == '#' {
			atLineStart = false
			for i < n && text[i] != '\n' {
				i++
			}
			continue
		}

		if !isAliasStart(ch) {
			if atLineStart {
				return core.NewErrorAt(core.ErrInvalidSyntax,
					fmt.Sprintf("invalid syntax in data section: unexpected character '%c' at line %d", ch, lineNumber),
					lineNumber, 0)
			}
			i++
			continue
		}
		atLineStart = false

		aliasStart := i
		i++
		for i < n && isAliasChar(text[i]) {
			i++
		}
		alias := text[aliasStart:i]

		for i < n && (text[i] == ' ' || text[i] == '\t' || text[i] == '\r') {
			i++
		}

		if i < n && text[i] == ':' {
			return core.NewErrorAt(core.ErrStream,
				fmt.Sprintf("type definition '%s:...' found in data section (after ###). Type definitions must appear before ###", alias),
				lineNumber, 0)
		}

		if i >= n || text[i] != '(' {
			continue
		}

		recordLine := lineNumber
		i++
		valuesStart := i

		parenDepth := 1
		bracketDepth := 0
		braceDepth := 0
		inStr := false
		escape := false

		for i < n {
			c := text[i]
			if c == '\n' {
				lineNumber++
			}
			if escape {
				escape = false
				i++
				continue
			}
			if inStr {
				if c == '\\' {
					escape = true
				} else if c == '"' {
					inStr = false
				}
				i++
				continue
			}
			switch c {
			case '"':
				inStr = true
			case '(':
				parenDepth++
			case ')':
				parenDepth--
				if parenDepth == 0 {
					goto endScan
				}
			case '[':
				bracketDepth++
			case ']':
				if bracketDepth > 0 {
					bracketDepth--
				}
			case '{':
				braceDepth++
			case '}':
				if braceDepth > 0 {
					braceDepth--
				}
			}
			i++
		}
	endScan:

		if i >= n || text[i] != ')' || parenDepth != 0 {
			if bracketDepth != 0 {
				return core.NewErrorAt(core.ErrArraySyntax,
					fmt.Sprintf("malformed array: unmatched bracket in record '%s'", alias),
					recordLine, 0)
			}
			return core.NewErrorAt(core.ErrInvalidSyntax,
				fmt.Sprintf("unclosed record parentheses for '%s'", alias),
				recordLine, 0)
		}

		valuesStr := text[valuesStart:i]
		i++

		record, err := p.parseSingleRecord(alias, valuesStr, recordLine)
		if err != nil {
			return err
		}
		p.result.Records = append(p.result.Records, record)
	}
	return nil
}

func (p *RecordParser) ParseSingleRecord(alias, valuesStr string, lineNumber int) (*core.MaxiRecord, error) {
	return p.parseSingleRecord(alias, valuesStr, lineNumber)
}

func (p *RecordParser) parseSingleRecord(alias, valuesStr string, lineNumber int) (*core.MaxiRecord, error) {
	td := p.result.Schema.GetType(alias)
	if td == nil {
		switch p.opts.AllowUnknownTypes {
		case core.UnknownTypesError:
			return nil, core.NewErrorAt(core.ErrUnknownType,
				fmt.Sprintf("unknown type alias '%s'", alias), lineNumber, 0)
		case core.UnknownTypesWarning:
			p.result.AddWarning(core.ErrUnknownType,
				fmt.Sprintf("unknown type alias '%s'", alias), lineNumber)
		}
		values, err := p.parseFieldValues(valuesStr, nil, lineNumber)
		if err != nil {
			return nil, err
		}
		return &core.MaxiRecord{Alias: alias, Values: anySlice(values), LineNumber: lineNumber}, nil
	}

	rawValues, err := p.parseFieldValues(valuesStr, td, lineNumber)
	if err != nil {
		return nil, err
	}

	if p.opts.AllowAdditionalFields != core.AdditionalFieldsError {
		if idx := findFieldIndex(td, "type"); idx != -1 && len(rawValues) == len(td.Fields)-1 {
			typeField := td.Fields[idx]
			var inferred any
			if typeField.DefaultValue != nil {
				inferred = typeField.DefaultValue
			} else if td.Name != "" {
				inferred = strings.ToLower(td.Name)
			} else {
				inferred = strings.ToLower(td.Alias)
			}
			patched := make([]any, len(rawValues)+1)
			copy(patched[:idx], rawValues[:idx])
			patched[idx] = inferred
			copy(patched[idx+1:], rawValues[idx:])
			rawValues = patched
		}
	}

	if p.opts.AllowMissingFields == core.MissingFieldsError {
		for i := len(rawValues); i < len(td.Fields); i++ {
			if td.IsRequired(i) && td.Fields[i].DefaultValue == nil {
				return nil, core.NewErrorAt(core.ErrMissingRequiredField,
					fmt.Sprintf("record '%s' missing required field '%s'", alias, td.Fields[i].Name),
					lineNumber, 0)
			}
		}
	}

	if len(rawValues) > len(td.Fields) {
		msg := fmt.Sprintf("record '%s' has %d values but type defines %d fields",
			alias, len(rawValues), len(td.Fields))
		switch p.opts.AllowAdditionalFields {
		case core.AdditionalFieldsError:
			return nil, core.NewErrorAt(core.ErrSchemaMismatch, msg, lineNumber, 0)
		case core.AdditionalFieldsWarning:
			p.result.AddWarning(core.ErrSchemaMismatch, msg, lineNumber)
		}
	}

	fieldCount := len(td.Fields)
	finalValues := make([]any, fieldCount)
	for i := 0; i < fieldCount; i++ {
		field := td.Fields[i]
		var value any
		if i < len(rawValues) {
			value = rawValues[i]
		}

		if value == explicitNull {
			if td.IsRequired(i) && field.DefaultValue != nil {
				msg := fmt.Sprintf("field '%s' is required with a default; explicit null (~) is not allowed", field.Name)
				if p.opts.AllowMissingFields == core.MissingFieldsError {
					return nil, core.NewErrorAt(core.ErrMissingRequiredField, msg, lineNumber, 0)
				}
				p.result.AddWarning(core.ErrMissingRequiredField, msg, lineNumber)
			}
			value = nil
		} else if value == nil || value == "" {
			if field.DefaultValue != nil {
				value = field.DefaultValue
			} else {
				value = nil
			}
		}

		if td.IsRequired(i) && value == nil {
			msg := fmt.Sprintf("required field '%s' is null in record '%s'", field.Name, alias)
			if p.opts.AllowMissingFields == core.MissingFieldsError {
				return nil, core.NewErrorAt(core.ErrMissingRequiredField, msg, lineNumber, 0)
			}
			p.result.AddWarning(core.ErrMissingRequiredField, msg, lineNumber)
		}

		finalValues[i] = value
	}

	for i, f := range td.Fields {
		if !strings.HasPrefix(f.TypeExpr, "enum") {
			continue
		}
		v := finalValues[i]
		if v == nil {
			continue
		}
		vals := f.EnumValues
		if vals == nil {
			vals = parseEnumValues(f.TypeExpr)
		}
		if vals == nil {
			continue
		}
		strVal := anyToString(v)
		found := false
		for _, ev := range vals {
			if ev == strVal {
				found = true
				break
			}
		}
		if !found {
			msg := fmt.Sprintf("value '%s' not in enum [%s] for field '%s'",
				strVal, strings.Join(vals, ","), f.Name)
			if p.opts.AllowConstraintViolations == core.ConstraintViolationsError {
				return nil, core.NewErrorAt(core.ErrConstraintViolation, msg, lineNumber, 0)
			}
			p.result.AddWarning(core.ErrConstraintViolation, msg, lineNumber)
		}
	}

	if td.HasRuntimeConstraints() {
		if err := validateRecordConstraints(finalValues, td, p.opts.AllowConstraintViolations == core.ConstraintViolationsError, p.result, lineNumber); err != nil {
			return nil, err
		}
	}

	idF := td.IDField()
	if idF != nil {
		idIdx := findFieldIndex(td, idF.Name)
		if idIdx >= 0 && idIdx < len(finalValues) && finalValues[idIdx] != nil {
			idKey := fmt.Sprintf("%v", finalValues[idIdx])
			if p.seenIDs[alias] == nil {
				p.seenIDs[alias] = make(map[string]bool)
			}
			if p.seenIDs[alias][idKey] {
				msg := fmt.Sprintf("duplicate identifier '%s' for type '%s'", idKey, alias)
				if p.opts.AllowConstraintViolations == core.ConstraintViolationsError {
					return nil, core.NewErrorAt(core.ErrDuplicateIdentifier, msg, lineNumber, 0)
				}
				p.result.AddWarning(core.ErrDuplicateIdentifier, msg, lineNumber)
			}
			p.seenIDs[alias][idKey] = true
		}
	}

	return &core.MaxiRecord{Alias: alias, Values: finalValues, LineNumber: lineNumber}, nil
}

func (p *RecordParser) parseFieldValues(valuesStr string, td *core.MaxiTypeDef, lineNumber int) ([]any, error) {
	if valuesStr == "" {
		return nil, nil
	}

	simple := true
	for _, c := range []byte(valuesStr) {
		if c == '"' || c == '(' || c == ')' || c == '[' || c == ']' || c == '{' || c == '}' {
			simple = false
			break
		}
	}

	var rawParts []string
	if simple {
		rawParts = strings.Split(valuesStr, "|")
	} else {
		rawParts = splitTopLevel(valuesStr, '|')
	}

	var out []any
	for fi, part := range rawParts {
		part = strings.Trim(part, " \t\r\n")
		var fieldDef *core.MaxiFieldDef
		if td != nil && fi < len(td.Fields) {
			fieldDef = td.Fields[fi]
		}
		v, err := p.parseFieldValue(part, fieldDef, lineNumber)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (p *RecordParser) parseFieldValue(valueStr string, fieldDef *core.MaxiFieldDef, lineNumber int) (any, error) {
	if valueStr == "" {
		if fieldDef != nil && fieldDef.DefaultValue != nil {
			return fieldDef.DefaultValue, nil
		}
		return nil, nil
	}
	if valueStr == "~" {
		return explicitNull, nil
	}

	c0 := valueStr[0]
	cL := valueStr[len(valueStr)-1]

	if c0 == '[' {
		if cL != ']' {
			return nil, core.NewErrorAt(core.ErrArraySyntax,
				"malformed array: unmatched opening bracket", lineNumber, 0)
		}
		return p.parseArray(valueStr, fieldDef, lineNumber)
	}

	if c0 == '{' && cL == '}' {
		return p.parseMap(valueStr, fieldDef, lineNumber)
	}

	if c0 == '(' && cL == ')' {
		return p.parseInlineObject(valueStr, fieldDef, lineNumber)
	}

	if c0 == '"' && cL == '"' {
		return unescapeString(valueStr[1 : len(valueStr)-1]), nil
	}

	typeExpr := "str"
	if fieldDef != nil && fieldDef.TypeExpr != "" {
		typeExpr = fieldDef.TypeExpr
	}

	baseType := scalarBaseType(typeExpr)

	switch baseType {
	case "int":
		return p.coerceInt(valueStr, lineNumber)

	case "bool":
		return p.coerceBool(valueStr, lineNumber)

	case "str":
		return valueStr, nil

	case "float":
		return p.coerceFloat(valueStr, lineNumber)

	case "decimal":
		return p.coerceDecimalStr(valueStr, lineNumber)

	case "bytes":
		if fieldDef != nil && fieldDef.Annotation == "base64" && p.opts.AllowTypeCoercion != core.TypeCoercionError {
			if looksLikeBase64(valueStr) {
				mod := len(valueStr) & 3
				switch mod {
				case 1:
					return valueStr + "===", nil
				case 2:
					return valueStr + "==", nil
				case 3:
					return valueStr + "=", nil
				}
			}
		}
		return valueStr, nil
	}

	if strings.HasPrefix(typeExpr, "enum") {
		if fieldDef != nil && fieldDef.EnumAliases != nil {
			if fullVal, ok := fieldDef.EnumAliases[valueStr]; ok {
				valueStr = fullVal
			}
		}
		if m := enumIntRe.FindStringSubmatch(typeExpr); m != nil {
			if n, err := strconv.Atoi(valueStr); err == nil {
				return n, nil
			}
		}
		return valueStr, nil
	}

	return valueStr, nil
}

var enumIntRe = regexp.MustCompile(`^enum<int>\[`)

func (p *RecordParser) coerceInt(s string, lineNumber int) (any, error) {
	nk := detectNumberKind(s)
	if nk == kindInt {
		n, _ := strconv.ParseInt(s, 10, 64)
		return int(n), nil
	}
	if p.opts.AllowTypeCoercion == core.TypeCoercionError {
		return nil, core.NewErrorAt(core.ErrTypeMismatch,
			fmt.Sprintf("type mismatch: field expects int, got '%s'", s), lineNumber, 0)
	}
	if nk == kindDecimal || nk == kindTrailingDot {
		p.result.AddWarning(core.ErrTypeMismatch,
			fmt.Sprintf("type coercion: value '%s' coerced to int, fractional part lost", s), lineNumber)
		n, _ := strconv.ParseFloat(s, 64)
		return int(n), nil
	}
	p.result.AddWarning(core.ErrTypeMismatch,
		fmt.Sprintf("type mismatch: field expects int, got '%s'", s), lineNumber)
	return s, nil
}

func (p *RecordParser) coerceBool(s string, lineNumber int) (any, error) {
	if s == "1" || s == "true" {
		return true, nil
	}
	if s == "0" || s == "false" {
		return false, nil
	}
	if p.opts.AllowTypeCoercion == core.TypeCoercionError {
		return nil, core.NewErrorAt(core.ErrTypeMismatch,
			fmt.Sprintf("type mismatch: field expects bool, got '%s'", s), lineNumber, 0)
	}
	if p.opts.AllowTypeCoercion == core.TypeCoercionWarning {
		p.result.AddWarning(core.ErrTypeMismatch,
			fmt.Sprintf("type coercion: value '%s' is not a valid bool", s), lineNumber)
	}
	return s, nil
}

func (p *RecordParser) coerceFloat(s string, lineNumber int) (any, error) {
	nk := detectNumberKind(s)
	fk := detectFloatKind(s)
	if fk || nk == kindInt || nk == kindDecimal || nk == kindTrailingDot {
		f, _ := strconv.ParseFloat(s, 64)
		return f, nil
	}
	if p.opts.AllowTypeCoercion == core.TypeCoercionError {
		return nil, core.NewErrorAt(core.ErrTypeMismatch,
			fmt.Sprintf("type mismatch: field expects float, got '%s'", s), lineNumber, 0)
	}
	if p.opts.AllowTypeCoercion == core.TypeCoercionWarning {
		p.result.AddWarning(core.ErrTypeMismatch,
			fmt.Sprintf("type coercion: value '%s' is not a valid float", s), lineNumber)
	}
	return s, nil
}

func (p *RecordParser) coerceDecimalStr(s string, lineNumber int) (any, error) {
	nk := detectNumberKind(s)
	if nk == kindTrailingDot {
		return s[:len(s)-1], nil
	}
	if nk == kindInt || nk == kindDecimal {
		return s, nil
	}
	if p.opts.AllowTypeCoercion == core.TypeCoercionError {
		return nil, core.NewErrorAt(core.ErrTypeMismatch,
			fmt.Sprintf("type mismatch: field expects decimal, got '%s'", s), lineNumber, 0)
	}
	if p.opts.AllowTypeCoercion == core.TypeCoercionWarning {
		p.result.AddWarning(core.ErrTypeMismatch,
			fmt.Sprintf("type coercion: value '%s' is not a valid decimal", s), lineNumber)
	}
	return s, nil
}

func (p *RecordParser) parseArray(arrayStr string, fieldDef *core.MaxiFieldDef, lineNumber int) (any, error) {
	content := strings.TrimSpace(arrayStr[1 : len(arrayStr)-1])
	if content == "" {
		return []any{}, nil
	}

	elemTypeExpr := ""
	if fieldDef != nil {
		elemTypeExpr = getArrayElementType(fieldDef.TypeExpr)
	}
	var elemFieldDef *core.MaxiFieldDef
	if elemTypeExpr != "" {
		elemFieldDef = &core.MaxiFieldDef{TypeExpr: elemTypeExpr}
	}

	parts := splitOnTopLevelCommasInContainer(content)
	var elems []any
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		v, err := p.parseFieldValue(part, elemFieldDef, lineNumber)
		if err != nil {
			return nil, err
		}
		elems = append(elems, v)
	}
	return elems, nil
}

func (p *RecordParser) parseMap(mapStr string, fieldDef *core.MaxiFieldDef, lineNumber int) (any, error) {
	content := strings.TrimSpace(mapStr[1 : len(mapStr)-1])
	if content == "" {
		return map[string]any{}, nil
	}

	valueTypeExpr := getMapValueType(fieldDef)
	keyTypeExpr := getMapKeyType(fieldDef)

	var valueFieldDef, keyFieldDef *core.MaxiFieldDef
	if valueTypeExpr != "" {
		baseValType, valConstraints := parseTypeExprConstraints(valueTypeExpr)
		valueFieldDef = &core.MaxiFieldDef{TypeExpr: baseValType, Constraints: valConstraints}
	} else if fieldDef != nil && fieldDef.TypeExpr != "" {
		valueFieldDef = &core.MaxiFieldDef{TypeExpr: "str"}
	}
	if keyTypeExpr != "" {
		baseKeyType, keyConstraints := parseTypeExprConstraints(keyTypeExpr)
		keyFieldDef = &core.MaxiFieldDef{TypeExpr: baseKeyType, Constraints: keyConstraints}
	}

	entries := splitOnTopLevelCommasInContainer(content)
	result := make(map[string]any, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if err := p.parseMapEntry(entry, result, lineNumber, valueFieldDef, keyFieldDef); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (p *RecordParser) parseMapEntry(entryStr string, out map[string]any, lineNumber int, valueFieldDef, keyFieldDef *core.MaxiFieldDef) error {
	colonIdx := findTopLevelChar(entryStr, ':')
	if colonIdx == -1 {
		return core.NewErrorAt(core.ErrInvalidSyntax,
			fmt.Sprintf("invalid map entry format: %s", entryStr), lineNumber, 0)
	}
	keyStr := strings.TrimSpace(entryStr[:colonIdx])
	valStr := strings.TrimSpace(entryStr[colonIdx+1:])

	kfd := keyFieldDef
	if kfd == nil {
		kfd = &core.MaxiFieldDef{TypeExpr: "str"}
	}
	k, err := p.parseFieldValue(keyStr, kfd, lineNumber)
	if err != nil {
		return err
	}
	// Validate key constraints from the key type expression
	if kfd != nil && len(kfd.Constraints) > 0 {
		if str, ok := k.(string); ok {
			for _, c := range kfd.Constraints {
				if c.Type == core.ConstraintComparison && c.Operator == ">=" {
					limit, ok := toFloat64(c.Value)
					if ok && float64(len(str)) < limit {
						msg := fmt.Sprintf("map key '%s' violates constraint >=%v on key type", str, limit)
						if p.opts.AllowConstraintViolations == core.ConstraintViolationsError {
							return core.NewErrorAt(core.ErrConstraintViolation, msg, lineNumber, 0)
						}
						p.result.AddWarning(core.ErrConstraintViolation, msg, lineNumber)
					}
				}
			}
		}
	}
	v, err := p.parseFieldValue(valStr, valueFieldDef, lineNumber)
	if err != nil {
		return err
	}
	// Validate value constraints
	if valueFieldDef != nil && len(valueFieldDef.Constraints) > 0 && v != nil {
		for _, c := range valueFieldDef.Constraints {
			if c.Type == core.ConstraintComparison {
				limit, ok := toFloat64(c.Value)
				if ok {
					actual, isNum := toFloat64(v)
					if isNum {
						violated := false
						switch c.Operator {
						case ">=":
							violated = actual < limit
						case ">":
							violated = actual <= limit
						case "<=":
							violated = actual > limit
						case "<":
							violated = actual >= limit
						}
						if violated {
							msg := fmt.Sprintf("map value %v violates constraint %s%v on value type", v, c.Operator, limit)
							if p.opts.AllowConstraintViolations == core.ConstraintViolationsError {
								return core.NewErrorAt(core.ErrConstraintViolation, msg, lineNumber, 0)
							}
							p.result.AddWarning(core.ErrConstraintViolation, msg, lineNumber)
						}
					}
				}
			}
		}
	}
	out[fmt.Sprintf("%v", k)] = v
	return nil
}

func (p *RecordParser) parseInlineObject(objStr string, fieldDef *core.MaxiFieldDef, lineNumber int) (any, error) {
	innerStr := objStr[1 : len(objStr)-1]
	typeAlias := getInlineObjectTypeAlias(fieldDef, p.result.Schema)
	if typeAlias == "" {
		values, err := p.parseFieldValues(innerStr, nil, lineNumber)
		if err != nil {
			return nil, err
		}
		return map[string]any{"values": values}, nil
	}

	td := p.result.Schema.GetType(typeAlias)
	if td == nil {
		switch p.opts.AllowUnknownTypes {
		case core.UnknownTypesError:
			return nil, core.NewErrorAt(core.ErrUnknownType,
				fmt.Sprintf("unknown type alias '%s' for inline object", typeAlias), lineNumber, 0)
		case core.UnknownTypesWarning:
			p.result.AddWarning(core.ErrUnknownType,
				fmt.Sprintf("unknown type alias '%s' for inline object", typeAlias), lineNumber)
		}
		values, err := p.parseFieldValues(innerStr, nil, lineNumber)
		if err != nil {
			return nil, err
		}
		return map[string]any{"values": values}, nil
	}

	values, err := p.parseFieldValues(innerStr, td, lineNumber)
	if err != nil {
		return nil, err
	}

	obj := make(map[string]any, len(td.Fields))
	for i, f := range td.Fields {
		var v any
		if i < len(values) {
			v = values[i]
		}
		if v == explicitNull {
			v = nil
		} else if v == nil || v == "" {
			if f.DefaultValue != nil {
				v = f.DefaultValue
			} else {
				v = nil
			}
		}
		obj[f.Name] = v
	}
	return obj, nil
}

func validateRecordConstraints(values []any, td *core.MaxiTypeDef, violationsAreErrors bool, result *core.MaxiParseResult, lineNumber int) error {
	for i, f := range td.Fields {
		if len(f.Constraints) == 0 {
			continue
		}
		var v any
		if i < len(values) {
			v = values[i]
		}
		if v == nil {
			continue
		}
		for _, c := range f.Constraints {
			msg := checkConstraint(c, v, f)
			if msg == "" {
				continue
			}
			if violationsAreErrors {
				return core.NewErrorAt(core.ErrConstraintViolation, msg, lineNumber, 0)
			}
			result.AddWarning(core.ErrConstraintViolation, msg, lineNumber)
		}
	}
	return nil
}

func checkConstraint(c core.ParsedConstraint, value any, f *core.MaxiFieldDef) string {
	switch c.Type {
	case core.ConstraintComparison:
		return checkComparison(c, value, f)
	case core.ConstraintPattern:
		return checkPattern(c, value, f)
	case core.ConstraintExactLength:
		return checkExactLength(c, value, f)
	}
	return ""
}

func checkComparison(c core.ParsedConstraint, value any, f *core.MaxiFieldDef) string {
	limit, ok := toFloat64(c.Value)
	if !ok {
		return ""
	}
	base := scalarBaseType(f.TypeExpr)
	var actual float64
	switch {
	case base == "str" || base == "bytes":
		s, ok := value.(string)
		if !ok {
			return ""
		}
		actual = float64(len(s))
	default:
		n, ok := toFloat64(value)
		if !ok {
			return ""
		}
		actual = n
	}

	violated := false
	switch c.Operator {
	case ">=":
		violated = actual < limit
	case ">":
		violated = actual <= limit
	case "<=":
		violated = actual > limit
	case "<":
		violated = actual >= limit
	}
	if violated {
		return fmt.Sprintf("field '%s': value %v violates constraint %s%v", f.Name, actual, c.Operator, limit)
	}
	return ""
}

func checkPattern(c core.ParsedConstraint, value any, f *core.MaxiFieldDef) string {
	s, ok := value.(string)
	if !ok {
		return ""
	}
	pat, _ := c.Value.(string)
	var re *regexp.Regexp
	if cached, hit := patternCache.Load(pat); hit {
		re = cached.(*regexp.Regexp)
	} else {
		compiled, err := regexp.Compile(pat)
		if err != nil {
			return ""
		}
		patternCache.Store(pat, compiled)
		re = compiled
	}
	if !re.MatchString(s) {
		return fmt.Sprintf("field '%s': value '%s' does not match pattern '%s'", f.Name, s, pat)
	}
	return ""
}

func checkExactLength(c core.ParsedConstraint, value any, f *core.MaxiFieldDef) string {
	expected, _ := c.Value.(int)
	switch v := value.(type) {
	case []any:
		if len(v) != expected {
			return fmt.Sprintf("field '%s': expected exactly %d elements, got %d", f.Name, expected, len(v))
		}
	case map[string]any:
		if len(v) != expected {
			return fmt.Sprintf("field '%s': expected exactly %d elements, got %d", f.Name, expected, len(v))
		}
	}
	return ""
}

const (
	kindNone        = 0
	kindInt         = 1
	kindDecimal     = 2
	kindTrailingDot = 3
)

func detectNumberKind(s string) int {
	n := len(s)
	if n == 0 {
		return kindNone
	}
	i := 0
	if s[0] == '-' {
		if n == 1 {
			return kindNone
		}
		i = 1
	}
	if s[i] < '0' || s[i] > '9' {
		return kindNone
	}
	for i < n && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == n {
		return kindInt
	}
	if s[i] != '.' {
		return kindNone
	}
	i++
	if i == n {
		return kindTrailingDot
	}
	if s[i] < '0' || s[i] > '9' {
		return kindNone
	}
	for i < n {
		if s[i] < '0' || s[i] > '9' {
			return kindNone
		}
		i++
	}
	return kindDecimal
}

func detectFloatKind(s string) bool {
	n := len(s)
	if n == 0 {
		return false
	}
	i := 0
	if s[0] == '-' {
		if n == 1 {
			return false
		}
		i = 1
	}
	if s[i] < '0' || s[i] > '9' {
		return false
	}
	for i < n && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i < n && s[i] == '.' {
		i++
		for i < n && s[i] >= '0' && s[i] <= '9' {
			i++
		}
	}
	if i >= n {
		return false
	}
	if s[i] != 'e' && s[i] != 'E' {
		return false
	}
	i++
	if i >= n {
		return false
	}
	if s[i] == '+' || s[i] == '-' {
		i++
		if i >= n {
			return false
		}
	}
	if s[i] < '0' || s[i] > '9' {
		return false
	}
	for i < n {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
		i++
	}
	return true
}

func looksLikeBase64(s string) bool {
	n := len(s)
	if n == 0 {
		return false
	}
	pad := 0
	for i := 0; i < n; i++ {
		c := s[i]
		if c == '=' {
			pad++
			continue
		}
		if pad > 0 {
			return false
		}
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
			(c >= '0' && c <= '9') || c == '+' || c == '/') {
			return false
		}
	}
	return pad <= 2
}

func scalarBaseType(typeExpr string) string {
	if typeExpr == "" {
		return "str"
	}
	t := typeExpr
	for strings.HasSuffix(t, "[]") {
		t = t[:len(t)-2]
	}
	if idx := strings.Index(t, "("); idx != -1 {
		t = t[:idx]
	}
	t = strings.TrimSpace(t)
	if strings.HasPrefix(t, "enum") {
		return "enum"
	}
	if strings.HasPrefix(t, "map") {
		return "map"
	}
	return t
}

func getArrayElementType(typeExpr string) string {
	if typeExpr == "" {
		return ""
	}
	t := strings.TrimSpace(typeExpr)
	if strings.HasSuffix(t, "[]") {
		return t[:len(t)-2]
	}
	return ""
}

func getMapValueType(fieldDef *core.MaxiFieldDef) string {
	if fieldDef == nil {
		return ""
	}
	t := strings.TrimSpace(fieldDef.TypeExpr)
	if t == "map" || t == "" {
		return ""
	}
	if !strings.HasPrefix(t, "map<") || !strings.HasSuffix(t, ">") {
		return ""
	}
	inside := t[4 : len(t)-1]
	parts := splitMapTypeParams(inside)
	if len(parts) == 1 {
		return parts[0]
	}
	if len(parts) >= 2 {
		return parts[len(parts)-1]
	}
	return ""
}

func getMapKeyType(fieldDef *core.MaxiFieldDef) string {
	if fieldDef == nil {
		return ""
	}
	t := strings.TrimSpace(fieldDef.TypeExpr)
	if t == "map" || t == "" {
		return ""
	}
	if !strings.HasPrefix(t, "map<") || !strings.HasSuffix(t, ">") {
		return ""
	}
	inside := t[4 : len(t)-1]
	parts := splitMapTypeParams(inside)
	if len(parts) >= 2 {
		return parts[0]
	}
	return ""
}

func splitMapTypeParams(s string) []string {
	var parts []string
	depth := 0
	parenDepth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '<':
			if i+1 < len(s) && s[i+1] == '=' {
			} else {
				depth++
			}
		case '>':
			if depth > 0 && (i == 0 || s[i-1] != '=') {
				depth--
			}
		case '(':
			parenDepth++
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}
		case ',':
			if depth == 0 && parenDepth == 0 {
				parts = append(parts, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	parts = append(parts, strings.TrimSpace(s[start:]))
	return parts
}

func getInlineObjectTypeAlias(fieldDef *core.MaxiFieldDef, schema *core.MaxiSchema) string {
	if fieldDef == nil {
		return ""
	}
	t := strings.TrimSpace(fieldDef.TypeExpr)

	t = strings.TrimSuffix(t, "[]")

	if strings.HasPrefix(t, "map<") {
		vt := getMapValueType(fieldDef)
		if vt == "" {
			return ""
		}
		return schema.ResolveTypeAlias(strings.TrimSpace(vt))
	}

	primitives := map[string]bool{
		"str": true, "int": true, "decimal": true,
		"bool": true, "bytes": true, "map": true,
	}
	if primitives[t] || t == "" {
		return ""
	}
	return schema.ResolveTypeAlias(t)
}

func parseEnumValues(typeExpr string) []string {
	m := enumParseRe.FindStringSubmatch(strings.TrimSpace(typeExpr))
	if m == nil {
		return nil
	}
	var out []string
	for _, v := range strings.Split(m[1], ",") {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if colonIdx := strings.Index(v, ":"); colonIdx >= 0 {
			v = v[colonIdx+1:]
		}
		out = append(out, v)
	}
	return out
}

func parseEnumAliases(typeExpr string) map[string]string {
	return ParseEnumAliases(typeExpr)
}

func ParseEnumAliases(typeExpr string) map[string]string {
	m := enumParseRe.FindStringSubmatch(strings.TrimSpace(typeExpr))
	if m == nil {
		return nil
	}
	result := make(map[string]string)
	hasAliases := false
	for _, v := range strings.Split(m[1], ",") {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if colonIdx := strings.Index(v, ":"); colonIdx >= 0 {
			alias := v[:colonIdx]
			value := v[colonIdx+1:]
			result[alias] = value
			hasAliases = true
		}
	}
	if !hasAliases {
		return nil
	}
	return result
}

func validateEnumAliases(typeExpr string, lineNum int) error {
	m := enumParseRe.FindStringSubmatch(strings.TrimSpace(typeExpr))
	if m == nil {
		return nil
	}

	type entry struct{ alias, value string }
	var entries []entry
	aliases := make(map[string]bool)
	values := make(map[string]bool)

	for _, v := range strings.Split(m[1], ",") {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		var alias, value string
		if colonIdx := strings.Index(v, ":"); colonIdx >= 0 {
			alias = v[:colonIdx]
			value = v[colonIdx+1:]
		} else {
			alias = v
			value = v
		}

		if aliases[alias] {
			return core.NewErrorAt(core.ErrEnumAliasError,
				fmt.Sprintf("duplicate enum alias '%s'", alias), lineNum, 0)
		}
		aliases[alias] = true

		if values[value] {
			return core.NewErrorAt(core.ErrEnumAliasError,
				fmt.Sprintf("duplicate enum value '%s'", value), lineNum, 0)
		}
		values[value] = true

		entries = append(entries, entry{alias, value})
	}

	for _, e := range entries {
		if e.alias != e.value && values[e.alias] {
			return core.NewErrorAt(core.ErrEnumAliasError,
				fmt.Sprintf("enum alias '%s' conflicts with the full value of another entry", e.alias),
				lineNum, 0)
		}
	}
	return nil
}

func parseTypeExprConstraints(typeExpr string) (string, []core.ParsedConstraint) {
	t := strings.TrimSpace(typeExpr)
	openIdx := strings.Index(t, "(")
	if openIdx == -1 || t[len(t)-1] != ')' {
		return t, nil
	}
	baseType := strings.TrimSpace(t[:openIdx])
	inner := t[openIdx+1 : len(t)-1]

	var constraints []core.ParsedConstraint
	parts := strings.Split(inner, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		for _, op := range []string{">=", "<=", ">", "<"} {
			if strings.HasPrefix(part, op) {
				valStr := strings.TrimSpace(part[len(op):])
				if f, err := strconv.ParseFloat(valStr, 64); err == nil {
					constraints = append(constraints, core.ParsedConstraint{
						Type:     core.ConstraintComparison,
						Operator: op,
						Value:    f,
					})
					break
				}
			}
		}
	}
	return baseType, constraints
}

func splitOnTopLevelCommasInContainer(s string) []string {
	var parts []string
	var cur strings.Builder
	inStr := false
	escape := false
	depth := 0

	for i := 0; i < len(s); i++ {
		ch := s[i]
		if escape {
			cur.WriteByte(ch)
			escape = false
			continue
		}
		if inStr {
			if ch == '\\' {
				cur.WriteByte(ch)
				escape = true
				continue
			}
			if ch == '"' {
				inStr = false
			}
			cur.WriteByte(ch)
			continue
		}
		switch ch {
		case '"':
			inStr = true
			cur.WriteByte(ch)
			continue
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				parts = append(parts, cur.String())
				cur.Reset()
				continue
			}
		}
		cur.WriteByte(ch)
	}
	if cur.Len() > 0 {
		parts = append(parts, cur.String())
	}
	return parts
}

func isAliasStart(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '_'
}

func isAliasChar(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
		(c >= '0' && c <= '9') || c == '-' || c == '_'
}

func findFieldIndex(td *core.MaxiTypeDef, name string) int {
	for i, f := range td.Fields {
		if f.Name == name {
			return i
		}
	}
	return -1
}

func anySlice(in []any) []any {
	if in == nil {
		return []any{}
	}
	return in
}
