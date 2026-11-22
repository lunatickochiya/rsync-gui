package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/lunatickochiya/rsync-gui/web/internal/location"
	"github.com/lunatickochiya/rsync-gui/web/internal/rsync"
)

type commandInfo struct {
	Phase   string `json:"phase"`
	Command string `json:"command"`
}

type undoInfo struct {
	Destination  string            `json:"destination"`
	Deletions    int               `json:"deletions"`
	Restorations int               `json:"restorations"`
	Result       *rsync.UndoResult `json:"result,omitempty"`
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
	// Undo is the data of a run that can still be undone (set when the last run
	// changed a local destination and recorded undo data).
	Undo *undoInfo `json:"undo,omitempty"`
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
	// Undo data of the last finished run (see rsync.UndoRecord): nil when there
	// is none. undoCancel stops a running undo; undoDest keeps the destination
	// for the result panel after the record itself is cleared.
	undoRecord *rsync.UndoRecord
	undoResult *rsync.UndoResult
	undoDest   string
	undoCancel context.CancelFunc
	// undoHook is called with the record of a finished run (nil when it cannot
	// be undone), so that the owner can persist the pointer to it.
	undoHook func(*rsync.UndoRecord)
}

func NewJobManager() *JobManager {
	return &JobManager{subs: make(map[chan sseEvent]struct{})}
}

// SetUndoHook installs the callback for finished runs (see undoHook).
func (j *JobManager) SetUndoHook(hook func(*rsync.UndoRecord)) {
	j.mu.Lock()
	j.undoHook = hook
	j.mu.Unlock()
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
	snapshot := jobSnapshot{
		Running:    j.running,
		Done:       j.done,
		Cancelling: j.cancelling,
		State:      j.state,
		Logs:       append([]rsync.LogLine(nil), j.logs...),
		Summary:    j.summary,
		Commands:   append([]commandInfo(nil), j.commands...),
		StartedAt:  j.startedAt,
	}
	if j.undoRecord != nil || j.undoResult != nil {
		undo := &undoInfo{
			Destination: j.undoDest,
			Result:      j.undoResult,
		}
		if j.undoRecord != nil {
			undo.Deletions, undo.Restorations, _ = rsync.UndoCounts(*j.undoRecord)
		}
		snapshot.Undo = undo
	}
	return snapshot
}

// SetUndoRecord remembers a run that can be undone (called when its events end).
func (j *JobManager) SetUndoRecord(rec *rsync.UndoRecord) {
	j.mu.Lock()
	j.undoRecord = rec
	j.undoResult = nil
	if rec != nil {
		j.undoDest = rec.Destination
	}
	snapshot := j.snapshotLocked()
	j.mu.Unlock()
	j.broadcast("snapshot", snapshot)
}

// ClearUndoRecord forgets the record without deleting its data.
func (j *JobManager) ClearUndoRecord() {
	j.mu.Lock()
	j.undoRecord = nil
	j.undoResult = nil
	j.mu.Unlock()
}

// AppendLog adds a line to the job log and tells the SSE clients.
func (j *JobManager) AppendLog(level rsync.Level, text string) {
	line := rsync.LogLine{Time: time.Now(), Level: level, Text: text}
	j.mu.Lock()
	j.logs = append(j.logs, line)
	if len(j.logs) > maxJobLogs {
		j.logs = j.logs[len(j.logs)-maxJobLogs:]
	}
	j.mu.Unlock()
	j.broadcast("log", line)
}

// DiscardUndoRecord deletes the undo data of the previous run (before a new one
// starts, so that rsync does not see it as part of the destination). Returns
// false when something was left behind.
func (j *JobManager) DiscardUndoRecord() ([]string, bool) {
	j.mu.Lock()
	rec := j.undoRecord
	j.undoRecord = nil
	j.undoResult = nil
	j.undoDest = ""
	hook := j.undoHook
	j.mu.Unlock()
	if hook != nil {
		hook(nil)
	}
	if rec == nil {
		return nil, true
	}
	return rsync.UndoDiscard(*rec)
}

// UndoCount tells the UI whether there is something to undo.
func (j *JobManager) UndoAvailable() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.undoRecord != nil
}

// Undo restores the destination of the last run. Returns false when a job or an
// undo is already running, or when there is nothing to undo.
func (j *JobManager) Undo() bool {
	j.mu.Lock()
	if j.running || j.undoRecord == nil {
		j.mu.Unlock()
		return false
	}
	rec := *j.undoRecord
	ctx, cancel := context.WithCancel(context.Background())
	j.running = true
	j.done = false
	j.cancelling = false
	j.summary = nil
	j.undoResult = nil
	j.undoCancel = cancel
	j.startedAt = time.Now()
	j.state = rsync.State{Phase: "撤回", Indeterminate: true}
	logLine := rsync.LogLine{Time: time.Now(), Level: rsync.LevelInfo,
		Text: "--- 撤回：恢复 " + location.DisplayPath(rec.Destination) + " ---"}
	j.logs = append(j.logs, logLine)
	snapshot := j.snapshotLocked()
	j.mu.Unlock()
	j.broadcast("snapshot", snapshot)

	go func() {
		result := rsync.UndoExecute(ctx, rec, func(permille int, status string) {
			j.mu.Lock()
			j.state.Indeterminate = false
			j.state.Percent = float64(permille) / 10
			j.state.CurrentFile = status
			state := j.state
			j.mu.Unlock()
			j.broadcast("state", state)
		})
		j.mu.Lock()
		j.running = false
		j.done = true
		j.cancelling = false
		j.undoCancel = nil
		j.undoResult = &result
		j.state.Indeterminate = false
		j.state.CurrentFile = ""
		j.state.Percent = 100
		for _, problem := range result.Problems {
			j.logs = append(j.logs, rsync.LogLine{Time: time.Now(), Level: rsync.LevelWarning, Text: problem})
		}
		level := rsync.LevelSuccess
		switch {
		case result.Cancelled:
			level = rsync.LevelWarning
		case result.Failed > 0:
			level = rsync.LevelError
		}
		j.logs = append(j.logs, rsync.LogLine{Time: time.Now(), Level: level,
			Text: fmt.Sprintf("已撤回：删除 %d 项，还原 %d 项。", result.Deleted, result.Restored)})
		if result.OK() {
			j.undoRecord = nil // the backup dir is empty and removed now
		}
		record := j.undoRecord
		hook := j.undoHook
		snapshot := j.snapshotLocked()
		j.mu.Unlock()
		j.broadcast("snapshot", snapshot)
		if hook != nil {
			hook(record)
		}
	}()
	return true
}

// CancelUndo stops a running undo.
func (j *JobManager) CancelUndo() bool {
	j.mu.Lock()
	cancel := j.undoCancel
	cancelling := cancel != nil && !j.cancelling
	if cancelling {
		j.cancelling = true
	}
	snapshot := j.snapshotLocked()
	j.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if cancelling {
		j.broadcast("snapshot", snapshot)
	}
	return cancelling
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
		case rsync.EventUndoAvailable:
			j.mu.Lock()
			j.undoRecord = ev.UndoRecord
			j.undoDest = ev.UndoRecord.Destination
			j.mu.Unlock()
			// The UI learns about it with the "done" event's snapshot.
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
			snapshot := j.snapshotLocked()
			record := j.undoRecord
			hook := j.undoHook
			j.mu.Unlock()
			j.broadcast("done", summary)
			j.broadcast("snapshot", snapshot)
			// Tell the owner whether this run can be undone (nil: it cannot).
			if hook != nil {
				hook(record)
			}
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
