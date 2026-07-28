package queuemonitor

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

const RootPath = "/admin/queues"

type Inspector interface {
	Queues() ([]string, error)
	GetQueueInfo(string) (*asynq.QueueInfo, error)
	ListPendingTasks(string, ...asynq.ListOption) ([]*asynq.TaskInfo, error)
	ListActiveTasks(string, ...asynq.ListOption) ([]*asynq.TaskInfo, error)
	ListScheduledTasks(string, ...asynq.ListOption) ([]*asynq.TaskInfo, error)
	ListRetryTasks(string, ...asynq.ListOption) ([]*asynq.TaskInfo, error)
	ListArchivedTasks(string, ...asynq.ListOption) ([]*asynq.TaskInfo, error)
}

type Handler struct {
	inspector Inspector
	close     func() error
	now       func() time.Time
}

func New(redisOption asynq.RedisConnOpt) (*Handler, error) {
	client, ok := redisOption.MakeRedisClient().(redis.UniversalClient)
	if !ok {
		return nil, fmt.Errorf("unsupported Asynq Redis connection option %T", redisOption)
	}
	return &Handler{
		inspector: asynq.NewInspectorFromRedisClient(client),
		close:     client.Close,
		now:       time.Now,
	}, nil
}

func NewWithInspector(inspector Inspector) *Handler {
	return &Handler{inspector: inspector, close: func() error { return nil }, now: time.Now}
}

func (h *Handler) Close() error {
	if h == nil || h.close == nil {
		return nil
	}
	return h.close()
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path != RootPath && r.URL.Path != RootPath+"/" && r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'none'; base-uri 'none'; frame-ancestors 'none'")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	page, err := h.dashboard(strings.TrimSpace(r.URL.Query().Get("queue")))
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = monitorPage.Execute(w, dashboardPage{
			RefreshedAt: h.now().UTC().Format("2006-01-02 15:04:05 UTC"),
			Error:       "Queue data is temporarily unavailable.",
		})
		return
	}
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_ = monitorPage.Execute(w, page)
	}
}

// ServeBackofficeJSON provides the same read-only operational snapshot as the
// legacy HTML monitor. It deliberately has no command surface: queue retries,
// deletion, and replay remain unavailable from the platform console.
// Authentication is applied by the platform router that mounts this method.
func (h *Handler) ServeBackofficeJSON(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", http.MethodGet+", "+http.MethodHead)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	page, err := h.dashboard(strings.TrimSpace(r.URL.Query().Get("queue")))
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		if r.Method != http.MethodHead {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]string{"code": "service_unavailable", "message": "Queue data is temporarily unavailable."},
			})
		}
		return
	}
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"dashboard": page})
}

type queueRow struct {
	Name      string `json:"name"`
	Pending   int    `json:"pending"`
	Active    int    `json:"active"`
	Scheduled int    `json:"scheduled"`
	Retry     int    `json:"retry"`
	Archived  int    `json:"archived"`
	Failed    int    `json:"failed"`
	Latency   string `json:"latency"`
	Selected  bool   `json:"selected"`
}

type taskRow struct {
	ID            string `json:"id"`
	Type          string `json:"type"`
	State         string `json:"state"`
	Retries       string `json:"retries"`
	LastError     string `json:"last_error"`
	NextProcessAt string `json:"next_process_at"`
}

type dashboardPage struct {
	Queues            []queueRow `json:"queues"`
	Tasks             []taskRow  `json:"tasks"`
	SelectedQueue     string     `json:"selected_queue"`
	RefreshedAt       string     `json:"refreshed_at"`
	TotalPending      int        `json:"total_pending"`
	TotalActive       int        `json:"total_active"`
	TotalRetry        int        `json:"total_retry"`
	TotalFailed       int        `json:"total_failed"`
	SelectedTaskCount int        `json:"selected_task_count"`
	Error             string     `json:"error,omitempty"`
}

func (h *Handler) dashboard(requestedQueue string) (dashboardPage, error) {
	queueNames, err := h.inspector.Queues()
	if err != nil {
		return dashboardPage{}, err
	}
	sort.Strings(queueNames)
	selectedQueue := ""
	for _, name := range queueNames {
		if name == requestedQueue {
			selectedQueue = name
		}
	}
	if selectedQueue == "" && len(queueNames) > 0 {
		selectedQueue = queueNames[0]
	}

	page := dashboardPage{
		SelectedQueue: selectedQueue,
		RefreshedAt:   h.now().UTC().Format("2006-01-02 15:04:05 UTC"),
	}
	for _, name := range queueNames {
		info, infoErr := h.inspector.GetQueueInfo(name)
		if infoErr != nil {
			return dashboardPage{}, infoErr
		}
		page.Queues = append(page.Queues, queueRow{
			Name: name, Pending: info.Pending, Active: info.Active, Scheduled: info.Scheduled,
			Retry: info.Retry, Archived: info.Archived, Failed: info.Failed,
			Latency: formatDuration(info.Latency), Selected: name == selectedQueue,
		})
		page.TotalPending += info.Pending
		page.TotalActive += info.Active
		page.TotalRetry += info.Retry
		page.TotalFailed += info.Failed
	}
	if selectedQueue == "" {
		return page, nil
	}

	taskGroups := []struct {
		state string
		list  func(string, ...asynq.ListOption) ([]*asynq.TaskInfo, error)
	}{
		{state: "active", list: h.inspector.ListActiveTasks},
		{state: "pending", list: h.inspector.ListPendingTasks},
		{state: "retry", list: h.inspector.ListRetryTasks},
		{state: "scheduled", list: h.inspector.ListScheduledTasks},
		{state: "archived", list: h.inspector.ListArchivedTasks},
	}
	for _, group := range taskGroups {
		tasks, listErr := group.list(selectedQueue, asynq.PageSize(20), asynq.Page(1))
		if listErr != nil {
			return dashboardPage{}, listErr
		}
		for _, task := range tasks {
			next := "-"
			if !task.NextProcessAt.IsZero() {
				next = task.NextProcessAt.UTC().Format("2006-01-02 15:04:05")
			}
			lastError := strings.TrimSpace(task.LastErr)
			if lastError == "" {
				lastError = "-"
			}
			page.Tasks = append(page.Tasks, taskRow{
				ID: task.ID, Type: task.Type, State: group.state,
				Retries:   fmt.Sprintf("%d / %d", task.Retried, task.MaxRetry),
				LastError: lastError, NextProcessAt: next,
			})
		}
	}
	page.SelectedTaskCount = len(page.Tasks)
	return page, nil
}

func formatDuration(value time.Duration) string {
	if value <= 0 {
		return "0s"
	}
	return value.Round(time.Second).String()
}

var monitorPage = template.Must(template.New("queue-monitor").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta http-equiv="refresh" content="15">
  <title>Queue operations · Fidibeki</title>
  <style>
    :root { color-scheme: light; font-family: Inter, ui-sans-serif, system-ui, sans-serif; color: #172321; background: #f4f7f6; }
    * { box-sizing: border-box; }
    body { margin: 0; min-width: 320px; }
    header { display: flex; min-height: 58px; align-items: center; gap: 12px; padding: 0 24px; background: #fff; border-bottom: 1px solid #dbe3e0; }
    .brand { display: flex; align-items: center; gap: 9px; font-size: 14px; font-weight: 750; }
    .brand i { display: grid; width: 29px; height: 29px; place-items: center; background: #172321; color: #fff; border-radius: 5px; font-style: normal; }
    nav { display: flex; align-self: stretch; gap: 4px; }
    nav a { display: flex; align-items: center; padding: 0 10px; border-bottom: 2px solid transparent; color: #65736f; font-size: 11px; font-weight: 700; text-decoration: none; }
    nav a[aria-current="page"] { border-color: #087f73; color: #172321; }
    .readonly { margin-left: auto; padding: 4px 8px; background: #e4f2ee; color: #087f73; border-radius: 4px; font-size: 10px; font-weight: 750; text-transform: uppercase; }
    main { width: min(1320px, 100%); margin: 0 auto; padding: 26px 28px 50px; }
    .heading { display: flex; align-items: end; justify-content: space-between; gap: 20px; margin-bottom: 20px; }
    .heading p { margin: 0 0 4px; color: #087f73; font-size: 10px; font-weight: 800; text-transform: uppercase; }
    h1 { margin: 0; font-size: 24px; letter-spacing: 0; }
    .heading time { color: #6a7874; font-size: 11px; }
    .error { padding: 14px; background: #fbe7e9; color: #a72f3b; border: 1px solid #e9b8bd; border-radius: 5px; }
    .ledger { display: grid; grid-template-columns: repeat(4, minmax(130px, 1fr)); margin-bottom: 18px; background: #fff; border: 1px solid #dbe3e0; border-radius: 6px; }
    .ledger div { min-height: 92px; padding: 18px; border-right: 1px solid #e4e9e7; }
    .ledger div:last-child { border: 0; }
    .ledger span { display: block; color: #687672; font-size: 10px; font-weight: 700; }
    .ledger b { display: block; margin-top: 6px; font-size: 25px; }
    .surface { margin-bottom: 18px; overflow: hidden; background: #fff; border: 1px solid #dbe3e0; border-radius: 6px; }
    .surface > h2 { margin: 0; padding: 15px 17px; border-bottom: 1px solid #e4e9e7; font-size: 13px; }
    table { width: 100%; border-collapse: collapse; table-layout: fixed; }
    th { height: 36px; padding: 0 12px; background: #f8faf9; color: #6a7874; font-size: 9px; text-align: left; text-transform: uppercase; }
    td { height: 48px; padding: 8px 12px; border-top: 1px solid #e6ebe9; font-size: 11px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    tbody tr:hover { background: #f5f9f7; }
    a { color: inherit; text-decoration: none; }
    .queue-name a { color: #087f73; font-weight: 750; }
    tr.selected { background: #edf6f3; }
    .state { display: inline-flex; min-width: 64px; justify-content: center; padding: 3px 6px; border-radius: 4px; background: #edf2f0; font-size: 9px; font-weight: 750; text-transform: uppercase; }
    .state-retry, .state-archived { background: #fff0d7; color: #8b4b05; }
    .state-active { background: #e6effc; color: #285ea9; }
    .empty { padding: 34px; color: #6a7874; font-size: 12px; text-align: center; }
    @media (max-width: 760px) { header { align-items: flex-start; flex-wrap: wrap; padding: 11px 14px 0; } .brand { min-height: 36px; } .readonly { min-height: 22px; margin-top: 7px; } nav { width: 100%; height: 42px; order: 3; overflow-x: auto; } nav a { flex: 0 0 auto; } main { padding: 18px 12px 36px; } .heading { align-items: start; flex-direction: column; } .ledger { grid-template-columns: 1fr 1fr; } .ledger div:nth-child(2) { border-right: 0; } .ledger div:nth-child(-n+2) { border-bottom: 1px solid #e4e9e7; } .wide { display: none; } }
  </style>
</head>
<body>
  <header>
    <div class="brand"><i>F</i> fidibeki operations</div>
    <nav aria-label="Platform operations">
      <a href="/admin/queues/" aria-current="page">Queue operations</a>
      <a href="/admin/operations/privacy-incidents">Privacy incidents</a>
    </nav>
    <span class="readonly">Read only</span>
  </header>
  <main>
    <div class="heading"><div><p>Platform admin</p><h1>Queue operations</h1></div><time>Refreshed {{.RefreshedAt}}</time></div>
    {{if .Error}}<p class="error">{{.Error}}</p>{{else}}
    <section class="ledger" aria-label="Queue totals">
      <div><span>Pending</span><b>{{.TotalPending}}</b></div><div><span>Active</span><b>{{.TotalActive}}</b></div><div><span>Retrying</span><b>{{.TotalRetry}}</b></div><div><span>Failed today</span><b>{{.TotalFailed}}</b></div>
    </section>
    <section class="surface"><h2>Queues</h2>{{if .Queues}}<table><thead><tr><th>Queue</th><th>Pending</th><th>Active</th><th>Scheduled</th><th>Retry</th><th class="wide">Archived</th><th class="wide">Latency</th></tr></thead><tbody>{{range .Queues}}<tr class="{{if .Selected}}selected{{end}}"><td class="queue-name"><a href="?queue={{.Name}}">{{.Name}}</a></td><td>{{.Pending}}</td><td>{{.Active}}</td><td>{{.Scheduled}}</td><td>{{.Retry}}</td><td class="wide">{{.Archived}}</td><td class="wide">{{.Latency}}</td></tr>{{end}}</tbody></table>{{else}}<div class="empty">No queues have been created yet.</div>{{end}}</section>
    <section class="surface"><h2>{{if .SelectedQueue}}{{.SelectedQueue}} tasks · {{.SelectedTaskCount}} shown{{else}}Tasks{{end}}</h2>{{if .Tasks}}<table><thead><tr><th style="width:22%">Type</th><th style="width:15%">State</th><th style="width:22%">Task ID</th><th style="width:10%">Retries</th><th class="wide">Last error</th><th class="wide" style="width:17%">Next run (UTC)</th></tr></thead><tbody>{{range .Tasks}}<tr><td>{{.Type}}</td><td><span class="state state-{{.State}}">{{.State}}</span></td><td title="{{.ID}}">{{.ID}}</td><td>{{.Retries}}</td><td class="wide" title="{{.LastError}}">{{.LastError}}</td><td class="wide">{{.NextProcessAt}}</td></tr>{{end}}</tbody></table>{{else}}<div class="empty">No pending, active, scheduled, retry, or archived tasks in this queue.</div>{{end}}</section>
    {{end}}
  </main>
</body>
</html>`))
