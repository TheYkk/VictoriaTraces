package tempo

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/VictoriaMetrics/VictoriaTraces/lib/traceql"
)

func TestGetProfileMode(t *testing.T) {
	f := func(query, modeExpected string, errExpected bool) {
		t.Helper()

		r := httptest.NewRequest("GET", "/select/tempo/api/profile?"+query, nil)
		mode, err := getProfileMode(r)
		if errExpected {
			if err == nil {
				t.Fatalf("expecting non-nil error for %q", query)
			}
			return
		}
		if err != nil {
			t.Fatalf("unexpected error for %q: %s", query, err)
		}
		if mode != modeExpected {
			t.Fatalf("unexpected mode for %q; got %q; want %q", query, mode, modeExpected)
		}
	}

	f("", "explain", false)
	f("mode=explain", "explain", false)
	f("mode=ANALYZE", "analyze", false)
	f("analyze=true", "analyze", false)
	f("analyze=1", "analyze", false)
	f("analyze=false", "explain", false)
	f("mode=explain&analyze=true", "explain", false)

	f("mode=foo", "", true)
	f("analyze=foo", "", true)
}

func TestBuildTraceSearchProfileStages(t *testing.T) {
	q, err := traceql.ParseQuery(`{resource.service.name="frontend" && duration > 1s}`)
	if err != nil {
		t.Fatalf("cannot parse TraceQL query: %s", err)
	}

	end := time.Now().Add(-time.Hour)
	start := end.Add(-time.Hour)
	stages, err := buildTraceSearchProfileStages(q, start, end, 20)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if len(stages) != 2 {
		t.Fatalf("unexpected number of stages; got %d; want 2", len(stages))
	}

	traceIDStage := stages[0]
	if traceIDStage.Name != "trace_id_search" || len(traceIDStage.DependsOn) != 0 {
		t.Fatalf("unexpected first stage: %+v", traceIDStage)
	}
	for _, s := range []string{`service.name`, `frontend`, `last by (_time) partition by (trace_id)`, `sort by (_time) desc limit 20`} {
		if !strings.Contains(traceIDStage.QueryTemplate, s) {
			t.Fatalf("trace_id_search query must contain %q; got %q", s, traceIDStage.QueryTemplate)
		}
	}
	if traceIDStage.Plan.OptimizedQuery == "" || traceIDStage.Plan.RemoteQuery == "" {
		t.Fatalf("missing static plan for trace_id_search: %+v", traceIDStage.Plan)
	}

	spanStage := stages[1]
	if spanStage.Name != "span_fetch" || len(spanStage.DependsOn) != 1 || spanStage.DependsOn[0] != "trace_id_search" {
		t.Fatalf("unexpected second stage: %+v", spanStage)
	}
	if !strings.Contains(spanStage.QueryTemplate, `trace_id:in(PROFILE_TRACE_ID)`) {
		t.Fatalf("span_fetch query must contain the trace_id placeholder; got %q", spanStage.QueryTemplate)
	}
	if spanStage.Plan.RemotePipeNames == nil || spanStage.Plan.LocalPipeNames == nil || spanStage.Plan.UnresolvedSubqueries == nil {
		t.Fatalf("static plan slices must be non-nil in order to be marshaled as JSON arrays: %+v", spanStage.Plan)
	}
}

// TestTraceSearchQueriesMatchSearchAPI verifies that the queries reported by the profile API
// are the same queries executed by /select/tempo/api/search.
func TestTraceSearchQueriesMatchSearchAPI(t *testing.T) {
	q, err := traceql.ParseQuery(`{span:name="GET /api"}`)
	if err != nil {
		t.Fatalf("cannot parse TraceQL query: %s", err)
	}
	now := time.Now()
	start := now.Add(-time.Hour)

	traceIDQuery, _, err := newTraceIDListQuery(q, now, 10)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if s := traceIDQuery.String(); !strings.Contains(s, `"span:name":="GET /api" | last by (_time) partition by (trace_id) | fields _time, trace_id | sort by (_time) desc limit 10`) {
		t.Fatalf("unexpected trace_id_search query; got %q", s)
	}

	spanQuery, err := newTraceSpansQuery(q, []string{"a", "b"}, now, start, now)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if s := spanQuery.String(); !strings.Contains(s, `trace_id:in(a,b)`) || !strings.Contains(s, `parent_span_id:=""`) {
		t.Fatalf("unexpected span_fetch query; got %q", s)
	}
}
