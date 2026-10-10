package internalselect

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/VictoriaMetrics/VictoriaLogs/lib/logstorage"
)

// TestRequestHandlerRequiresPOST checks that the /internal/select/* and /internal/delete/*
// endpoints answer 405 to every method except POST.
//
// See https://github.com/VictoriaMetrics/VictoriaTraces/issues/225
func TestRequestHandlerRequiresPOST(t *testing.T) {
	Init()
	defer Stop()

	f := func(method, path string) {
		t.Helper()

		r := httptest.NewRequest(method, path, nil)
		w := httptest.NewRecorder()
		RequestHandler(context.Background(), w, r)

		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("unexpected status code for %s %s; got %d; want %d; response: %q",
				method, path, w.Code, http.StatusMethodNotAllowed, w.Body.String())
		}
	}

	f(http.MethodGet, "/internal/select/query")
	f(http.MethodGet, "/internal/select/query_profile")
	f(http.MethodGet, "/internal/select/field_names")
	f(http.MethodGet, "/internal/delete/run_task?filter=*")

	f(http.MethodDelete, "/internal/select/query")
	f(http.MethodDelete, "/internal/select/field_names")
	f(http.MethodDelete, "/internal/delete/run_task?filter=*")
}

// TestRequestHandlerPostPassesTheMethodCheck checks that a POST request reaches the args
// parsing, so the method check above rejects nothing which vtselect sends.
//
// vtselect always sends POST here, see getResponseBodyForPathAndArgs in app/vtstorage/netselect.
func TestRequestHandlerPostPassesTheMethodCheck(t *testing.T) {
	Init()
	defer Stop()

	args := url.Values{}
	args.Set("filter", "*")
	r := httptest.NewRequest(http.MethodPost, "/internal/delete/run_task", strings.NewReader(args.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	// the request carries no version arg, so it must fail on the protocol version check
	// rather than on the method check.
	RequestHandler(context.Background(), w, r)

	if w.Code == http.StatusMethodNotAllowed {
		t.Fatalf("a POST request must not be rejected by the method check; response: %q", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "unexpected protocol version") {
		t.Fatalf("unexpected response for a POST request; got %q; want the protocol version error", w.Body.String())
	}
}

// TestMarshalQueryStatsBlockKeepsV5Format checks that the query stats block sent by /internal/select/query
// doesn't contain the query profiling counters, so the v5 protocol stays compatible with older vtselect nodes.
func TestMarshalQueryStatsBlockKeepsV5Format(t *testing.T) {
	q, err := logstorage.ParseQuery("*")
	if err != nil {
		t.Fatalf("cannot parse query: %s", err)
	}

	var qs logstorage.QueryStats
	qs.EnableDetailedProfiling()
	qs.RowsFound = 10
	qs.PartsSelected = 3
	qctx := logstorage.NewQueryContext(context.Background(), &qs, nil, q, false, nil)

	getColumnNames := func(data []byte) []string {
		t.Helper()

		var db logstorage.DataBlock
		tail, _, err := db.UnmarshalInplace(data, nil)
		if err != nil {
			t.Fatalf("cannot unmarshal query stats block: %s", err)
		}
		if len(tail) != 0 {
			t.Fatalf("unexpected tail left after unmarshaling query stats block: %d bytes", len(tail))
		}
		var names []string
		for _, c := range db.GetColumns(false) {
			names = append(names, c.Name)
		}
		return names
	}

	namesExpected := []string{
		"BytesReadColumnsHeaders",
		"BytesReadColumnsHeaderIndexes",
		"BytesReadBloomFilters",
		"BytesReadValues",
		"BytesReadTimestamps",
		"BytesReadBlockHeaders",
		"BytesReadTotal",
		"BlocksProcessed",
		"RowsProcessed",
		"RowsFound",
		"ValuesRead",
		"TimestampsRead",
		"BytesProcessedUncompressedValues",
		"QueryDurationNsecs",
	}
	names := getColumnNames(marshalQueryStatsBlock(nil, qctx))
	if !slices.Equal(names, namesExpected) {
		t.Fatalf("unexpected columns in v5 query stats block\ngot\n%q\nwant\n%q", names, namesExpected)
	}

	// The query profile protocol must include the data-layout counters.
	names = getColumnNames(marshalQueryStatsBlockWithSelectionCounters(nil, qctx))
	if !slices.Contains(names, "PartsSelected") || !slices.Contains(names, "BlocksScheduled") {
		t.Fatalf("missing data-layout counters in query profile stats block: %q", names)
	}
}

// TestQueryProfileSnapshotDataBlockRoundtrip checks that query profiles survive the transfer
// via /internal/select/query_profile.
func TestQueryProfileSnapshotDataBlockRoundtrip(t *testing.T) {
	src := logstorage.QueryProfileSnapshot{
		Queries: []logstorage.QueryProfileQuery{{
			ID:        "query-1",
			Stage:     "span_fetch",
			Scope:     "remote",
			Query:     "*",
			Completed: true,
			Operators: []logstorage.QueryProfileOperator{{
				Name:       "scan",
				OutputRows: 42,
			}},
		}},
		Error: "some error",
	}
	db, err := src.CreateDataBlock()
	if err != nil {
		t.Fatalf("cannot create data block: %s", err)
	}
	data := db.Marshal(nil)

	var dbDst logstorage.DataBlock
	if _, _, err := dbDst.UnmarshalInplace(data, nil); err != nil {
		t.Fatalf("cannot unmarshal data block: %s", err)
	}
	dst, err := logstorage.QueryProfileSnapshotFromDataBlock(&dbDst)
	if err != nil {
		t.Fatalf("cannot decode query profile: %s", err)
	}
	if len(dst.Queries) != 1 || dst.Queries[0].Stage != "span_fetch" || dst.Queries[0].Operators[0].OutputRows != 42 || dst.Error != "some error" {
		t.Fatalf("unexpected query profile after roundtrip: %+v", dst)
	}
	if dst.NodeScanStats == nil {
		t.Fatalf("NodeScanStats must be non-nil in order to be marshaled as JSON array")
	}
}
