package logsql

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/VictoriaMetrics/VictoriaLogs/lib/logstorage"
	"github.com/VictoriaMetrics/VictoriaMetrics/lib/httpserver"

	"github.com/VictoriaMetrics/VictoriaTraces/app/vtselect/traces/tracecommon"
	"github.com/VictoriaMetrics/VictoriaTraces/app/vtstorage"
)

// queryProfileStage is the stage name recorded for the queries executed by /select/logsql/profile.
const queryProfileStage = "logsql_query"

type queryProfileResponse struct {
	Mode                   string                           `json:"mode"`
	Query                  string                           `json:"query"`
	ExecutedQuery          string                           `json:"executed_query"`
	PlanningDurationNsecs  int64                            `json:"planning_duration_nsecs"`
	ExecutionDurationNsecs int64                            `json:"execution_duration_nsecs,omitempty"`
	Plan                   logstorage.QueryStaticPlan       `json:"plan"`
	Analysis               *queryProfileAnalysis            `json:"analysis,omitempty"`
	Execution              *logstorage.QueryProfileSnapshot `json:"execution,omitempty"`
	Warnings               []string                         `json:"warnings"`
	Error                  string                           `json:"error,omitempty"`
}

type queryProfileAnalysis struct {
	RowsReturned   uint64 `json:"rows_returned"`
	BlocksReturned uint64 `json:"blocks_returned"`
}

// ProcessProfileRequest handles /select/logsql/profile request.
//
// It reports the plan and, with mode=analyze, the execution stats of the query passed to /select/logsql/query.
// It accepts the same query args as /select/logsql/query except of format.
func ProcessProfileRequest(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	planningStart := time.Now()
	ca, err := parseCommonArgs(r)
	if err != nil {
		httpserver.Errorf(w, r, "%s", err)
		return
	}
	offset, err := getPositiveInt(r, "offset")
	if err != nil {
		httpserver.Errorf(w, r, "%s", err)
		return
	}
	limit, err := getPositiveInt(r, "limit")
	if err != nil {
		httpserver.Errorf(w, r, "%s", err)
		return
	}
	mode, err := tracecommon.GetProfileMode(r)
	if err != nil {
		httpserver.Errorf(w, r, "incorrect profile mode: %s", err)
		return
	}

	// Build the query in the same way as /select/logsql/query does.
	addQueryOffsetLimit(ca.q, offset, limit)

	resp := queryProfileResponse{
		Mode:          mode,
		Query:         r.FormValue("query"),
		ExecutedQuery: ca.q.String(),
		Plan:          logstorage.GetQueryStaticPlan(ca.q),
		Warnings: []string{
			"operator durations are summed wall-clock activity across workers, not CPU time",
			"exclusive_active_duration_nsecs excludes synchronous downstream forwarding but can include internal waits",
			"bloom-filter row selectivity and stream-index bytes are omitted because they cannot yet be measured exactly",
		},
	}
	resp.PlanningDurationNsecs = time.Since(planningStart).Nanoseconds()

	statusCode := http.StatusOK
	if mode == "analyze" {
		collector := logstorage.NewQueryProfileCollector()
		executionStart := time.Now()
		analysis, runErr := analyzeQuery(ctx, ca, collector)
		resp.ExecutionDurationNsecs = time.Since(executionStart).Nanoseconds()
		resp.Analysis = &analysis
		snapshot := collector.Snapshot()
		if runErr != nil {
			resp.Error = runErr.Error()
			snapshot.Error = runErr.Error()
			statusCode = http.StatusInternalServerError
		}
		resp.Execution = &snapshot
	}

	data, err := json.Marshal(resp)
	if err != nil {
		httpserver.Errorf(w, r, "cannot marshal profile response: %s", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_, _ = w.Write(data)
}

// analyzeQuery executes ca.q with the attached profile collector and drops the returned rows after counting them.
func analyzeQuery(ctx context.Context, ca *commonArgs, collector *logstorage.QueryProfileCollector) (queryProfileAnalysis, error) {
	qctx := ca.newQueryContext(ctx)
	qctx.AttachQueryProfile(collector)
	qctx.SetQueryProfileStage(queryProfileStage, "local")
	defer ca.updatePerQueryStatsMetrics()

	var rowsReturned, blocksReturned atomic.Uint64
	writeBlock := func(_ uint, db *logstorage.DataBlock) {
		blocksReturned.Add(1)
		rowsReturned.Add(uint64(db.RowsCount()))
	}
	err := vtstorage.RunQuery(qctx, writeBlock)
	if cause := context.Cause(ctx); cause != nil && err == nil {
		err = cause
	}
	return queryProfileAnalysis{
		RowsReturned:   rowsReturned.Load(),
		BlocksReturned: blocksReturned.Load(),
	}, err
}
