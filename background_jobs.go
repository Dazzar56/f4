package main

import (
	"sort"
	"sync"
	"time"
)

// A long operation on a remote host runs there whether or not anybody is
// watching: the FISH+ job machinery already lets one be started, polled and
// dropped by whoever cares to. What has been missing is a place to keep the
// ones that are running, so that a dialog can be closed without either
// killing the work or losing the way back to it.
//
// The registry holds no session and no context of its own. It knows how to
// ask a job to stop and how to say what it is doing; everything else stays
// with the task that started it.

// BackgroundJobState is what the list shows about a job.
type BackgroundJobState struct {
	ID       int
	Title    string
	Status   string
	Started  time.Time
	Finished bool
}

// BackgroundJob is the handle the task that started the work keeps.
type BackgroundJob struct {
	id       int
	registry *BackgroundJobRegistry
}

// BackgroundJobRegistry is the list of jobs that are running. It is safe for
// use from several goroutines, which it has to be: a job reports progress
// from its own task while the interface reads the list from the UI thread.
type BackgroundJobRegistry struct {
	mu     sync.Mutex
	next   int
	jobs   map[int]*backgroundJobEntry
	notify []func()
}

type backgroundJobEntry struct {
	state  BackgroundJobState
	cancel func()
}

// GlobalBackgroundJobs is the registry the interface shows.
var GlobalBackgroundJobs = NewBackgroundJobRegistry()

func NewBackgroundJobRegistry() *BackgroundJobRegistry {
	return &BackgroundJobRegistry{jobs: make(map[int]*backgroundJobEntry)}
}

// Start records a job that has begun. cancel may be nil for work that cannot
// be stopped, in which case the list shows it and offers nothing.
func (r *BackgroundJobRegistry) Start(title string, cancel func()) *BackgroundJob {
	r.mu.Lock()
	r.next++
	id := r.next
	r.jobs[id] = &backgroundJobEntry{
		state:  BackgroundJobState{ID: id, Title: title, Started: time.Now()},
		cancel: cancel,
	}
	r.mu.Unlock()
	r.changed()
	return &BackgroundJob{id: id, registry: r}
}

// SetStatus replaces the line the list shows for a job. It is called from
// the job's own progress callback, so it does nothing expensive.
func (j *BackgroundJob) SetStatus(status string) {
	if j == nil {
		return
	}
	r := j.registry
	r.mu.Lock()
	if e := r.jobs[j.id]; e != nil {
		e.state.Status = status
	}
	r.mu.Unlock()
	r.changed()
}

// Finish marks a job as ended and takes it out of the list. A job that
// failed is finished too: the error belongs to whoever was waiting for it.
func (j *BackgroundJob) Finish() {
	if j == nil {
		return
	}
	r := j.registry
	r.mu.Lock()
	delete(r.jobs, j.id)
	r.mu.Unlock()
	r.changed()
}

// ID identifies the job in the list.
func (j *BackgroundJob) ID() int {
	if j == nil {
		return 0
	}
	return j.id
}

// List returns what is running, oldest first, which is the order somebody
// watching a list expects things to have started in.
func (r *BackgroundJobRegistry) List() []BackgroundJobState {
	r.mu.Lock()
	out := make([]BackgroundJobState, 0, len(r.jobs))
	for _, e := range r.jobs {
		out = append(out, e.state)
	}
	r.mu.Unlock()
	sort.Slice(out, func(i, k int) bool { return out[i].ID < out[k].ID })
	return out
}

// Cancel asks a job to stop and reports whether there was one to ask. The
// job is not removed here: it disappears from the list when the work it was
// doing actually ends, so that a cancel that takes a while is still visible.
func (r *BackgroundJobRegistry) Cancel(id int) bool {
	r.mu.Lock()
	e := r.jobs[id]
	var cancel func()
	if e != nil {
		cancel = e.cancel
		e.state.Status = "cancelling"
	}
	r.mu.Unlock()
	if e == nil {
		return false
	}
	r.changed()
	if cancel != nil {
		cancel()
	}
	return true
}

// CancelAll stops everything, for a quit that must not leave remote work
// running on somebody's server.
func (r *BackgroundJobRegistry) CancelAll() {
	for _, s := range r.List() {
		r.Cancel(s.ID)
	}
}

// OnChange registers a callback fired whenever the list changes, so a window
// showing it can redraw. It is called with no lock held.
func (r *BackgroundJobRegistry) OnChange(fn func()) {
	r.mu.Lock()
	r.notify = append(r.notify, fn)
	r.mu.Unlock()
}

func (r *BackgroundJobRegistry) changed() {
	r.mu.Lock()
	fns := append([]func(){}, r.notify...)
	r.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}