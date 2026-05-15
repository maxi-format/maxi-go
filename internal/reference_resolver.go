package internal

import (
	"fmt"
	"strings"

	"github.com/maxi-format/maxi-go/core"
)

var nonRefTypes = map[string]bool{
	"int": true, "decimal": true, "float": true,
	"str": true, "bool": true, "bytes": true,
}

func getReferencedTypeAlias(typeExpr string, schema *core.MaxiSchema) string {
	if typeExpr == "" {
		return ""
	}
	t := strings.TrimSpace(typeExpr)

	for strings.HasSuffix(t, "[]") {
		t = t[:len(t)-2]
		t = strings.TrimRight(t, " \t")
	}

	if nonRefTypes[t] {
		return ""
	}
	if t == "map" || strings.HasPrefix(t, "map<") {
		return ""
	}
	if strings.HasPrefix(t, "enum") {
		return ""
	}

	return schema.ResolveTypeAlias(t)
}

type ObjectRegistry = map[string]map[string]map[string]any

func BuildObjectRegistry(result *core.MaxiParseResult) ObjectRegistry {
	reg := make(map[string]map[string]map[string]any)

	for _, record := range result.Records {
		td := result.Schema.GetType(record.Alias)
		if td == nil {
			continue
		}
		idF := td.IDField()
		if idF == nil {
			continue
		}
		idIdx := findFieldIdx(td, idF.Name)
		if idIdx < 0 || idIdx >= len(record.Values) {
			continue
		}
		idVal := record.Values[idIdx]
		if idVal == nil {
			continue
		}
		idKey := anyToString(idVal)

		if reg[record.Alias] == nil {
			reg[record.Alias] = make(map[string]map[string]any)
		}
		obj := make(map[string]any, len(td.Fields))
		for i, f := range td.Fields {
			var v any
			if i < len(record.Values) {
				v = record.Values[i]
			}
			if m, ok := v.(map[string]any); ok {
				refAlias := getReferencedTypeAlias(f.TypeExpr, result.Schema)
				if refAlias != "" {
					refTD := result.Schema.GetType(refAlias)
					if refTD != nil && refTD.IDField() != nil {
						if idV := m[refTD.IDField().Name]; idV != nil {
							v = idV
						}
					}
				}
			}
			if arr, ok := v.([]any); ok {
				refBaseAlias := getArrayElemTypeAlias(f.TypeExpr, result.Schema)
				if refBaseAlias != "" {
					refTD := result.Schema.GetType(refBaseAlias)
					if refTD != nil && refTD.IDField() != nil {
						replaced := make([]any, len(arr))
						for ai, elem := range arr {
							if em, ok := elem.(map[string]any); ok {
								if idV := em[refTD.IDField().Name]; idV != nil {
									replaced[ai] = idV
									continue
								}
							}
							replaced[ai] = elem
						}
						v = replaced
					}
				}
			}
			fieldKey := annotatedFieldName(f)
			obj[fieldKey] = v
		}
		reg[record.Alias][idKey] = obj
		if td.Name != "" && td.Name != record.Alias {
			if reg[td.Name] == nil {
				reg[td.Name] = make(map[string]map[string]any)
			}
			reg[td.Name][idKey] = obj
		}
	}

	for _, record := range result.Records {
		td := result.Schema.GetType(record.Alias)
		if td == nil {
			continue
		}
		for i, f := range td.Fields {
			if i >= len(record.Values) {
				break
			}
			v := record.Values[i]
			if v == nil {
				continue
			}

			if m, ok := v.(map[string]any); ok {
				refAlias := getReferencedTypeAlias(f.TypeExpr, result.Schema)
				if refAlias == "" {
					continue
				}
				refTD := result.Schema.GetType(refAlias)
				if refTD == nil {
					continue
				}
				refIDField := refTD.IDField()
				if refIDField == nil {
					continue
				}
				refIDVal := m[refIDField.Name]
				if refIDVal == nil {
					continue
				}
				idKey := anyToString(refIDVal)

				registerObjectEntry(reg, refAlias, refTD, idKey, m)
				continue
			}

			if arr, ok := v.([]any); ok {
				refBaseAlias := getArrayElemTypeAlias(f.TypeExpr, result.Schema)
				if refBaseAlias == "" {
					continue
				}
				refTD := result.Schema.GetType(refBaseAlias)
				if refTD == nil {
					continue
				}
				refIDField := refTD.IDField()
				if refIDField == nil {
					continue
				}
				for _, elem := range arr {
					m, ok := elem.(map[string]any)
					if !ok {
						continue
					}
					refIDVal := m[refIDField.Name]
					if refIDVal == nil {
						continue
					}
					idKey := anyToString(refIDVal)
					registerObjectEntry(reg, refBaseAlias, refTD, idKey, m)
				}
			}
		}
	}

	return reg
}
func registerObjectEntry(reg map[string]map[string]map[string]any, alias string, td *core.MaxiTypeDef, idKey string, m map[string]any) {
	if reg[alias] == nil {
		reg[alias] = make(map[string]map[string]any)
	}
	if _, exists := reg[alias][idKey]; !exists {
		reg[alias][idKey] = m
	}
	if td.Name != "" && td.Name != alias {
		if reg[td.Name] == nil {
			reg[td.Name] = make(map[string]map[string]any)
		}
		if _, exists := reg[td.Name][idKey]; !exists {
			reg[td.Name][idKey] = m
		}
	}
}

func annotatedFieldName(f *core.MaxiFieldDef) string {
	switch f.Annotation {
	case "hex":
		return f.Name + "_hex"
	case "base64":
		return f.Name + "_base64"
	default:
		return f.Name
	}
}

func ValidateReferences(result *core.MaxiParseResult, reg ObjectRegistry, opts core.ParseOptions) error {
	lineIndex := make(map[string]map[string]int)
	for _, record := range result.Records {
		td := result.Schema.GetType(record.Alias)
		if td == nil {
			continue
		}
		idField := td.IDField()
		if idField == nil {
			continue
		}
		idx := td.IDFieldIndex()
		if idx < 0 || idx >= len(record.Values) {
			continue
		}
		idKey := anyToString(record.Values[idx])
		if lineIndex[record.Alias] == nil {
			lineIndex[record.Alias] = make(map[string]int)
		}
		lineIndex[record.Alias][idKey] = record.LineNumber
	}

	for _, record := range result.Records {
		td := result.Schema.GetType(record.Alias)
		if td == nil {
			continue
		}
		for i, f := range td.Fields {
			if i >= len(record.Values) {
				break
			}
			v := record.Values[i]
			if v == nil {
				continue
			}
			switch v.(type) {
			case map[string]any, []any:
				continue
			}

			refAlias := getReferencedTypeAlias(f.TypeExpr, result.Schema)
			if refAlias == "" {
				continue
			}

			idKey := anyToString(v)
			typeReg := reg[refAlias]
			if typeReg == nil || typeReg[idKey] == nil {
				msg := fmt.Sprintf(
					"unresolved reference: field '%s' in '%s' references %s id '%v', but no such object found",
					f.Name, record.Alias, refAlias, v)

				if !opts.AllowForwardReferences {
					return core.NewErrorAt(core.ErrUnresolvedReference, msg, record.LineNumber, 0)
				}
				result.AddWarning(core.ErrUnresolvedReference, msg, record.LineNumber)
			} else if !opts.AllowForwardReferences {
				if refLineIndex, ok := lineIndex[refAlias]; ok {
					if refLine, ok := refLineIndex[idKey]; ok {
						if refLine > record.LineNumber {
							msg := fmt.Sprintf(
								"forward reference: field '%s' in '%s' (line %d) references %s id '%v' which is defined at line %d",
								f.Name, record.Alias, record.LineNumber, refAlias, v, refLine)
							return core.NewErrorAt(core.ErrUnresolvedReference, msg, record.LineNumber, 0)
						}
					}
				}
			}
		}
	}
	return nil
}

func HasReferenceFields(schema *core.MaxiSchema) bool {
	return hasReferenceFields(schema)
}

func hasReferenceFields(schema *core.MaxiSchema) bool {
	for _, td := range schema.Types() {
		for _, f := range td.Fields {
			if getReferencedTypeAlias(f.TypeExpr, schema) != "" {
				return true
			}
		}
	}
	return false
}

func findFieldIdx(td *core.MaxiTypeDef, name string) int {
	return findFieldIndex(td, name)
}

func getArrayElemTypeAlias(typeExpr string, schema *core.MaxiSchema) string {
	if !strings.HasSuffix(typeExpr, "[]") {
		return ""
	}
	inner := strings.TrimSuffix(typeExpr, "[]")
	inner = strings.TrimRight(inner, " \t")
	return getReferencedTypeAlias(inner, schema)
}
