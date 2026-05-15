package api_test

// Conformance runner for the shared maxi-testdata test suite.
// Reads every sub-directory of ../maxi-testdata/testdata/, parses in.maxi
// with the parserOptions from test.json, and validates the result against
// expected.json.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/maxi-format/maxi-go/api"
	"github.com/maxi-format/maxi-go/core"
)

const testdataDir = "../maxi-testdata/testdata"

type testMeta struct {
	ID          string            `json:"id"`
	Title       string            `json:"title"`
	Category    string            `json:"category"`
	Tags        []string          `json:"tags"`
	ParserOptions map[string]any  `json:"parserOptions"`
}

type expectedResult struct {
	Success   bool                        `json:"success"`
	ErrorCode string                      `json:"error_code"`
	Records   []expectedRecord            `json:"records"`
	Objects   map[string]map[string]any   `json:"objects"`
	RecordValidations []recordValidation  `json:"record_validations"`
	ObjectValidations []objectValidation  `json:"object_validations"`
}

type expectedRecord struct {
	Type  string         `json:"type"`
	Value map[string]any `json:"value"`
}

type recordValidation struct {
	Description string `json:"description"`
	Path        string `json:"path"`
	ExpectedValue any   `json:"expected_value"`
}

type objectValidation struct {
	Description string `json:"description"`
	Path        string `json:"path"`
	ExpectedValue any   `json:"expected_value"`
}

func TestConformance(t *testing.T) {
	abs, err := filepath.Abs(testdataDir)
	if err != nil {
		t.Skipf("testdata not found: %v", err)
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		t.Skipf("cannot read testdata dir %s: %v", abs, err)
	}

	passed, failed, skipped := 0, 0, 0

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		caseDir := filepath.Join(abs, e.Name())
		t.Run(e.Name(), func(t *testing.T) {
			result, skip := runConformanceCase(t, caseDir)
			switch result {
			case "pass":
				passed++
			case "fail":
				failed++
			case "skip":
				skipped++
				_ = skip
			}
		})
	}

	t.Logf("Conformance: %d passed, %d failed, %d skipped", passed, failed, skipped)
}

func runConformanceCase(t *testing.T, caseDir string) (string, string) {
	t.Helper()

	metaBytes, err := os.ReadFile(filepath.Join(caseDir, "test.json"))
	if err != nil {
		t.Skipf("no test.json: %v", err)
		return "skip", "no test.json"
	}
	var meta testMeta
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		t.Fatalf("bad test.json: %v", err)
	}

	for _, tag := range meta.Tags {
		if tag == "url-schema" {
			t.Skipf("skipping url-schema test %s", meta.ID)
			return "skip", "url-schema"
		}
	}

	inputBytes, err := os.ReadFile(filepath.Join(caseDir, "in.maxi"))
	if err != nil {
		t.Skipf("no in.maxi: %v", err)
		return "skip", "no in.maxi"
	}
	input := string(inputBytes)

	expBytes, err := os.ReadFile(filepath.Join(caseDir, "expected.json"))
	if err != nil {
		t.Skipf("no expected.json: %v", err)
		return "skip", "no expected.json"
	}
	var expected expectedResult
	if err := json.Unmarshal(expBytes, &expected); err != nil {
		t.Fatalf("bad expected.json: %v", err)
	}

	opts := buildParseOptions(meta.ParserOptions, caseDir)

	parseResult, parseErr := api.ParseMaxi(input, opts)

	if !expected.Success {
		if parseErr == nil {
			t.Errorf("[%s] %s: expected error %s but parse succeeded", meta.ID, meta.Title, expected.ErrorCode)
			return "fail", ""
		}
		me, ok := parseErr.(*core.MaxiError)
		if !ok {
			t.Errorf("[%s] %s: expected *MaxiError, got %T: %v", meta.ID, meta.Title, parseErr, parseErr)
			return "fail", ""
		}
		if expected.ErrorCode != "" && me.Code != expected.ErrorCode {
			t.Errorf("[%s] %s: expected error code %s, got %s (%s)",
				meta.ID, meta.Title, expected.ErrorCode, me.Code, me.Message)
			return "fail", ""
		}
		return "pass", ""
	}

	if parseErr != nil {
		t.Errorf("[%s] %s: unexpected error: %v", meta.ID, meta.Title, parseErr)
		return "fail", ""
	}

	for _, rv := range expected.RecordValidations {
		checkPath(t, meta.ID, meta.Title, parseResult, rv.Path, rv.ExpectedValue, rv.Description)
	}

	for _, ov := range expected.ObjectValidations {
		checkPath(t, meta.ID, meta.Title, parseResult, ov.Path, ov.ExpectedValue, ov.Description)
	}

	return "pass", ""
}

func checkPath(t *testing.T, id, title string, result *core.MaxiParseResult, path string, expected any, desc string) {
	t.Helper()
	got := resolvePath(result, path)

	if !jsonEqual(got, expected) {
		t.Errorf("[%s] %s: %s\n  path:     %s\n  expected: %v (%T)\n  got:      %v (%T)",
			id, title, desc, path, expected, expected, got, got)
	}
}

func resolvePath(result *core.MaxiParseResult, path string) any {
	parts := strings.Split(strings.TrimPrefix(path, "#/"), "/")
	if len(parts) == 0 {
		return nil
	}

	switch parts[0] {
	case "records":
		return resolveRecordsPath(result, parts[1:])
	case "objects":
		return resolveObjectsPath(result, parts[1:])
	}
	return nil
}

func resolveRecordsPath(result *core.MaxiParseResult, parts []string) any {
	if len(parts) == 0 {
		return nil
	}
	idx := parseInt(parts[0])
	if idx < 0 || idx >= len(result.Records) {
		return nil
	}
	rec := result.Records[idx]
	td := result.Schema.GetType(rec.Alias)

	obj := recordToMap(rec, td)
	if obj == nil {
		return nil
	}

	if len(parts) == 1 {
		return obj
	}
	return resolveInMap(obj, parts[1:])
}

func resolveObjectsPath(result *core.MaxiParseResult, parts []string) any {
	if len(parts) == 0 || result.ObjectRegistry == nil {
		return nil
	}
	reg, ok := result.ObjectRegistry.(map[string]map[string]map[string]any)
	if !ok {
		return nil
	}

	typeName := parts[0]
	alias := result.Schema.ResolveTypeAlias(typeName)
	if alias == "" {
		alias = typeName
	}

	typeReg := reg[alias]
	if typeReg == nil {
		typeReg = reg[typeName]
		if typeReg == nil {
			return nil
		}
	}
	if len(parts) == 1 {
		out := make(map[string]any, len(typeReg))
		for k, v := range typeReg {
			out[k] = v
		}
		return out
	}

	idStr := parts[1]
	obj := typeReg[idStr]
	if obj == nil {
		return nil
	}
	if len(parts) == 2 {
		return obj
	}
	return resolveObjectValue(result, reg, alias, obj, parts[2:])
}

func resolveObjectValue(result *core.MaxiParseResult, reg map[string]map[string]map[string]any, typeAlias string, obj map[string]any, parts []string) any {
	if len(parts) == 0 {
		return obj
	}
	key := parts[0]
	v, ok := obj[key]
	if !ok {
		return nil
	}
	if len(parts) == 1 {
		return v
	}

	fieldTypeAlias := getFieldTypeAlias(result, typeAlias, key)
	return resolveDeep(result, reg, fieldTypeAlias, v, parts[1:])
}

func resolveDeep(result *core.MaxiParseResult, reg map[string]map[string]map[string]any, typeAlias string, v any, parts []string) any {
	if len(parts) == 0 || v == nil {
		return v
	}
	key := parts[0]

	switch vt := v.(type) {
	case map[string]any:
		sub, ok := vt[key]
		if !ok {
			return nil
		}
		subFieldAlias := getFieldTypeAlias(result, typeAlias, key)
		return resolveDeep(result, reg, subFieldAlias, sub, parts[1:])

	case []any:
		idx := parseInt(key)
		if idx < 0 || idx >= len(vt) {
			return nil
		}
		elem := vt[idx]
		if len(parts) == 1 {
			return elem
		}
		switch et := elem.(type) {
		case map[string]any:
			return resolveDeep(result, reg, typeAlias, et, parts[1:])
		default:
			if typeAlias != "" {
				idKey := fmt.Sprintf("%v", et)
				if refReg := reg[typeAlias]; refReg != nil {
					if refObj := refReg[idKey]; refObj != nil {
						return resolveDeep(result, reg, typeAlias, refObj, parts[1:])
					}
				}
			}
			return nil
		}

	default:
		if typeAlias != "" {
			idKey := fmt.Sprintf("%v", vt)
			if refReg := reg[typeAlias]; refReg != nil {
				if refObj := refReg[idKey]; refObj != nil {
					return resolveDeep(result, reg, typeAlias, refObj, parts)
				}
			}
		}
		return nil
	}
}


func getFieldTypeAlias(result *core.MaxiParseResult, typeAlias string, fieldName string) string {
	td := result.Schema.GetType(typeAlias)
	if td == nil {
		alias := result.Schema.ResolveTypeAlias(typeAlias)
		if alias != "" {
			td = result.Schema.GetType(alias)
		}
	}
	if td == nil {
		return ""
	}
	for _, f := range td.Fields {
		if f.Name == fieldName {
			ref := f.TypeExpr
			if ref == "" {
				return ""
			}
			for strings.HasSuffix(ref, "[]") {
				ref = ref[:len(ref)-2]
			}
			return result.Schema.ResolveTypeAlias(ref)
		}
	}
	return ""
}

func resolveInMap(m map[string]any, parts []string) any {
	if len(parts) == 0 {
		return m
	}
	key := parts[0]
	v, ok := m[key]
	if !ok {
		return nil
	}
	if len(parts) == 1 {
		return v
	}
	return resolveValue(v, parts[1:])
}

func resolveValue(v any, parts []string) any {
	if len(parts) == 0 || v == nil {
		return v
	}
	key := parts[0]
	switch vt := v.(type) {
	case map[string]any:
		sub, ok := vt[key]
		if !ok {
			return nil
		}
		return resolveValue(sub, parts[1:])
	case []any:
		idx := parseInt(key)
		if idx < 0 || idx >= len(vt) {
			return nil
		}
		return resolveValue(vt[idx], parts[1:])
	}
	return nil
}

func recordToMap(rec *core.MaxiRecord, td *core.MaxiTypeDef) map[string]any {
	m := make(map[string]any)
	m["type"] = rec.Alias

	if td != nil {
		if td.Name != "" {
			m["type"] = td.Name
		}
		inner := make(map[string]any, len(td.Fields))
		for i, f := range td.Fields {
			fieldKey := f.Name
			if f.Annotation == "hex" || f.Annotation == "base64" {
				fieldKey = f.Name + "_" + f.Annotation
			}
			if i < len(rec.Values) {
				inner[fieldKey] = rec.Values[i]
			} else {
				inner[fieldKey] = nil
			}
		}
		for k, v := range inner {
			m[k] = v
		}
		m["value"] = inner
	} else {
		vals := make([]any, len(rec.Values))
		copy(vals, rec.Values)
		m["value"] = map[string]any{"values": vals}
	}
	return m
}

func buildParseOptions(po map[string]any, caseDir string) core.ParseOptions {
	opts := core.DefaultParseOptions()

	if po != nil {
		if v, ok := po["allowAdditionalFields"].(string); ok {
			opts.AllowAdditionalFields = core.AdditionalFieldsMode(v)
		}
		if v, ok := po["allowMissingFields"].(string); ok {
			opts.AllowMissingFields = core.MissingFieldsMode(v)
		}
		if v, ok := po["allowTypeCoercion"].(string); ok {
			opts.AllowTypeCoercion = core.TypeCoercionMode(v)
		}
		if v, ok := po["allowConstraintViolations"].(string); ok {
			opts.AllowConstraintViolations = core.ConstraintViolationsMode(v)
		}
		if v, ok := po["allowForwardReferences"].(bool); ok {
			opts.AllowForwardReferences = v
		}
		if v, ok := po["allowUnknownTypes"].(string); ok {
			opts.AllowUnknownTypes = core.UnknownTypesMode(v)
		}
	}

	capturedDir := caseDir
	opts.LoadSchema = func(pathOrURL string) (string, error) {
		p := filepath.Join(capturedDir, pathOrURL)
		b, err := os.ReadFile(p)
		if err != nil {
			return "", fmt.Errorf("schema file not found: %s (tried: %s): %w", pathOrURL, p, err)
		}
		return string(b), nil
	}

	return opts
}

func parseInt(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return -1
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func jsonEqual(a, b any) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	af := toJSONNumber(a)
	bf := toJSONNumber(b)
	if af != nil && bf != nil {
		return *af == *bf
	}

	aSlice, aIsSlice := toAnySlice(a)
	bSlice, bIsSlice := toAnySlice(b)
	if aIsSlice && bIsSlice {
		if len(aSlice) != len(bSlice) {
			return false
		}
		for i := range aSlice {
			if !jsonEqual(aSlice[i], bSlice[i]) {
				return false
			}
		}
		return true
	}

	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	return string(aj) == string(bj)
}

func toJSONNumber(v any) *float64 {
	switch n := v.(type) {
	case float64:
		return &n
	case float32:
		f := float64(n)
		return &f
	case int:
		f := float64(n)
		return &f
	case int64:
		f := float64(n)
		return &f
	case int32:
		f := float64(n)
		return &f
	case string:
		if f, err := strconv.ParseFloat(n, 64); err == nil {
			return &f
		}
	}
	return nil
}

func toAnySlice(v any) ([]any, bool) {
	if s, ok := v.([]any); ok {
		return s, true
	}
	return nil, false
}

