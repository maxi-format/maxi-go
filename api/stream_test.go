package api_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/maxi-format/maxi-go/api"
	"github.com/maxi-format/maxi-go/core"
)

func collectRecords(t *testing.T, s *api.MaxiStreamResult) []*core.MaxiRecord {
	t.Helper()
	var recs []*core.MaxiRecord
	for rec := range s.Records() {
		recs = append(recs, rec)
	}
	return recs
}

func collectRecordsWithError(s *api.MaxiStreamResult) ([]*core.MaxiRecord, error) {
	var recs []*core.MaxiRecord
	for rec, err := range s.RecordsWithError() {
		if err != nil {
			return recs, err
		}
		recs = append(recs, rec)
	}
	return recs, nil
}

func TestStream_SchemaAvailableBeforeIteration(t *testing.T) {
	input := "U:User(id:int|name|email)\n###\nU(1|Julie|julie@maxi.org)\nU(2|Matt|matt@maxi.org)"

	s, err := api.StreamMaxi(input)
	if err != nil {
		t.Fatalf("StreamMaxi error: %v", err)
	}

	if !s.Schema.HasType("U") {
		t.Error("expected schema to have type U")
	}

	recs := collectRecords(t, s)
	if len(recs) != 2 {
		t.Fatalf("expected 2 records, got %d", len(recs))
	}
	if recs[0].Values[0] != int64(1) && recs[0].Values[0] != 1 {
		t.Errorf("record[0].id: want 1, got %v", recs[0].Values[0])
	}
	if recs[0].Values[1] != "Julie" {
		t.Errorf("record[0].name: want Julie, got %v", recs[0].Values[1])
	}
	if recs[1].Values[1] != "Matt" {
		t.Errorf("record[1].name: want Matt, got %v", recs[1].Values[1])
	}
}

func TestStream_RecordsMethod(t *testing.T) {
	input := "U:User(id:int|name)\n###\nU(1|Alice)\nU(2|Bob)\nU(3|Charlie)"

	s, err := api.StreamMaxi(input)
	if err != nil {
		t.Fatal(err)
	}

	var names []string
	for rec := range s.Records() {
		names = append(names, rec.Values[1].(string))
	}
	if len(names) != 3 || names[0] != "Alice" || names[2] != "Charlie" {
		t.Errorf("unexpected names: %v", names)
	}
}

func TestStream_RecordsWithErrorMethod(t *testing.T) {
	input := "U:User(id:int|name)\n###\nU(1|Alice)\nU(2|Bob)"

	s, err := api.StreamMaxi(input)
	if err != nil {
		t.Fatal(err)
	}

	recs, err := collectRecordsWithError(s)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("expected 2 records, got %d", len(recs))
	}
}

func TestStream_SchemaErrorReturnedEagerly(t *testing.T) {
	input := "@version:2.0.0\nU:User(id:int)\n###\nU(1)"
	_, err := api.StreamMaxi(input)
	if err == nil {
		t.Fatal("expected schema error, got nil")
	}
	var me *core.MaxiError
	if !errors.As(err, &me) {
		t.Fatalf("expected *core.MaxiError, got %T", err)
	}
	if me.Code != core.ErrUnsupportedVersion {
		t.Errorf("expected ErrUnsupportedVersion, got %s", me.Code)
	}
}

func TestStream_RecordErrorDuringIteration(t *testing.T) {
	input := "U:User(id:int|name)\n###\nU(hello|Julie)"

	opts := core.DefaultParseOptions()
	opts.AllowTypeCoercion = core.TypeCoercionError

	s, err := api.StreamMaxi(input, opts)
	if err != nil {
		t.Fatal(err)
	}

	_, iterErr := collectRecordsWithError(s)
	if iterErr == nil {
		t.Fatal("expected type coercion error during iteration")
	}
	var me *core.MaxiError
	if !errors.As(iterErr, &me) {
		t.Fatalf("expected *MaxiError, got %T", iterErr)
	}
	if me.Code != core.ErrTypeMismatch {
		t.Errorf("expected ErrTypeMismatch, got %s", me.Code)
	}
}

func TestStream_EmptyRecordsSection(t *testing.T) {
	input := "U:User(id:int|name)\n###"

	s, err := api.StreamMaxi(input)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Schema.HasType("U") {
		t.Error("expected schema to have type U")
	}
	recs := collectRecords(t, s)
	if len(recs) != 0 {
		t.Errorf("expected 0 records, got %d", len(recs))
	}
}

func TestStream_SchemaOnlyNoSeparator(t *testing.T) {
	input := "U:User(id:int|name)"

	s, err := api.StreamMaxi(input)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Schema.HasType("U") {
		t.Error("expected schema to have type U")
	}
	recs := collectRecords(t, s)
	if len(recs) != 0 {
		t.Errorf("expected 0 records, got %d", len(recs))
	}
}

func TestStream_EarlyBreak(t *testing.T) {
	input := "U:User(id:int|name)\n###\nU(1|Alice)\nU(2|Bob)\nU(3|Charlie)\nU(4|Diana)"

	s, err := api.StreamMaxi(input)
	if err != nil {
		t.Fatal(err)
	}

	var collected []*core.MaxiRecord
	for rec := range s.Records() {
		collected = append(collected, rec)
		if len(collected) == 2 {
			break
		}
	}
	if len(collected) != 2 {
		t.Fatalf("expected 2 records after break, got %d", len(collected))
	}
	if collected[0].Values[1] != "Alice" {
		t.Errorf("first: want Alice, got %v", collected[0].Values[1])
	}
	if collected[1].Values[1] != "Bob" {
		t.Errorf("second: want Bob, got %v", collected[1].Values[1])
	}
}

func TestStream_WarningsAccumulated(t *testing.T) {
	input := "U:User(id:int|name)\n###\nU(hello|Julie)"

	s, err := api.StreamMaxi(input)
	if err != nil {
		t.Fatal(err)
	}

	for range s.Records() {
	}

	if len(s.Warnings()) == 0 {
		t.Error("expected at least one warning")
	}
}

func TestStream_ImportedSchema(t *testing.T) {
	input := "@schema:users.mxs\n###\nU(1|Julie)\nU(2|Matt)"

	opts := core.DefaultParseOptions()
	opts.LoadSchema = func(path string) (string, error) {
		if path != "users.mxs" {
			t.Errorf("unexpected schema path: %s", path)
		}
		return "U:User(id:int|name)", nil
	}

	s, err := api.StreamMaxi(input, opts)
	if err != nil {
		t.Fatalf("StreamMaxi error: %v", err)
	}
	if !s.Schema.HasType("U") {
		t.Error("expected schema to have type U after import")
	}

	recs := collectRecords(t, s)
	if len(recs) != 2 {
		t.Fatalf("expected 2 records, got %d", len(recs))
	}
}

func TestStream_UnknownTypeErrorDuringIteration(t *testing.T) {
	input := "U:User(id:int|name)\n###\nX(1|oops)"

	opts := core.DefaultParseOptions()
	opts.AllowUnknownTypes = core.UnknownTypesError

	s, err := api.StreamMaxi(input, opts)
	if err != nil {
		t.Fatal(err)
	}

	_, iterErr := collectRecordsWithError(s)
	if iterErr == nil {
		t.Fatal("expected unknown-type error during iteration")
	}
	var me *core.MaxiError
	if !errors.As(iterErr, &me) {
		t.Fatalf("expected *MaxiError, got %T", iterErr)
	}
	if me.Code != core.ErrUnknownType {
		t.Errorf("expected ErrUnknownType, got %s", me.Code)
	}
}

func TestStream_MultilineRecords(t *testing.T) {
	input := strings.Join([]string{
		"U:User(id:int|name|email)",
		"###",
		"U(",
		"  1|",
		"  Julie|",
		"  julie@test.com",
		")",
		"U(2|Matt|matt@test.com)",
	}, "\n")

	s, err := api.StreamMaxi(input)
	if err != nil {
		t.Fatal(err)
	}

	recs := collectRecords(t, s)
	if len(recs) != 2 {
		t.Fatalf("expected 2 records, got %d", len(recs))
	}
	if recs[0].Values[1] != "Julie" {
		t.Errorf("first name: want Julie, got %v", recs[0].Values[1])
	}
}

func TestStream_ParseOptionsForwarded(t *testing.T) {
	input := "U:User(id:int(!)|name(!)|email(!))\n###\nU(1|Julie)"

	opts := core.DefaultParseOptions()
	opts.AllowMissingFields = core.MissingFieldsError

	s, err := api.StreamMaxi(input, opts)
	if err != nil {
		t.Fatal(err)
	}

	_, iterErr := collectRecordsWithError(s)
	if iterErr == nil {
		t.Fatal("expected missing-field error during iteration")
	}
}

func TestStream_ResultTypeIsCorrect(t *testing.T) {
	s, err := api.StreamMaxi("U:User(id:int)\n###\nU(1)")
	if err != nil {
		t.Fatal(err)
	}
	if s.Schema == nil {
		t.Error("Schema should not be nil")
	}
	if s.Warnings() == nil {
		t.Error("Warnings should not be nil")
	}
}
