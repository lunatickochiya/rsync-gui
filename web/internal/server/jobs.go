package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/lunatickochiya/rsync-gui/web/internal/rsync"
)

type commandInfo struct {
	Phase   string `json:"phase"`
	Command string `json:"command"`
}

type jobSnapshot struct {
	Running    bool            `json:"running"`
	Done       bool            `json:"done"`
	Cancelling bool            `json:"cancelling"`
	State      rsync.State     `json:"state"`
	Logs       []rsync.LogLine `json:"logs"`
	Summary    *rsync.Summary  `json:"summary,omitempty"`
	Commands   []commandInfo   `json:"commands,omitempty"`
	StartedAt  time.Time       `json:"startedAt"`
}

type sseEvent struct {
	event string
	data  any
}

// JobManager runs one rsync plan at a time and fans events out to SSE clients.
type JobManager struct {
	mu         sync.Mutex
	running    bool
	done       bool
	cancelling bool
	state      rsync.State
	logs       []rsync.LogLine
	summary    *rsync.Summary
	commands   []commandInfo
	startedAt  time.Time
	cancel     context.CancelFunc
	subs       map[chan sseEvent]struct{}
}

func NewJobManager() *JobManager {
	return &JobManager{subs: make(map[chan sseEvent]struct{})}
}

func (j *JobManager) Running() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.running
}

func (j *JobManager) Snapshot() jobSnapshot {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.snapshotLocked()
}

func (j *JobManager) snapshotLocked() jobSnapshot {
	return jobSnapshot{
		Running:    j.running,
		Done:       j.done,
		Cancelling: j.cancelling,
		State:      j.state,
		Logs:       append([]rsync.LogLine(nil), j.logs...),
		Summary:    j.summary,
		Commands:   append([]commandInfo(nil), j.commands...),
		StartedAt:  j.startedAt,
	}
}

// Start takes ownership of plan and returns false when a job is already running.
func (j *JobManager) Start(plan *rsync.Plan, commands []commandInfo) bool {
	j.mu.Lock()
	if j.running {
		j.mu.Unlock()
		return false
	}
	ctx, cancel := context.WithCancel(context.Background())
	j.running = true
	j.done = false
	j.cancelling = false
	j.summary = nil
	j.logs = nil
	j.commands = append([]commandInfo(nil), commands...)
	j.startedAt = time.Now()
	j.state = rsync.State{TransferCount: len(plan.Transfers)}
	j.cancel = cancel
	snapshot := j.snapshotLocked()
	j.mu.Unlock()

	j.broadcast("snapshot", snapshot)
	events := rsync.Start(ctx, plan)
	go j.consume(events)
	return true
}

func (j *JobManager) consume(events <-chan rsync.Event) {
	for ev := range events {
		switch ev.Kind {
		case rsync.EventState:
			j.mu.Lock()
			j.state = ev.State
			j.mu.Unlock()
			j.broadcast("state", ev.State)
		case rsync.EventLog:
			j.mu.Lock()
			j.logs = append(j.logs, ev.Log)
			if len(j.logs) > maxJobLogs {
				j.logs = j.logs[len(j.logs)-maxJobLogs:]
			}
			j.mu.Unlock()
			j.broadcast("log", ev.Log)
		case rsync.EventDone:
			j.mu.Lock()
			j.running = false
			j.done = true
			j.cancelling = false
			j.cancel = nil
			j.state = ev.State
			if ev.Summary != nil {
				j.summary = ev.Summary
			} else {
				j.summary = &rsync.Summary{Aborted: true, Err: "传输进程意外结束"}
			}
			summary := j.summary
			j.mu.Unlock()
			j.broadcast("done", summary)
			return
		}
	}

	j.mu.Lock()
	if j.running {
		j.running = false
		j.done = true
		j.cancelling = false
		j.cancel = nil
		j.summary = &rsync.Summary{Aborted: true, Err: "传输进程意外结束"}
	}
	summary := j.summary
	j.mu.Unlock()
	j.broadcast("done", summary)
}

func (j *JobManager) Cancel() bool {
	j.mu.Lock()
	if !j.running || j.cancelling || j.cancel == nil {
		j.mu.Unlock()
		return false
	}
	j.cancelling = true
	cancel := j.cancel
	snapshot := j.snapshotLocked()
	j.mu.Unlock()

	cancel()
	j.broadcast("snapshot", snapshot)
	return true
}

func (j *JobManager) broadcast(event string, data any) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for ch := range j.subs {
		select {
		case ch <- sseEvent{event: event, data: data}:
		default:
		}
	}
}

func (j *JobManager) subscribe() (<-chan sseEvent, func()) {
	j.mu.Lock()
	ch := make(chan sseEvent, 512)
	j.subs[ch] = struct{}{}
	snapshot := j.snapshotLocked()
	j.mu.Unlock()

	ch <- sseEvent{event: "snapshot", data: snapshot}
	return ch, func() {
		j.mu.Lock()
		delete(j.subs, ch)
		j.mu.Unlock()
	}
}

func (j *JobManager) ServeSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "流式响应不可用", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	events, unsubscribe := j.subscribe()
	defer unsubscribe()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case ev, open := <-events:
			if !open {
				return
			}
			data, err := json.Marshal(ev.data)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.event, data); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
