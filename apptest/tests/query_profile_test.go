package tests

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	at "github.com/VictoriaMetrics/VictoriaTraces/apptest"
	otelpb "github.com/VictoriaMetrics/VictoriaTraces/lib/protoparser/opentelemetry/pb"
)

type tempoProfileResponse struct {
	Mode   string `json:"mode"`
	Stages []struct {
		Name          string `json:"name"`
		QueryTemplate string `json:"query_template"`
	} `json:"stages"`
	Analysis *struct {
		TraceIDsFound int    `json:"trace_ids_found"`
		SpanRowsFound uint64 `json:"span_rows_found"`
	} `json:"analysis"`
	Execution *struct {
		Queries []struct {
			Stage     string `json:"stage"`
			Scope     string `json:"scope"`
			Completed bool   `json:"completed"`
			Node      *struct {
				Address string `json:"address"`
			} `json:"node"`
			Operators []struct {
				Name       string `json:"name"`
				OutputRows uint64 `json:"output_rows"`
			} `json:"operators"`
		} `json:"queries"`
		QueryStats struct {
			RowsFound          uint64 `json:"rows_found"`
			PartitionsSelected uint64 `json:"partitions_selected"`
			PartsSelected      uint64 `json:"parts_selected"`
			BlocksScheduled    uint64 `json:"blocks_scheduled"`
			BlocksProcessed    uint64 `json:"blocks_processed"`
		} `json:"query_stats"`
		NodeScanStats []struct {
			Node struct {
				Address string `json:"address"`
			} `json:"node"`
			Stage string `json:"stage"`
		} `json:"node_scan_stats"`
	} `json:"execution"`
	Error string `json:"error"`
}

// TestTempoProfileAPI verifies the /select/tempo/api/profile endpoint in single-node and cluster setups,
// and checks that it reports the same traces as /select/tempo/api/search.
func TestTempoProfileAPI(t *testing.T) {
	tc := at.NewTestCase(t)
	defer tc.Stop()

	storage, frontend, serviceName, spanTime := startProfileTestCluster(t, tc)

	start := spanTime.Add(-time.Hour)
	end := time.Now()
	traceQL := fmt.Sprintf(`{resource.service.name=%q}`, serviceName)
	cli := tc.Client()

	getProfile := func(t *testing.T, sut *at.Vtsingle, mode string, statusCodeExpected int) *tempoProfileResponse {
		t.Helper()

		uv := url.Values{
			"q":     {traceQL},
			"start": {strconv.FormatInt(start.Unix(), 10)},
			"end":   {strconv.FormatInt(end.Unix(), 10)},
			"mode":  {mode},
		}
		body, statusCode := cli.Get(t, "http://"+sut.HTTPAddr()+"/select/tempo/api/profile?"+uv.Encode())
		if statusCode != statusCodeExpected {
			t.Fatalf("unexpected status code for mode=%q; got %d; want %d; response: %s", mode, statusCode, statusCodeExpected, body)
		}
		if statusCode != http.StatusOK {
			return nil
		}
		var resp tempoProfileResponse
		if err := json.Unmarshal([]byte(body), &resp); err != nil {
			t.Fatalf("cannot unmarshal profile response: %s; response: %s", err, body)
		}
		return &resp
	}

	f := func(t *testing.T, sut *at.Vtsingle, isCluster bool) {
		// The ordinary search must keep working and return the expected traces.
		tc.Assert(&at.AssertOptions{
			Msg: "unexpected number of traces from /select/tempo/api/search",
			Got: func() any {
				return len(sut.TempoAPISearch(t, traceQL, start, end).Traces)
			},
			Want: 3,
		})

		// explain mode must not execute the query
		explain := getProfile(t, sut, "explain", http.StatusOK)
		if explain.Mode != "explain" || len(explain.Stages) != 2 || explain.Execution != nil || explain.Analysis != nil {
			t.Fatalf("unexpected explain response: %+v", explain)
		}
		if explain.Stages[0].Name != "trace_id_search" || explain.Stages[1].Name != "span_fetch" {
			t.Fatalf("unexpected stages: %+v", explain.Stages)
		}

		// analyze mode must execute the same workflow as /select/tempo/api/search
		analyze := getProfile(t, sut, "analyze", http.StatusOK)
		if analyze.Error != "" {
			t.Fatalf("unexpected error in analyze response: %s", analyze.Error)
		}
		if analyze.Analysis == nil || analyze.Analysis.TraceIDsFound != 3 || analyze.Analysis.SpanRowsFound != 3 {
			t.Fatalf("unexpected analysis: %+v", analyze.Analysis)
		}
		ex := analyze.Execution
		if ex == nil || len(ex.Queries) == 0 {
			t.Fatalf("missing execution profile: %+v", analyze)
		}
		if ex.QueryStats.RowsFound == 0 || ex.QueryStats.PartitionsSelected == 0 || ex.QueryStats.PartsSelected == 0 ||
			ex.QueryStats.BlocksScheduled == 0 || ex.QueryStats.BlocksProcessed == 0 {
			t.Fatalf("unexpected query stats: %+v", ex.QueryStats)
		}
		stages := map[string]bool{}
		remoteQueries := 0
		for _, q := range ex.Queries {
			if !q.Completed {
				t.Fatalf("query must be completed: %+v", q)
			}
			stages[q.Stage] = true
			if q.Scope == "remote" {
				remoteQueries++
				if q.Node == nil || q.Node.Address != storage.HTTPAddr() {
					t.Fatalf("unexpected node for remote query: %+v", q)
				}
			}
		}
		if !stages["trace_id_search"] || !stages["span_fetch"] {
			t.Fatalf("missing stages in execution profile: %+v", stages)
		}
		if isCluster {
			if remoteQueries == 0 {
				t.Fatalf("missing remote queries in cluster execution profile: %+v", ex.Queries)
			}
			if len(ex.NodeScanStats) == 0 || ex.NodeScanStats[0].Node.Address != storage.HTTPAddr() {
				t.Fatalf("unexpected node scan stats: %+v", ex.NodeScanStats)
			}
		} else if remoteQueries != 0 || len(ex.NodeScanStats) != 0 {
			t.Fatalf("unexpected remote stats in single-node execution profile: %+v", ex)
		}

		// invalid mode must be rejected
		getProfile(t, sut, "foo", http.StatusBadRequest)
	}

	t.Run("single", func(t *testing.T) {
		f(t, storage, false)
	})
	t.Run("cluster", func(t *testing.T) {
		f(t, frontend, true)
	})
}

// startProfileTestCluster starts a vtstorage node and a vtselect node, which queries it,
// and ingests 6 single-span traces into vtstorage. Every other trace belongs to the returned serviceName.
func startProfileTestCluster(t *testing.T, tc *at.TestCase) (storage, frontend *at.Vtsingle, serviceName string, spanTime time.Time) {
	t.Helper()

	storage = tc.MustStartVtsingle("vtstorage", []string{
		"-storageDataPath=" + tc.Dir() + "/vtstorage",
		"-retentionPeriod=100y",
	})
	frontend = tc.MustStartVtselect("vtselect", []string{
		"-storageNode=" + storage.HTTPAddr(),
	})

	serviceName = "testProfileService"
	otherServiceName := "testProfileOtherService"
	spanTime = time.Now().Add(-time.Hour)
	var resourceSpans []*otelpb.ResourceSpans
	for i := 0; i < 6; i++ {
		svc := serviceName
		if i%2 == 1 {
			svc = otherServiceName
		}
		resourceSpans = append(resourceSpans, &otelpb.ResourceSpans{
			Resource: otelpb.Resource{
				Attributes: []*otelpb.KeyValue{
					{Key: "service.name", Value: &otelpb.AnyValue{StringValue: &svc}},
				},
			},
			ScopeSpans: []*otelpb.ScopeSpans{
				{
					Spans: []*otelpb.Span{
						{
							TraceID:           fmt.Sprintf("5b8efff798038103d269b633813fc6%02d", i),
							SpanID:            fmt.Sprintf("eee19b7ec3c1b1%02d", i),
							Name:              "testProfileSpan",
							StartTimeUnixNano: uint64(spanTime.Add(time.Duration(i) * time.Second).UnixNano()),
							EndTimeUnixNano:   uint64(spanTime.Add(time.Duration(i)*time.Second + time.Millisecond).UnixNano()),
						},
					},
				},
			},
		})
	}
	storage.OTLPHTTPExportTraces(t, &otelpb.ExportTraceServiceRequest{ResourceSpans: resourceSpans}, at.QueryOpts{})
	storage.ForceFlush(t)

	return storage, frontend, serviceName, spanTime
}

type logsqlProfileResponse struct {
	Mode          string `json:"mode"`
	ExecutedQuery string `json:"executed_query"`
	Plan          struct {
		OptimizedQuery  string   `json:"optimized_query"`
		RemoteQuery     string   `json:"remote_query"`
		RemotePipeNames []string `json:"remote_pipe_names"`
		LocalPipeNames  []string `json:"local_pipe_names"`
	} `json:"plan"`
	Analysis *struct {
		RowsReturned uint64 `json:"rows_returned"`
	} `json:"analysis"`
	Execution *struct {
		Queries []struct {
			Stage     string `json:"stage"`
			Scope     string `json:"scope"`
			Query     string `json:"query"`
			Completed bool   `json:"completed"`
		} `json:"queries"`
		QueryStats struct {
			RowsFound          uint64 `json:"rows_found"`
			PartitionsSelected uint64 `json:"partitions_selected"`
			BlocksScheduled    uint64 `json:"blocks_scheduled"`
		} `json:"query_stats"`
		NodeScanStats []struct {
			Node struct {
				Address string `json:"address"`
			} `json:"node"`
		} `json:"node_scan_stats"`
	} `json:"execution"`
	Error string `json:"error"`
}

// TestLogsQLProfileAPI verifies the /select/logsql/profile endpoint in single-node and cluster setups,
// and checks that it returns the same number of rows as /select/logsql/query.
func TestLogsQLProfileAPI(t *testing.T) {
	tc := at.NewTestCase(t)
	defer tc.Stop()

	storage, frontend, serviceName, spanTime := startProfileTestCluster(t, tc)

	start := strconv.FormatInt(spanTime.Add(-time.Hour).Unix(), 10)
	end := strconv.FormatInt(time.Now().Unix(), 10)
	serviceFilter := fmt.Sprintf(`"resource_attr:service.name":=%q`, serviceName)
	cli := tc.Client()

	getProfile := func(t *testing.T, sut *at.Vtsingle, query, limit, mode string, statusCodeExpected int) *logsqlProfileResponse {
		t.Helper()

		uv := url.Values{
			"query": {query},
			"start": {start},
			"end":   {end},
			"limit": {limit},
			"mode":  {mode},
		}
		body, statusCode := cli.PostForm(t, "http://"+sut.HTTPAddr()+"/select/logsql/profile", uv)
		if statusCode != statusCodeExpected {
			t.Fatalf("unexpected status code for query=%q, mode=%q; got %d; want %d; response: %s", query, mode, statusCode, statusCodeExpected, body)
		}
		if statusCode != http.StatusOK {
			return nil
		}
		var resp logsqlProfileResponse
		if err := json.Unmarshal([]byte(body), &resp); err != nil {
			t.Fatalf("cannot unmarshal profile response: %s; response: %s", err, body)
		}
		if resp.Error != "" {
			t.Fatalf("unexpected error in profile response for query=%q: %s", query, resp.Error)
		}
		return &resp
	}

	checkAnalyze := func(t *testing.T, sut *at.Vtsingle, isCluster bool, query, limit string, rowsExpected uint64, minQueries int) {
		t.Helper()

		resp := getProfile(t, sut, query, limit, "analyze", http.StatusOK)
		if resp.Analysis == nil || resp.Analysis.RowsReturned != rowsExpected {
			t.Fatalf("unexpected analysis for query=%q; got %+v; want %d rows", query, resp.Analysis, rowsExpected)
		}

		// The regular query endpoint must return the same rows.
		rows := sut.LogsQLQuery(t, query, at.QueryOpts{Start: start, End: end, Limit: limit})
		if uint64(len(rows.LogLines)) != rowsExpected {
			t.Fatalf("unexpected number of rows from /select/logsql/query for query=%q; got %d; want %d", query, len(rows.LogLines), rowsExpected)
		}

		ex := resp.Execution
		if ex == nil {
			t.Fatalf("missing execution profile for query=%q", query)
		}
		if ex.QueryStats.RowsFound == 0 || ex.QueryStats.PartitionsSelected == 0 || ex.QueryStats.BlocksScheduled == 0 {
			t.Fatalf("unexpected query stats for query=%q: %+v", query, ex.QueryStats)
		}
		storageQueries := 0
		remoteQueries := 0
		for _, q := range ex.Queries {
			if !q.Completed || q.Stage != "logsql_query" {
				t.Fatalf("unexpected query in profile for query=%q: %+v", query, q)
			}
			switch q.Scope {
			case "remote":
				remoteQueries++
				storageQueries++
			case "local":
				storageQueries++
			}
		}
		if storageQueries < minQueries {
			t.Fatalf("unexpected number of profiled storage queries for query=%q; got %d; want at least %d; queries: %+v", query, storageQueries, minQueries, ex.Queries)
		}
		if isCluster {
			if remoteQueries != storageQueries || len(ex.NodeScanStats) == 0 || ex.NodeScanStats[0].Node.Address != storage.HTTPAddr() {
				t.Fatalf("unexpected cluster profile for query=%q: %+v", query, ex)
			}
		} else if remoteQueries != 0 || len(ex.NodeScanStats) != 0 {
			t.Fatalf("unexpected remote stats in single-node profile for query=%q: %+v", query, ex)
		}
	}

	f := func(t *testing.T, sut *at.Vtsingle, isCluster bool) {
		// explain mode must not execute the query
		explain := getProfile(t, sut, serviceFilter+" | stats count() hits", "", "explain", http.StatusOK)
		if explain.Mode != "explain" || explain.Analysis != nil || explain.Execution != nil {
			t.Fatalf("unexpected explain response: %+v", explain)
		}
		if explain.Plan.OptimizedQuery == "" || explain.Plan.RemoteQuery == "" || len(explain.Plan.RemotePipeNames) == 0 {
			t.Fatalf("unexpected plan: %+v", explain.Plan)
		}

		// a plain filter
		checkAnalyze(t, sut, isCluster, serviceFilter, "", 3, 1)

		// the limit query arg must be applied in the same way as at /select/logsql/query
		checkAnalyze(t, sut, isCluster, serviceFilter, "2", 2, 1)

		// a stats query
		checkAnalyze(t, sut, isCluster, serviceFilter+" | stats count() hits", "", 1, 1)

		// a query with a subquery must profile both the subquery and the outer query
		checkAnalyze(t, sut, isCluster, `trace_id:in(`+serviceFilter+` | fields trace_id)`, "", 3, 2)

		// invalid mode and invalid query must be rejected
		getProfile(t, sut, serviceFilter, "", "foo", http.StatusBadRequest)
		getProfile(t, sut, "foo | bar(", "", "explain", http.StatusBadRequest)
	}

	t.Run("single", func(t *testing.T) {
		f(t, storage, false)
	})
	t.Run("cluster", func(t *testing.T) {
		f(t, frontend, true)
	})
}
