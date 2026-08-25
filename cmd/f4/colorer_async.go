package main

import (
	"context"
	"time"

	colorer "github.com/unxed/colorer4go"
	"github.com/unxed/vtui"
)

// colorerJob contains only immutable data captured on the UI thread. In
// particular, the worker never calls EditorView.lineTextForHighlight while
// the document can be edited.
type colorerJob struct {
	id           uint64
	generation   uint64
	target       int
	line         string
	contextStart int
	context      []string
	reset        bool
	baseAttr     uint64
	syntax       bool
	total        int
}

type colorerResult struct {
	job        colorerJob
	attrs      []uint64
	background uint64
	parsedIdx  int
	err        error
}

// startWorker transfers ownership of session calls to one goroutine. A
// Colorer Session is stateful, so a pool of concurrent calls would be wrong;
// one worker also gives cancellation a single well-defined owner.
func (ch *ColorerHighlighter) startWorker(session *colorer.Session) {
	if ch.closed {
		session.Close()
		return
	}
	if ch.sessionCtx == nil {
		ch.sessionCtx, ch.sessionCancel = context.WithCancel(context.Background())
	}

	ch.workerJobs = make(chan colorerJob, 1)
	ch.workerDone = make(chan struct{})
	go ch.runWorker(ch.sessionCtx, session)
}

func (ch *ColorerHighlighter) runWorker(ctx context.Context, session *colorer.Session) {
	defer close(ch.workerDone)
	defer session.Close()

	parsedIdx := 0
	forgottenUpTo := 0
	forgetDisabled := false
	var baseAttr uint64
	var schemeGen uint64
	baseKnown := false

	for {
		select {
		case <-ctx.Done():
			return
		case job := <-ch.workerJobs:
			if err := ch.prepareWorkerSession(ctx, session, job, &parsedIdx, &forgottenUpTo); err != nil {
				ch.postColorerResult(colorerResult{job: job, parsedIdx: parsedIdx, err: err})
				return
			}

			generation := ColorerSchemeGeneration()
			if !baseKnown || baseAttr != job.baseAttr || schemeGen != generation {
				activeScheme := currentColorerSchemeName()
				if err := session.SetHRD("rgb", activeScheme); err != nil {
					ch.postColorerResult(colorerResult{job: job, parsedIdx: parsedIdx, err: err})
					return
				}
				baseAttr = job.baseAttr
				baseKnown = true
				schemeGen = generation
			}

			for i, contextLine := range job.context {
				if ctx.Err() != nil {
					return
				}
				if _, err := session.ParseLine(contextLine); err != nil {
					vtui.DebugLog("COLORER: ParseLine failed in context at line %d: %v", job.contextStart+i, err)
					ch.postColorerResult(colorerResult{job: job, parsedIdx: parsedIdx, err: err})
					return
				}
				parsedIdx++
				if i == len(job.context)-1 || i%16 == 0 {
					ch.postColorerProgress(job, i+1)
				}
			}

			if parsedIdx != job.target {
				err := colorerWorkerStateError{want: job.target, got: parsedIdx}
				ch.postColorerResult(colorerResult{job: job, parsedIdx: parsedIdx, err: err})
				return
			}

			if ctx.Err() != nil {
				return
			}
			regions, err := session.ParseLine(job.line)
			parsedIdx = job.target + 1
			if err != nil {
				vtui.DebugLog("COLORER: ParseLine failed at line %d: %v", job.target, err)
				ch.postColorerResult(colorerResult{job: job, parsedIdx: parsedIdx, err: err})
				return
			}

			attrs, background := ch.attrsForSyntax(job.line, regions, job.baseAttr, job.syntax)
			if ctx.Err() != nil {
				return
			}
			if !forgetDisabled {
				if keepFrom, do := colorerForgetPlan(parsedIdx, forgottenUpTo); do {
					if err := session.ForgetBefore(keepFrom); err != nil {
						vtui.DebugLog("COLORER: ForgetBefore unsupported, disabling for this session: %v", err)
						forgetDisabled = true
					} else {
						forgottenUpTo = keepFrom
					}
				}
			}
			ch.postColorerProgress(job, job.total)
			ch.postColorerResult(colorerResult{
				job:        job,
				attrs:      attrs,
				background: background,
				parsedIdx:  parsedIdx,
			})
		}
	}
}

type colorerWorkerStateError struct {
	want int
	got  int
}

func (e colorerWorkerStateError) Error() string {
	return "colorer worker state mismatch"
}

func currentColorerSchemeName() string {
	schemeMu.Lock()
	name := schemeName
	schemeMu.Unlock()
	if name == "" {
		return "default"
	}
	return name
}

func (ch *ColorerHighlighter) prepareWorkerSession(ctx context.Context, session *colorer.Session, job colorerJob, parsedIdx, forgottenUpTo *int) error {
	needReset := job.reset || *parsedIdx != job.contextStart
	if needReset {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		session.Reset()
		if _, err := session.SelectType(ch.filename, ch.firstLine); err != nil {
			return err
		}
		*parsedIdx = job.contextStart
		*forgottenUpTo = job.contextStart
	}
	return nil
}

// queueLine is called by the UI render path. It may read the document, but it
// never calls into Colorer. A jump is re-anchored to at most
// hlColorerContext lines, and ordinary scrolling snapshots only the missing
// forward lines.
func (ch *ColorerHighlighter) queueLine(idx int, line string, baseAttr uint64) {
	if ch.pending || ch.workerJobs == nil || ch.lineAt == nil {
		return
	}

	start, reset := colorerContextPlan(ch.parsedIdx, idx)
	// Do not make a large document read on the UI thread merely because the
	// parser could theoretically be fed forward. A jump gets the same bounded
	// re-anchor as the worker's reset path.
	if !reset && idx-start > hlColorerContext {
		reset = true
		start = idx - hlColorerContext
		if start < 0 {
			start = 0
		}
	}
	if ch.forceReset {
		reset = true
		start = idx - hlColorerContext
		if start < 0 {
			start = 0
		}
	}

	contextLines := make([]string, 0, idx-start)
	for lineIdx := start; lineIdx < idx; lineIdx++ {
		contextLine, ok := ch.lineAt(lineIdx)
		if !ok {
			return
		}
		contextLines = append(contextLines, contextLine)
	}

	ch.workGeneration++
	job := colorerJob{
		id:           ch.workGeneration,
		generation:   ch.workGeneration,
		target:       idx,
		line:         line,
		contextStart: start,
		context:      contextLines,
		reset:        reset,
		baseAttr:     baseAttr,
		syntax:       AppConfig.EditorColorerSyntax,
		total:        len(contextLines) + 1,
	}
	ch.forceReset = false
	ch.pending = true
	ch.beginColorerWork(job.id, job.total)

	select {
	case ch.workerJobs <- job:
	default:
		ch.pending = false
	}
}

func (ch *ColorerHighlighter) postColorerProgress(job colorerJob, done int) {
	if ch.postTask == nil || ch.owner == nil {
		return
	}
	postTask := ch.postTask
	redraw := ch.redraw
	owner := ch.owner
	postTask(func() {
		if ch.closed || owner.highlighter != vtui.Highlighter(ch) || owner.colorerWorkID != job.id {
			return
		}
		owner.colorerProgress = done
		owner.colorerTotal = job.total
		if redraw != nil {
			redraw()
		}
	})
}

func (ch *ColorerHighlighter) postColorerResult(result colorerResult) {
	if ch.postTask == nil || ch.owner == nil {
		return
	}
	postTask := ch.postTask
	redraw := ch.redraw
	owner := ch.owner
	postTask(func() {
		if ch.closed || owner.highlighter != vtui.Highlighter(ch) {
			return
		}
		if ch.workGeneration != result.job.generation {
			ch.pending = false
			if redraw != nil {
				redraw()
			}
			return
		}
		ch.pending = false
		if result.err != nil {
			ch.disabled = true
			vtui.DebugLog("COLORER: background highlighting stopped: %v", result.err)
		} else {
			ch.parsedIdx = result.parsedIdx
			ch.storeAttrs(result.job.target, result.attrs, result.background)
		}
		owner.finishColorerWork(result.job.id)
		if redraw != nil {
			redraw()
		}
	})
}

func (ch *ColorerHighlighter) beginColorerWork(id uint64, total int) {
	if ch.owner == nil || ch.owner.highlighter != vtui.Highlighter(ch) {
		return
	}
	ch.owner.colorerWorkID = id
	ch.owner.colorerIndexing = true
	ch.owner.colorerProgress = 0
	ch.owner.colorerTotal = total
	ch.owner.colorerCancel = ch.Cancel
}

func (ch *ColorerHighlighter) beginColorerStartup() {
	if ch.owner == nil || ch.owner.highlighter != vtui.Highlighter(ch) {
		return
	}
	ch.workGeneration++
	ch.startupWorkID = ch.workGeneration
	ch.owner.colorerWorkID = ch.startupWorkID
	ch.owner.colorerIndexing = true
	ch.owner.colorerProgress = 0
	ch.owner.colorerTotal = 1
	ch.owner.colorerCancel = ch.Cancel
}

func (ch *ColorerHighlighter) Cancel() {
	ch.disabled = true
	ch.pending = false
	ch.workGeneration++
	ch.forceReset = false
	if ch.sessionCancel != nil {
		ch.sessionCancel()
	}
}

func (ch *ColorerHighlighter) stopWorker() {
	if ch.workerDone == nil {
		return
	}
	select {
	case <-ch.workerDone:
	case <-time.After(250 * time.Millisecond):
		// The session owns the remaining call and will close itself when it
		// observes context cancellation. The editor must not wait forever for
		// a parser while it is closing.
	}
}

func (ev *EditorView) finishColorerWork(id uint64) {
	if ev.colorerWorkID != id {
		return
	}
	ev.colorerIndexing = false
	ev.colorerProgress = 0
	ev.colorerTotal = 0
	ev.colorerCancel = nil
}

func (ev *EditorView) cancelColorer() {
	if ev.colorerCancel != nil {
		cancel := ev.colorerCancel
		ev.colorerCancel = nil
		cancel()
	} else if ch, ok := ev.highlighter.(*ColorerHighlighter); ok {
		ch.Cancel()
	}
	ev.colorerWorkID++
	ev.colorerIndexing = false
	ev.colorerProgress = 0
	ev.colorerTotal = 0
}
