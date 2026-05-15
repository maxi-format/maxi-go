package internal

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/maxi-format/maxi-go/core"
)

var primitiveTypes = map[string]bool{
	"str": true, "int": true, "decimal": true,
	"float": true, "bool": true, "bytes": true, "map": true,
}

type SchemaParser struct {
	schemaText   string
	result       *core.MaxiParseResult
	options      core.ParseOptions
	filename     string
	loadingStack map[string]bool
	localAliases map[string]bool
	isImported   bool
}

func NewSchemaParser(schemaText string, result *core.MaxiParseResult, opts core.ParseOptions, filename string) *SchemaParser {
	return &SchemaParser{
		schemaText:   schemaText,
		result:       result,
		options:      opts,
		filename:     filename,
		loadingStack: make(map[string]bool),
		localAliases: make(map[string]bool),
	}
}

func (p *SchemaParser) Parse() error {
	if strings.TrimSpace(p.schemaText) == "" {
		return nil
	}

	lines := splitLines(p.schemaText)
	for i := 0; i < len(lines); {
		line := strings.TrimSpace(lines[i])

		if line == "" || strings.HasPrefix(line, "#") {
			i++
			continue
		}

		if strings.HasPrefix(line, "@") {
			if err := p.parseDirective(line, i+1); err != nil {
				return err
			}
			i++
			continue
		}

		nextI, err := p.parseTypeDefinition(lines, i)
		if err != nil {
			return err
		}
		if nextI == i {
			i++
		} else {
			i = nextI + 1
		}
	}

	if err := p.resolveInheritance(); err != nil {
		return err
	}
	if err := p.validateSchemaConstraints(); err != nil {
		return err
	}
	if err := p.validateDefaultValues(); err != nil {
		return err
	}
	if !p.isImported {
		p.buildNameIndex()
		if err := p.validateFieldTypeReferences(); err != nil {
			return err
		}
	} else {
		p.buildNameIndex()
	}
	return nil
}

var directiveRe = regexp.MustCompile(`^@([a-zA-Z_][a-zA-Z0-9_-]*):(.+)$`)
var versionRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

func (p *SchemaParser) parseDirective(line string, lineNum int) error {
	m := directiveRe.FindStringSubmatch(line)
	if m == nil {
		return core.NewErrorAt(core.ErrInvalidSyntax,
			fmt.Sprintf("invalid directive syntax: %s", line), lineNum, 0)
	}
	name := m[1]
	value := strings.TrimSpace(m[2])

	switch name {
	case "version":
		return p.parseVersionDirective(value, lineNum)
	case "schema":
		return p.parseSchemaDirective(value, lineNum)
	default:
		p.result.AddWarning(core.ErrUnknownDirective,
			fmt.Sprintf("unknown directive '@%s' ignored", name), lineNum)
	}
	return nil
}

func (p *SchemaParser) parseVersionDirective(value string, lineNum int) error {
	if !versionRe.MatchString(value) {
		return core.NewErrorAt(core.ErrInvalidSyntax,
			fmt.Sprintf("invalid version format: %s", value), lineNum, 0)
	}
	if value != "1.0.0" {
		return core.NewErrorAt(core.ErrUnsupportedVersion,
			fmt.Sprintf("unsupported version: %s. Parser supports v1.0.0", value), lineNum, 0)
	}
	p.result.Schema.Version = value
	return nil
}

func (p *SchemaParser) parseSchemaDirective(pathOrURL string, lineNum int) error {
	if p.loadingStack[pathOrURL] {
		return nil
	}
	p.result.Schema.Imports = append(p.result.Schema.Imports, pathOrURL)
	return p.loadExternalSchema(pathOrURL, lineNum)
}

var (
	looksLikeAliasParen    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*\s*\(`)
	looksLikeExplicitType  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*\s*:\s*[A-Za-z_][A-Za-z0-9_-]*\s*(?:<[^>]+>)?\s*\(`)
	looksLikeInheritance   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*\s*<[^>]+>\s*\(`)
	headerRe               = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_-]*)(?::([A-Za-z_][A-Za-z0-9_-]*))?(?:<\s*([^>]+?)\s*>)?\s*$`)
	intDefaultRe           = regexp.MustCompile(`^-?\d+$`)
)

func (p *SchemaParser) parseTypeDefinition(lines []string, startIndex int) (int, error) {
	trimmed := strings.TrimSpace(lines[startIndex])

	isExplicit := looksLikeExplicitType.MatchString(trimmed)
	isInherit := looksLikeInheritance.MatchString(trimmed)
	isAlias := looksLikeAliasParen.MatchString(trimmed)

	if !isExplicit && !isInherit {
		if isAlias {
			parenPos := strings.Index(trimmed, "(")
			after := strings.TrimLeft(trimmed[parenPos+1:], " \t")
			if len(after) > 0 && (after[0] == '~' || (after[0] >= '0' && after[0] <= '9') || after[0] == '-') {
				return startIndex, nil
			}
		} else {
			return startIndex, nil
		}
	}

	var sb strings.Builder
	inStr := false
	escape := false
	sawOpen := false
	depth := 0
	endIndex := startIndex

	for i := startIndex; i < len(lines); i++ {
		cur := lines[i]
		sb.WriteString(cur)
		sb.WriteByte('\n')

		for _, ch := range cur {
			if escape {
				escape = false
				continue
			}
			if inStr {
				if ch == '\\' {
					escape = true
					continue
				}
				if ch == '"' {
					inStr = false
				}
				continue
			}
			switch ch {
			case '"':
				inStr = true
			case '(':
				sawOpen = true
				depth++
			case ')':
				if !sawOpen {
					return 0, core.NewErrorAt(core.ErrInvalidSyntax,
						"unmatched closing parenthesis in type definition", i+1, 0)
				}
				depth--
				if depth < 0 {
					return 0, core.NewErrorAt(core.ErrInvalidSyntax,
						"unmatched closing parenthesis in type definition", i+1, 0)
				}
			}
		}

		endIndex = i
		if sawOpen && depth == 0 {
			break
		}
	}

	if !sawOpen {
		return startIndex, nil
	}
	if depth != 0 {
		return 0, core.NewErrorAt(core.ErrConstraintSyntax,
			"unclosed parenthesis in type definition (possible malformed constraint)",
			startIndex+1, 0)
	}

	if err := p.parseCompleteTypeDefinition(sb.String(), startIndex+1); err != nil {
		return 0, err
	}
	return endIndex, nil
}

func (p *SchemaParser) parseCompleteTypeDefinition(def string, lineNum int) error {
	trimmed := strings.TrimSpace(def)

	openIdx := strings.Index(trimmed, "(")
	if openIdx == -1 {
		return core.NewErrorAt(core.ErrInvalidSyntax,
			fmt.Sprintf("invalid type definition syntax: %s", trimmed), lineNum, 0)
	}

	closeIdx := p.findMatchingParen(trimmed, openIdx)
	if closeIdx == -1 {
		return core.NewErrorAt(core.ErrInvalidSyntax,
			fmt.Sprintf("invalid type definition syntax: %s", trimmed), lineNum, 0)
	}

	tail := strings.TrimSpace(trimmed[closeIdx+1:])
	if tail != "" {
		return core.NewErrorAt(core.ErrInvalidSyntax,
			fmt.Sprintf("invalid type definition syntax: %s", trimmed), lineNum, 0)
	}

	header := strings.TrimSpace(trimmed[:openIdx])
	fieldsStr := strings.TrimSpace(trimmed[openIdx+1 : closeIdx])

	m := headerRe.FindStringSubmatch(header)
	if m == nil {
		return core.NewErrorAt(core.ErrInvalidSyntax,
			fmt.Sprintf("invalid type definition header: %s", header), lineNum, 0)
	}

	alias := m[1]
	typeName := m[2]
	parentsStr := m[3]

	if typeName != "" && !isLetter(rune(typeName[0])) {
		return core.NewErrorAt(core.ErrUnknownType,
			fmt.Sprintf("invalid type name '%s': type names must start with a letter [a-zA-Z]", typeName),
			lineNum, 0)
	}

	alreadyLocal := p.localAliases[alias]
	if alreadyLocal {
		return core.NewErrorAt(core.ErrDuplicateType,
			fmt.Sprintf("duplicate type alias '%s'", alias), lineNum, 0)
	}

	var parents []string
	if parentsStr != "" {
		for _, pp := range strings.Split(parentsStr, ",") {
			pp = strings.TrimSpace(pp)
			if pp != "" {
				parents = append(parents, pp)
			}
		}
	}

	td := &core.MaxiTypeDef{
		Alias:   alias,
		Name:    typeName,
		Parents: parents,
	}

	if fieldsStr != "" {
		fields, err := p.parseFieldList(fieldsStr, lineNum)
		if err != nil {
			return err
		}
		for _, f := range fields {
			td.AddField(f)
		}
	}

	if err := p.result.Schema.AddType(td); err != nil {
		if p.isImported || !alreadyLocal {
			p.result.Schema.SetType(td)
		} else {
			return &core.MaxiError{
				Code:     err.Code,
				Message:  err.Message,
				Line:     lineNum,
				Filename: p.filename,
			}
		}
	}
	p.localAliases[alias] = true
	return nil
}

func (p *SchemaParser) parseFieldList(fieldsStr string, lineNum int) ([]*core.MaxiFieldDef, error) {
	norm := strings.Join(strings.Fields(fieldsStr), " ")
	parts := splitTopLevel(norm, '|')

	var fields []*core.MaxiFieldDef
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		f, err := p.parseField(part, lineNum)
		if err != nil {
			return nil, err
		}
		fields = append(fields, f)
	}
	return fields, nil
}

func (p *SchemaParser) parseField(fieldStr string, lineNum int) (*core.MaxiFieldDef, error) {
	rem := strings.TrimSpace(fieldStr)
	var constraints []core.ParsedConstraint
	var elemConstraints []core.ParsedConstraint
	var defaultValue any

	colonIdx := findTopLevelChar(rem, ':')
	namePart := rem
	restPart := ""
	if colonIdx != -1 {
		namePart = strings.TrimSpace(rem[:colonIdx])
		restPart = strings.TrimSpace(rem[colonIdx+1:])
	}

	if restPart != "" {
		if tr := extractTrailingGroup(restPart, '(', ')'); tr != nil {
			var err error
			constraints, err = p.parseConstraints(tr.inner, lineNum)
			if err != nil {
				return nil, err
			}
			restPart = strings.TrimSpace(tr.before)

			if strings.HasSuffix(restPart, "[]") {
				withoutBrackets := strings.TrimSuffix(restPart, "[]")
				withoutBrackets = strings.TrimRight(withoutBrackets, " \t")
				if innerTr := extractTrailingGroup(withoutBrackets, '(', ')'); innerTr != nil {
					var err error
					elemConstraints, err = p.parseConstraints(innerTr.inner, lineNum)
					if err != nil {
						return nil, err
					}
					restPart = strings.TrimSpace(innerTr.before) + "[]"
				}
			}
		}
	}

	if len(constraints) == 0 {
		if tr := extractTrailingGroup(namePart, '(', ')'); tr != nil {
			var err error
			constraints, err = p.parseConstraints(tr.inner, lineNum)
			if err != nil {
				return nil, err
			}
			namePart = strings.TrimSpace(tr.before)
		}
	}

	if eqIdx := findTopLevelChar(namePart, '='); eqIdx != -1 {
		defaultValue = parseDefaultValue(strings.TrimSpace(namePart[eqIdx+1:]))
		namePart = strings.TrimSpace(namePart[:eqIdx])
		if len(constraints) == 0 {
			if tr := extractTrailingGroup(namePart, '(', ')'); tr != nil {
				var err error
				constraints, err = p.parseConstraints(tr.inner, lineNum)
				if err != nil {
					return nil, err
				}
				namePart = strings.TrimSpace(tr.before)
			}
		}
	} else if restPart != "" {
		if eqIdx := findTopLevelChar(restPart, '='); eqIdx != -1 {
			defaultValue = parseDefaultValue(strings.TrimSpace(restPart[eqIdx+1:]))
			restPart = strings.TrimSpace(restPart[:eqIdx])
		}
	}

	var typeExpr, annotation string
	if restPart != "" {
		if atIdx := findTopLevelChar(restPart, '@'); atIdx != -1 {
			typeExpr = strings.TrimSpace(restPart[:atIdx])
			annotation = strings.TrimSpace(restPart[atIdx+1:])
		} else {
			typeExpr = strings.TrimSpace(restPart)
		}
	}

	fd := &core.MaxiFieldDef{
		Name:       namePart,
		TypeExpr:   typeExpr,
		Annotation: annotation,
	}
	if len(constraints) > 0 {
		fd.Constraints = constraints
	}
	if len(elemConstraints) > 0 {
		fd.ElementConstraints = elemConstraints
	}
	if defaultValue != nil {
		fd.DefaultValue = defaultValue
	}
	if strings.HasPrefix(typeExpr, "enum") {
		if err := validateEnumAliases(typeExpr, lineNum); err != nil {
			return nil, err
		}
		fd.EnumValues = parseEnumValues(typeExpr)
		fd.EnumAliases = parseEnumAliases(typeExpr)
	}
	return fd, nil
}

var (
	exactLengthRe   = regexp.MustCompile(`^=(\d+)$`)
	comparisonRe    = regexp.MustCompile(`^(>=|>|<=|<|=)\s*(.+)$`)
	decimalPrecRe   = regexp.MustCompile(`^(\d+:)?(\d+)?\.(\d+(?::\d+)?)?$`)
)

func (p *SchemaParser) parseConstraints(s string, lineNum int) ([]core.ParsedConstraint, error) {
	parts := splitConstraintParts(s)
	var out []core.ParsedConstraint

	for _, raw := range parts {
		t := strings.TrimSpace(raw)
		if t == "" {
			continue
		}

		switch t {
		case "!":
			out = append(out, core.ParsedConstraint{Type: core.ConstraintRequired})
			continue
		case "id":
			out = append(out, core.ParsedConstraint{Type: core.ConstraintID})
			continue
		}

		if m := exactLengthRe.FindStringSubmatch(t); m != nil {
			n, _ := strconv.Atoi(m[1])
			out = append(out, core.ParsedConstraint{Type: core.ConstraintExactLength, Value: n})
			continue
		}

		if m := comparisonRe.FindStringSubmatch(t); m != nil {
			op := m[1]
			valStr := strings.TrimSpace(m[2])
			var val any
			if v, err := strconv.ParseFloat(valStr, 64); err == nil {
				val = v
			} else {
				val = valStr
			}
			out = append(out, core.ParsedConstraint{Type: core.ConstraintComparison, Operator: op, Value: val})
			continue
		}

		if strings.HasPrefix(t, "pattern:") {
			pat := strings.TrimSpace(t[len("pattern:"):])
			if _, err := regexp.Compile(pat); err != nil {
				return nil, core.NewErrorAt(core.ErrConstraintSyntax,
					fmt.Sprintf("invalid regex pattern: %s", pat), lineNum, 0)
			}
			out = append(out, core.ParsedConstraint{Type: core.ConstraintPattern, Value: pat})
			continue
		}

		if strings.HasPrefix(t, "mime:") {
			mimeSpec := strings.TrimSpace(t[len("mime:"):])
			types, err := parseMimeSpec(mimeSpec)
			if err != nil {
				return nil, core.NewErrorAt(core.ErrConstraintSyntax,
					fmt.Sprintf("invalid mime constraint value: %s", mimeSpec), lineNum, 0)
			}
			out = append(out, core.ParsedConstraint{Type: core.ConstraintMime, Value: types})
			continue
		}

		if decimalPrecRe.MatchString(t) {
			out = append(out, parseDecimalPrecision(t))
			continue
		}

		return nil, core.NewErrorAt(core.ErrConstraintSyntax,
			fmt.Sprintf("unknown constraint: %s", t), lineNum, 0)
	}

	return out, nil
}

func parseDecimalPrecision(raw string) core.ParsedConstraint {
	dotIdx := strings.Index(raw, ".")
	intPart := raw[:dotIdx]
	fracPart := raw[dotIdx+1:]

	type nullable struct {
		v   int
		set bool
	}
	parseRange := func(s string) (min, max nullable) {
		if s == "" {
			return
		}
		if idx := strings.Index(s, ":"); idx >= 0 {
			a, b := s[:idx], s[idx+1:]
			if a != "" {
				if n, err := strconv.Atoi(a); err == nil {
					min = nullable{n, true}
				}
			}
			if b != "" {
				if n, err := strconv.Atoi(b); err == nil {
					max = nullable{n, true}
				}
			}
		} else {
			if n, err := strconv.Atoi(s); err == nil {
				max = nullable{n, true}
			}
		}
		return
	}

	iMin, iMax := parseRange(intPart)
	fMin, fMax := parseRange(fracPart)

	val := map[string]any{"raw": raw}
	if iMin.set {
		val["intMin"] = iMin.v
	}
	if iMax.set {
		val["intMax"] = iMax.v
	}
	if fMin.set {
		val["fracMin"] = fMin.v
	}
	if fMax.set {
		val["fracMax"] = fMax.v
	}

	return core.ParsedConstraint{Type: core.ConstraintDecimalPrecision, Value: val}
}

func (p *SchemaParser) resolveInheritance() error {
	visited := make(map[string]bool)
	visiting := make(map[string]bool)

	var resolve func(alias string) error
	resolve = func(alias string) error {
		if visited[alias] {
			return nil
		}
		if visiting[alias] {
			return core.NewError(core.ErrCircularInheritance,
				fmt.Sprintf("circular inheritance detected involving type '%s'", alias))
		}

		td := p.result.Schema.GetType(alias)
		if td == nil || td.InheritanceResolved {
			return nil
		}

		visiting[alias] = true

		var inherited []*core.MaxiFieldDef
		for _, parentAlias := range td.Parents {
			parent := p.result.Schema.GetType(parentAlias)
			if parent == nil {
				return core.NewError(core.ErrUndefinedParent,
					fmt.Sprintf("type '%s' inherits from '%s', but '%s' is not defined",
						alias, parentAlias, parentAlias))
			}
			if err := resolve(parentAlias); err != nil {
				return err
			}
			for _, f := range parent.Fields {
				if !containsField(inherited, f.Name) {
					inherited = append(inherited, f)
				}
			}
		}

		final := make([]*core.MaxiFieldDef, len(inherited))
		copy(final, inherited)
		for _, own := range td.Fields {
			found := false
			for i, f := range final {
				if f.Name == own.Name {
					final[i] = own
					found = true
					break
				}
			}
			if !found {
				final = append(final, own)
			}
		}
		td.Fields = final
		td.InheritanceResolved = true

		delete(visiting, alias)
		visited[alias] = true
		return nil
	}

	for _, td := range p.result.Schema.Types() {
		if err := resolve(td.Alias); err != nil {
			return err
		}
	}
	return nil
}

var annotationTypeMap = map[string][]string{
	"base64":   {"bytes"},
	"hex":      {"bytes"},
	"timestamp": {"int"},
	"date":     {"str"},
	"datetime": {"str"},
	"time":     {"str"},
	"email":    {"str"},
	"url":      {"str"},
	"uuid":     {"str"},
}

func (p *SchemaParser) validateSchemaConstraints() error {
	for _, td := range p.result.Schema.Types() {
		for _, f := range td.Fields {
			if err := p.validateAnnotationTypeCompat(f, td.Alias); err != nil {
				return err
			}
			if err := p.validateConstraintConflicts(f, td.Alias); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *SchemaParser) validateAnnotationTypeCompat(f *core.MaxiFieldDef, typeAlias string) error {
	if f.Annotation == "" {
		return nil
	}
	allowed, known := annotationTypeMap[f.Annotation]
	base := getBaseTypeName(f.TypeExpr)

	if !known {
		if base == "bytes" {
			return core.NewError(core.ErrUnsupportedBinaryFormat,
				fmt.Sprintf("unsupported binary format annotation '@%s' on bytes field '%s' in type '%s'. Supported: @base64, @hex",
					f.Annotation, f.Name, typeAlias))
		}
		return nil
	}

	if base == "" {
		return nil
	}
	for _, a := range allowed {
		if a == base {
			return nil
		}
	}
	return core.NewError(core.ErrInvalidConstraintValue,
		fmt.Sprintf("type annotation '@%s' cannot be applied to '%s' field '%s' in type '%s'",
			f.Annotation, base, f.Name, typeAlias))
}

func (p *SchemaParser) validateConstraintConflicts(f *core.MaxiFieldDef, typeAlias string) error {
	if len(f.Constraints) < 2 {
		return nil
	}
	var minGe, minGt, maxLe, maxLt *float64
	for _, c := range f.Constraints {
		if c.Type != core.ConstraintComparison {
			continue
		}
		v, ok := toFloat64(c.Value)
		if !ok {
			continue
		}
		switch c.Operator {
		case ">=":
			if minGe == nil || v > *minGe {
				minGe = &v
			}
		case ">":
			if minGt == nil || v > *minGt {
				minGt = &v
			}
		case "<=":
			if maxLe == nil || v < *maxLe {
				maxLe = &v
			}
		case "<":
			if maxLt == nil || v < *maxLt {
				maxLt = &v
			}
		}
	}
	effectiveMin := minGe
	if effectiveMin == nil && minGt != nil {
		v := *minGt + 1
		effectiveMin = &v
	}
	effectiveMax := maxLe
	if effectiveMax == nil && maxLt != nil {
		v := *maxLt - 1
		effectiveMax = &v
	}
	if effectiveMin != nil && effectiveMax != nil && *effectiveMin > *effectiveMax {
		return core.NewError(core.ErrInvalidConstraintValue,
			fmt.Sprintf("constraint conflict on field '%s' in type '%s': min > max", f.Name, typeAlias))
	}
	return nil
}

func (p *SchemaParser) validateDefaultValues() error {
	for _, td := range p.result.Schema.Types() {
		for _, f := range td.Fields {
			if f.DefaultValue == nil {
				continue
			}
			defStr := fmt.Sprintf("%v", f.DefaultValue)
			switch f.TypeExpr {
			case "int":
				if !intDefaultRe.MatchString(defStr) {
					return core.NewError(core.ErrInvalidDefaultValue,
						fmt.Sprintf("invalid default value '%v' for field '%s' of type 'int' in '%s'",
							f.DefaultValue, f.Name, td.Alias))
				}
			case "float", "decimal":
				if _, err := strconv.ParseFloat(defStr, 64); err != nil {
					return core.NewError(core.ErrInvalidDefaultValue,
						fmt.Sprintf("invalid default value '%v' for field '%s' of type '%s' in '%s'",
							f.DefaultValue, f.Name, f.TypeExpr, td.Alias))
				}
			case "bool":
				switch defStr {
				case "true", "false", "1", "0":
				default:
					return core.NewError(core.ErrInvalidDefaultValue,
						fmt.Sprintf("invalid default value '%v' for field '%s' of type 'bool' in '%s'",
							f.DefaultValue, f.Name, td.Alias))
				}
			}
		}
	}
	return nil
}

func (p *SchemaParser) validateFieldTypeReferences() error {
	for _, td := range p.result.Schema.Types() {
		for _, f := range td.Fields {
			ref := extractReferencedType(f.TypeExpr)
			if ref == "" {
				continue
			}
			if p.result.Schema.ResolveTypeAlias(ref) == "" {
				return core.NewError(core.ErrUnknownType,
					fmt.Sprintf("field '%s' in type '%s' references unknown type '%s'",
						f.Name, td.Alias, ref))
			}
		}
	}
	return nil
}

func (p *SchemaParser) buildNameIndex() {
	p.result.Schema.BuildNameIndex()
}

func (p *SchemaParser) loadExternalSchema(pathOrURL string, lineNum int) error {
	if p.options.LoadSchema != nil {
		return p.loadViaHook(pathOrURL, lineNum)
	}
	if strings.HasPrefix(pathOrURL, "http://") || strings.HasPrefix(pathOrURL, "https://") {
		return p.loadHTTP(pathOrURL, lineNum)
	}
	return p.loadFile(pathOrURL, lineNum)
}

func (p *SchemaParser) loadFile(path string, lineNum int) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return &core.MaxiError{
			Code: core.ErrSchemaLoad,
			Message: fmt.Sprintf("failed to load schema '%s': %s", path, err.Error()),
			Line: lineNum, Filename: p.filename, Cause: err,
		}
	}
	return p.parseImported(string(data), path)
}

func (p *SchemaParser) loadHTTP(url string, lineNum int) error {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return &core.MaxiError{
			Code: core.ErrSchemaLoad,
			Message: fmt.Sprintf("failed to load schema '%s': %s", url, err.Error()),
			Line: lineNum, Filename: p.filename, Cause: err,
		}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &core.MaxiError{
			Code: core.ErrSchemaLoad,
			Message: fmt.Sprintf("failed to read schema '%s': %s", url, err.Error()),
			Line: lineNum, Filename: p.filename, Cause: err,
		}
	}
	return p.parseImported(string(body), url)
}

func (p *SchemaParser) loadViaHook(pathOrURL string, lineNum int) error {
	content, err := p.options.LoadSchema(pathOrURL)
	if err != nil {
		return &core.MaxiError{
			Code: core.ErrSchemaLoad,
			Message: fmt.Sprintf("failed to load schema '%s': %s", pathOrURL, err.Error()),
			Line: lineNum, Filename: p.filename, Cause: err,
		}
	}
	return p.parseImported(content, pathOrURL)
}

func (p *SchemaParser) parseImported(content, srcPath string) error {
	child := &SchemaParser{
		schemaText:   content,
		result:       p.result,
		options:      p.options,
		filename:     srcPath,
		loadingStack: p.loadingStack,
		localAliases: make(map[string]bool),
		isImported:   true,
	}
	child.loadingStack[srcPath] = true
	defer func() { delete(child.loadingStack, srcPath) }()
	return child.Parse()
}

func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.Split(s, "\n")
}

func splitTopLevel(s string, delim byte) []string {
	var out []string
	var cur strings.Builder
	inStr := false
	escape := false
	paren, bracket, brace := 0, 0, 0

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
		case '(':
			paren++
		case ')':
			if paren > 0 {
				paren--
			}
		case '[':
			bracket++
		case ']':
			if bracket > 0 {
				bracket--
			}
		case '{':
			brace++
		case '}':
			if brace > 0 {
				brace--
			}
		}
		if ch == delim && paren == 0 && bracket == 0 && brace == 0 {
			out = append(out, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(ch)
	}
	out = append(out, cur.String())
	return out
}

func splitConstraintParts(s string) []string {
	return splitTopLevel(s, ',')
}

func findTopLevelChar(s string, ch byte) int {
	inStr := false
	escape := false
	paren, bracket, brace := 0, 0, 0

	for i := 0; i < len(s); i++ {
		c := s[i]
		if escape {
			escape = false
			continue
		}
		if inStr {
			if c == '\\' {
				escape = true
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
			continue
		case '(':
			paren++
		case ')':
			if paren > 0 {
				paren--
			}
		case '[':
			bracket++
		case ']':
			if bracket > 0 {
				bracket--
			}
		case '{':
			brace++
		case '}':
			if brace > 0 {
				brace--
			}
		}
		if c == ch && paren == 0 && bracket == 0 && brace == 0 {
			return i
		}
	}
	return -1
}

type group struct{ before, inner string }

func extractTrailingGroup(s string, openCh, closeCh byte) *group {
	trimmed := strings.TrimRight(s, " \t")
	if len(trimmed) == 0 || trimmed[len(trimmed)-1] != closeCh {
		return nil
	}
	closeIdx := len(trimmed) - 1
	depth := 1
	startIdx := -1
	inStr := false

	for i := closeIdx - 1; i >= 0; i-- {
		c := trimmed[i]
		if c == '"' {
			inStr = !inStr
			continue
		}
		if inStr {
			continue
		}
		if c == closeCh {
			depth++
		} else if c == openCh {
			depth--
			if depth == 0 {
				startIdx = i
				break
			}
		}
	}
	if startIdx == -1 {
		return nil
	}
	return &group{
		before: trimmed[:startIdx],
		inner:  trimmed[startIdx+1 : closeIdx],
	}
}

func (p *SchemaParser) findMatchingParen(s string, openIdx int) int {
	if openIdx < 0 || s[openIdx] != '(' {
		return -1
	}
	depth := 0
	inStr := false
	escape := false

	for i := openIdx; i < len(s); i++ {
		ch := s[i]
		if escape {
			escape = false
			continue
		}
		if inStr {
			if ch == '\\' {
				escape = true
				continue
			}
			if ch == '"' {
				inStr = false
			}
			continue
		}
		switch ch {
		case '"':
			inStr = true
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func parseDefaultValue(s string) any {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return unescapeString(s[1 : len(s)-1])
	}
	return s
}

func unescapeString(s string) string {
	s = strings.ReplaceAll(s, `\n`, "\n")
	s = strings.ReplaceAll(s, `\r`, "\r")
	s = strings.ReplaceAll(s, `\t`, "\t")
	s = strings.ReplaceAll(s, `\"`, `"`)
	s = strings.ReplaceAll(s, `\\`, `\`)
	return s
}

func parseMimeSpec(s string) ([]string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	if !strings.HasPrefix(s, "[") {
		item := s
		if len(item) >= 2 && item[0] == '"' && item[len(item)-1] == '"' {
			item = unescapeString(item[1 : len(item)-1])
		}
		return []string{strings.TrimSpace(item)}, nil
	}
	if !strings.HasSuffix(s, "]") {
		return nil, fmt.Errorf("invalid mime spec: %s", s)
	}
	content := strings.TrimSpace(s[1 : len(s)-1])
	if content == "" {
		return nil, nil
	}
	parts := splitTopLevel(content, ',')
	var out []string
	for _, p := range parts {
		item := strings.TrimSpace(p)
		if item == "" {
			continue
		}
		if len(item) >= 2 && item[0] == '"' && item[len(item)-1] == '"' {
			item = unescapeString(item[1 : len(item)-1])
		}
		out = append(out, strings.TrimSpace(item))
	}
	return out, nil
}

func getBaseTypeName(typeExpr string) string {
	if typeExpr == "" {
		return ""
	}
	t := typeExpr
	if strings.HasPrefix(t, "enum") {
		return "str"
	}
	for strings.HasSuffix(t, "[]") {
		t = strings.TrimSuffix(t, "[]")
		t = strings.TrimRight(t, " \t")
	}
	if tr := extractTrailingGroup(t, '(', ')'); tr != nil {
		t = strings.TrimSpace(tr.before)
	}
	return t
}

func extractReferencedType(typeExpr string) string {
	if typeExpr == "" {
		return ""
	}
	t := strings.TrimSpace(typeExpr)
	if strings.HasPrefix(t, "enum") {
		return ""
	}
	if strings.HasPrefix(t, "map<") && strings.HasSuffix(t, ">") {
		inside := t[4 : len(t)-1]
		lastComma := -1
		depth := 0
		parenDepth := 0
		for i := 0; i < len(inside); i++ {
			switch inside[i] {
			case '<':
				if i+1 < len(inside) && inside[i+1] == '=' {
				} else {
					depth++
				}
			case '>':
				if depth > 0 && (i == 0 || inside[i-1] != '=') {
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
					lastComma = i
				}
			}
		}
		valueType := inside
		if lastComma >= 0 {
			valueType = strings.TrimSpace(inside[lastComma+1:])
		}
		return extractReferencedType(valueType)
	}
	if t == "map" {
		return ""
	}
	for {
		if tr := extractTrailingGroup(t, '(', ')'); tr != nil {
			t = strings.TrimSpace(tr.before)
		} else {
			break
		}
	}
	for strings.HasSuffix(t, "[]") {
		t = strings.TrimSuffix(t, "[]")
		t = strings.TrimRight(t, " \t")
		for {
			if tr := extractTrailingGroup(t, '(', ')'); tr != nil {
				t = strings.TrimSpace(tr.before)
			} else {
				break
			}
		}
	}
	if t == "" || primitiveTypes[t] {
		return ""
	}
	return t
}

func containsField(fields []*core.MaxiFieldDef, name string) bool {
	for _, f := range fields {
		if f.Name == name {
			return true
		}
	}
	return false
}

func isLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}
