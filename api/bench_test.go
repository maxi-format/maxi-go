package api_test

// Performance benchmarks — measures MAXI parse/dump vs JSON parse/dump
// for 100,000 records using the standard User schema used by all language benchmarks.
//
// Run with:
//   go test -bench=. -benchtime=5s -count=3 -benchmem
//
// Schema used:
//   U:User(id:int|name|email:str@email|role:enum[admin,user]|createdAt:str@datetime|logins:int|active:bool)

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/maxi-format/maxi-go/api"
	"github.com/maxi-format/maxi-go/core"
)

const benchN = 100_000

const benchSchema = `U:User(id:int|name|email:str@email|role:enum[admin,user]|createdAt:str@datetime|logins:int|active:bool)`

var (
	benchMaxiPayload string
	benchJSONPayload string
	benchJSONObjects []map[string]any
	benchDumpTypes   []*core.MaxiTypeDef
	benchDumpRows    []map[string]any
)

var names = []string{
	"Alice", "Bob", "Charlie", "Diana", "Ethan", "Fiona", "George", "Hannah",
	"Ivan", "Julia", "Kevin", "Laura", "Michael", "Nina", "Oscar", "Paula",
}
var domains = []string{"example.com", "test.org", "maxi.io", "bench.dev"}
var roles = []string{"admin", "user"}

func randName(r *rand.Rand) string { return names[r.Intn(len(names))] }
func randEmail(r *rand.Rand, id int) string {
	return fmt.Sprintf("user%d@%s", id, domains[r.Intn(len(domains))])
}
func randDate(r *rand.Rand) string {
	y := 2020 + r.Intn(5)
	m := 1 + r.Intn(12)
	d := 1 + r.Intn(28)
	return fmt.Sprintf("%04d-%02d-%02dT%02d:%02d:%02dZ",
		y, m, d, r.Intn(24), r.Intn(60), r.Intn(60))
}

func generatePayloads() {
	r := rand.New(rand.NewSource(42))

	var sb strings.Builder
	sb.WriteString(benchSchema)
	sb.WriteString("\n###\n")

	jsonObjs := make([]map[string]any, benchN)
	dumpRows := make([]map[string]any, benchN)

	for i := 1; i <= benchN; i++ {
		name := randName(r)
		email := randEmail(r, i)
		role := roles[r.Intn(len(roles))]
		created := randDate(r)
		logins := r.Intn(500)
		active := r.Intn(2) == 1

		activeVal := "0"
		if active {
			activeVal = "1"
		}

		sb.WriteString(fmt.Sprintf("U(%d|%s|%s|%s|%s|%d|%s)\n",
			i, name, email, role, created, logins, activeVal))

		jsonObjs[i-1] = map[string]any{
			"id": i, "name": name, "email": email, "role": role,
			"createdAt": created, "logins": logins, "active": active,
		}
		dumpRows[i-1] = map[string]any{
			"id": i, "name": name, "email": email, "role": role,
			"createdAt": created, "logins": logins, "active": active,
		}
	}

	benchMaxiPayload = sb.String()

	jb, _ := json.Marshal(jsonObjs)
	benchJSONPayload = string(jb)
	benchJSONObjects = jsonObjs
	benchDumpRows = dumpRows

	benchDumpTypes = []*core.MaxiTypeDef{{
		Alias: "U", Name: "User",
		Fields: []*core.MaxiFieldDef{
			{Name: "id", TypeExpr: "int"},
			{Name: "name"},
			{Name: "email", Annotation: "email"},
			{Name: "role", TypeExpr: "enum[admin,user]"},
			{Name: "createdAt", Annotation: "datetime"},
			{Name: "logins", TypeExpr: "int"},
			{Name: "active", TypeExpr: "bool"},
		},
	}}
}

func init() {
	generatePayloads()
}

func TestBenchPayloadSizes(t *testing.T) {
	maxiKB := float64(len(benchMaxiPayload)) / 1024
	jsonKB := float64(len(benchJSONPayload)) / 1024
	t.Logf("MAXI payload: %.0f KB (%d bytes)", maxiKB, len(benchMaxiPayload))
	t.Logf("JSON payload: %.0f KB (%d bytes)", jsonKB, len(benchJSONPayload))
	t.Logf("MAXI is %.1f%% the size of JSON (%.1f%% smaller)",
		maxiKB/jsonKB*100, (1-maxiKB/jsonKB)*100)
}

func BenchmarkMaxiParse(b *testing.B) {
	input := benchMaxiPayload
	b.SetBytes(int64(len(input)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := api.ParseMaxi(input)
		if err != nil {
			b.Fatal(err)
		}
		_ = result
	}
	recordThroughput(b, benchN)
}

func BenchmarkMaxiDump(b *testing.B) {
	data := map[string][]map[string]any{"U": benchDumpRows}
	opts := api.DumpOptions{
		DefaultAlias:      "U",
		Types:             benchDumpTypes,
		IncludeTypes:      true,
		CollectReferences: false,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out, err := api.DumpMaxi(data, opts)
		if err != nil {
			b.Fatal(err)
		}
		b.SetBytes(int64(len(out)))
		_ = out
	}
	recordThroughput(b, benchN)
}

func BenchmarkJSONParse(b *testing.B) {
	input := []byte(benchJSONPayload)
	b.SetBytes(int64(len(input)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var out []map[string]any
		if err := json.Unmarshal(input, &out); err != nil {
			b.Fatal(err)
		}
		_ = out
	}
	recordThroughput(b, benchN)
}

func BenchmarkJSONDump(b *testing.B) {
	data := benchJSONObjects
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out, err := json.Marshal(data)
		if err != nil {
			b.Fatal(err)
		}
		b.SetBytes(int64(len(out)))
		_ = out
	}
	recordThroughput(b, benchN)
}

func recordThroughput(b *testing.B, recordsPerOp int) {
	b.Helper()
	elapsed := time.Duration(b.Elapsed())
	if elapsed == 0 {
		return
	}
	recsPerSec := float64(b.N) * float64(recordsPerOp) / elapsed.Seconds()
	b.ReportMetric(recsPerSec, "rec/s")
}
