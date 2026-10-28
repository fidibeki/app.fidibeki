package websitechat

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/fidibeki/app.fidibeki/backend/internal/project"
	"github.com/fidibeki/app.fidibeki/backend/internal/retentionhealth"
	"github.com/jackc/pgx/v5/pgconn"
)

type testAuthorizer struct{}

func (testAuthorizer) Authorize(context.Context, string, string) (project.PublicConfiguration, error) {
	return project.PublicConfiguration{}, nil
}

func TestRequestBodyDoesNotHoldDatabaseConnection(t *testing.T) {
	var begins atomic.Int32
	h := NewHandler(Options{Authorizer: testAuthorizer{}, Begin: func(context.Context) (db.Transaction, error) {
		begins.Add(1)
		return nil, errors.New("database unavailable")
	}})
	route := h.run(false, "", h.session)
	reader, writer := io.Pipe()
	r := httptest.NewRequest(http.MethodPost, "/v1/widget/chat/session", reader)
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { route(w, r); close(done) }()
	if _, err := writer.Write([]byte("{")); err != nil {
		t.Fatal(err)
	}
	if begins.Load() != 0 {
		t.Fatal("incomplete request acquired a database connection")
	}
	if _, err := writer.Write([]byte("}")); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	<-done
	if begins.Load() != 1 {
		t.Fatal("complete request did not proceed to authorization")
	}
	begins.Store(0)
	route(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/widget/chat/session", strings.NewReader(strings.Repeat("x", 20001))))
	if begins.Load() != 0 {
		t.Fatal("oversized body reached the database")
	}
}

type cleanupDB struct {
	db.Querier
	remaining int
	failure   error
}

func (d *cleanupDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	if d.failure != nil {
		return pgconn.CommandTag{}, d.failure
	}
	if d.remaining == 0 {
		return pgconn.NewCommandTag("DELETE 0"), nil
	}
	d.remaining -= 100
	return pgconn.NewCommandTag("DELETE 100"), nil
}

type cleanupHealth struct {
	runs   []retentionhealth.Run
	cancel context.CancelFunc
}

func (h *cleanupHealth) RecordRun(_ context.Context, run retentionhealth.Run) error {
	h.runs = append(h.runs, run)
	if h.cancel != nil && !run.BudgetExhausted {
		h.cancel()
	}
	return nil
}

func TestRetentionDrainsBacklogWithoutWaitingADay(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	database := &cleanupDB{remaining: 2200}
	health := &cleanupHealth{cancel: cancel}
	if err := (Maintainer{Database: database, Health: health}).Run(ctx, 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if database.remaining != 0 || len(health.runs) != 3 || !health.runs[0].BudgetExhausted || health.runs[2].BudgetExhausted {
		t.Fatalf("backlog not drained promptly: remaining=%d runs=%+v", database.remaining, health.runs)
	}
	database.failure = errors.New("cleanup unavailable")
	if err := (Maintainer{Database: database, Health: health}).MaintainOnce(context.Background(), time.Now()); err == nil {
		t.Fatal("cleanup failure was hidden")
	}
	if health.runs[len(health.runs)-1].Result != retentionhealth.ResultFailed {
		t.Fatal("cleanup failure was not recorded")
	}
}
