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

	storage := tc.MustStartVtsingle("vtstorage", []string{
		"-storageDataPath=" + tc.Dir() + "/vtstorage",
		"-retentionPeriod=100y",
	})
	frontend := tc.MustStartVtselect("vtselect", []string{
		"-storageNode=" + storage.HTTPAddr(),
	})

	serviceName := "testProfileService"
	otherServiceName := "testProfileOtherService"
	spanTime := time.Now().Add(-time.Hour)
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
