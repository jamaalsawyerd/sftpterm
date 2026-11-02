package transfer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"charm.land/log/v2"
	"golang.org/x/time/rate"

	"github.com/jamaalsawyerd/sftpterm/internal/units"
	"github.com/jamaalsawyerd/sftpterm/internal/vfs"
)

// partSuffix marks in-progress files; they're renamed into place when done.
const partSuffix = ".sftpterm-part"

// maxFinished bounds how many finished items are kept for display.
const maxFinished = 200

// Options configure an Engine.
type Options struct {
	Local, Remote vfs.FS
	Concurrency   int
	UploadLimit   units.Rate
	DownloadLimit units.Rate
}

// Engine runs transfers between a local and a remote file system.
type Engine struct {
	local, remote vfs.FS
	limiters      [2]*rate.Limiter
	events        chan Event

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu        sync.Mutex
	items     []*item
	pending   []task
	running   int
	limit     int
	nextID    int
	busySince time.Time
	idleStats IdleEvent
}

type batch struct {
	mu     sync.Mutex // serializes conflict prompts
	policy *Resolution
	left   atomic.Int64 // files not yet checked for conflicts
}

type item struct {
	id       int
	dir      Direction
	name     string
	srcPath  string
	dstDir   string
	entry    vfs.Entry
	batch    *batch
	ctx      context.Context
	cancel   context.CancelFunc
	state    State
	done     atomic.Int64
	total    int64
	files    int
	filesOK  int
	skipped  int
	failures int
	current  string
	err      error
	started  time.Time
	finished time.Time
	pending  int // outstanding tasks

	lastBytes  int64
	lastSample time.Time
	rate       float64
}

type taskKind int

const (
	scanTask taskKind = iota
	fileTask
)

type task struct {
	it       *item
	kind     taskKind
	src, dst string
	entry    vfs.Entry
}

// New creates an engine. Call Close when done with it.
func New(opts Options) *Engine {
	ctx, cancel := context.WithCancel(context.Background())
	return &Engine{
		local:    opts.Local,
		remote:   opts.Remote,
		limiters: [2]*rate.Limiter{newLimiter(opts.UploadLimit), newLimiter(opts.DownloadLimit)},
		events:   make(chan Event, 64),
		ctx:      ctx,
		cancel:   cancel,
		limit:    max(opts.Concurrency, 1),
	}
}

// Events delivers conflicts and completions. The UI must keep reading it.
func (e *Engine) Events() <-chan Event { return e.events }

func (e *Engine) fsFor(d Direction) (src, dst vfs.FS) {
	if d == Upload {
		return e.local, e.remote
	}
	return e.remote, e.local
}

// Enqueue copies entries from srcDir into dstDir. Each entry becomes one item
// in the queue; directories are copied recursively. It returns the new item
// IDs.
func (e *Engine) Enqueue(d Direction, srcDir string, entries []vfs.Entry, dstDir string) []int {
	src, _ := e.fsFor(d)
	b := &batch{}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.running == 0 && len(e.pending) == 0 {
		e.busySince = time.Now()
		e.idleStats = IdleEvent{}
	}
	var ids []int
	for _, ent := range entries {
		e.nextID++
		ctx, cancel := context.WithCancel(e.ctx)
		it := &item{
			id: e.nextID, dir: d, name: ent.Name, srcPath: src.Join(srcDir, ent.Name),
			dstDir: dstDir, entry: ent, batch: b, ctx: ctx, cancel: cancel,
		}
		e.items = append(e.items, it)
		e.start(it)
		ids = append(ids, it.id)
	}
	e.prune()
	e.schedule()
	return ids
}

// start queues an item's first task. Caller holds e.mu.
func (e *Engine) start(it *item) {
	_, dst := e.fsFor(it.dir)
	t := task{it: it, src: it.srcPath, dst: dst.Join(it.dstDir, it.name), entry: it.entry}
	if it.entry.IsDir {
		t.kind = scanTask
		it.state = Scanning
	} else {
		t.kind = fileTask
		it.total = it.entry.Size
		it.files = 1
		it.batch.left.Add(1)
	}
	it.pending = 1
	e.pending = append(e.pending, t)
}

// schedule starts tasks up to the concurrency limit. Caller holds e.mu.
func (e *Engine) schedule() {
	for e.running < e.limit && len(e.pending) > 0 {
		t := e.pending[0]
		e.pending = e.pending[1:]
		e.running++
		e.wg.Add(1)
		go e.run(t)
	}
}

func (e *Engine) run(t task) {
	defer e.wg.Done()
	var err error
	if t.it.ctx.Err() == nil {
		switch t.kind {
		case scanTask:
			err = e.scan(t)
		case fileTask:
			err = e.transferFile(t)
		}
	} else {
		err = t.it.ctx.Err()
	}

	e.mu.Lock()
	e.running--
	var events []Event
	if ev, ok := e.finishTask(t.it, err); ok {
		events = append(events, ev)
	}
	e.schedule()
	if e.running == 0 && len(e.pending) == 0 && e.idleStats.Done+e.idleStats.Failed > 0 {
		ev := e.idleStats
		ev.Elapsed = time.Since(e.busySince)
		e.idleStats = IdleEvent{}
		events = append(events, ev)
	}
	e.mu.Unlock()

	// Never send while holding e.mu: if the buffer is full, the UI must still
	// be able to take snapshots while it drains events.
	for _, ev := range events {
		e.emit(ev)
	}
}

// finishTask records a task result, returning an event if that finished the
// item. Caller holds e.mu.
func (e *Engine) finishTask(it *item, err error) (ItemDoneEvent, bool) {
	if err != nil && !errors.Is(err, context.Canceled) {
		it.failures++
		if it.err == nil {
			it.err = err
		}
		log.Debug("transfer failed", "item", it.name, "err", err)
	}
	it.pending--
	if it.pending > 0 {
		return ItemDoneEvent{}, false
	}

	it.finished = time.Now()
	it.current = ""
	switch {
	case it.ctx.Err() != nil && it.err == nil:
		it.state = Cancelled
	case it.err != nil:
		it.state = Failed
		if it.failures > 1 {
			it.err = fmt.Errorf("%d files failed, first: %w", it.failures, it.err)
		}
	default:
		it.state = Done
	}
	it.cancel()

	ev := ItemDoneEvent{
		ID: it.id, Name: it.name, Dir: it.dir, DstDir: it.dstDir, State: it.state, Err: it.err,
		Bytes: it.done.Load(), Files: it.filesOK, Skipped: it.skipped, Elapsed: it.finished.Sub(it.started),
	}
	switch it.state {
	case Done:
		e.idleStats.Done++
		e.idleStats.Bytes += ev.Bytes
	case Failed:
		e.idleStats.Failed++
	case Queued, Scanning, Active, Cancelled:
	}
	return ev, true
}

// emit sends an event, giving up if the engine is closing. Callers must not
// hold e.mu.
func (e *Engine) emit(ev Event) {
	select {
	case e.events <- ev:
	case <-e.ctx.Done():
	}
}

// scan walks a directory, creating it at the destination and queueing a
// task per file.
func (e *Engine) scan(t task) error {
	it := t.it
	src, dst := e.fsFor(it.dir)
	e.setStarted(it)

	type dirPair struct{ src, dst string }
	stack := []dirPair{{t.src, t.dst}}
	for len(stack) > 0 {
		if err := it.ctx.Err(); err != nil {
			return err
		}
		d := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if err := ensureDir(dst, d.dst); err != nil {
			return err
		}
		entries, err := src.ReadDir(d.src)
		if err != nil {
			return err
		}
		var files []task
		for _, ent := range entries {
			s, dd := src.Join(d.src, ent.Name), dst.Join(d.dst, ent.Name)
			switch {
			case ent.IsLink && ent.IsDir:
				// Don't follow directory symlinks: they can loop.
				log.Debug("skipping directory symlink", "path", s)
			case ent.IsDir:
				stack = append(stack, dirPair{s, dd})
			case ent.Mode.IsRegular() || ent.IsLink:
				files = append(files, task{it: it, kind: fileTask, src: s, dst: dd, entry: ent})
			}
		}

		e.mu.Lock()
		for _, f := range files {
			it.total += f.entry.Size
			it.files++
			it.pending++
			it.batch.left.Add(1)
		}
		e.pending = append(e.pending, files...)
		e.schedule()
		e.mu.Unlock()
	}
	e.mu.Lock()
	if it.state == Scanning {
		it.state = Active
	}
	e.mu.Unlock()
	return nil
}

func ensureDir(fsys vfs.FS, p string) error {
	if st, err := fsys.Stat(p); err == nil {
		if !st.IsDir {
			return fmt.Errorf("%s exists and is not a folder", fsys.Base(p))
		}
		return nil
	}
	if err := fsys.Mkdir(p); err != nil {
		// Another worker may have created it in the meantime.
		if st, serr := fsys.Stat(p); serr == nil && st.IsDir {
			return nil
		}
		return err
	}
	return nil
}

func (e *Engine) setStarted(it *item) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if it.started.IsZero() {
		it.started = time.Now()
		it.lastSample = it.started
	}
	if it.state == Queued {
		it.state = Active
	}
}

func (e *Engine) transferFile(t task) error {
	it := t.it
	src, dst := e.fsFor(it.dir)
	e.setStarted(it)

	target := t.dst
	existing, err := dst.Stat(target)
	it.batch.left.Add(-1)
	switch {
	case err == nil:
		if existing.IsDir {
			return fmt.Errorf("%s: a folder with that name already exists", dst.Base(target))
		}
		res, err := e.resolve(it, target, t.entry, existing)
		if err != nil {
			return err
		}
		switch res {
		case Skip:
			e.mu.Lock()
			it.skipped++
			e.mu.Unlock()
			it.done.Add(t.entry.Size)
			return nil
		case KeepBoth:
			target = uniqueName(dst, target)
		case CancelItem:
			it.cancel()
			return context.Canceled
		case Overwrite:
		}
	case !errors.Is(err, fs.ErrNotExist):
		return err
	}

	e.mu.Lock()
	it.current = src.Base(t.src)
	e.mu.Unlock()

	var fileBytes atomic.Int64
	err = e.copyFile(it, src, dst, t.src, target, t.entry, &fileBytes)
	if err != nil {
		it.done.Add(-fileBytes.Load()) // don't count a failed file's bytes
		return err
	}
	e.mu.Lock()
	it.filesOK++
	e.mu.Unlock()
	return nil
}

func (e *Engine) copyFile(it *item, src, dst vfs.FS, srcPath, dstPath string, ent vfs.Entry, fileBytes *atomic.Int64) error {
	r, err := src.Open(srcPath)
	if err != nil {
		return err
	}
	defer r.Close()

	part := dst.Join(dst.Dir(dstPath), "."+dst.Base(dstPath)+partSuffix)
	w, err := dst.Create(part)
	if err != nil {
		return err
	}
	m := meter{ctx: it.ctx, limiter: e.limiters[it.dir], done: &it.done, file: fileBytes}
	err = copyData(w, r, it.dir, ent.Size, m)
	if cerr := w.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = dst.Remove(part)
		return err
	}

	if err := dst.Rename(part, dstPath); err != nil {
		// Plain SFTP rename refuses to replace an existing file.
		if _, serr := dst.Stat(dstPath); serr == nil && dst.Remove(dstPath) == nil {
			err = dst.Rename(part, dstPath)
		}
		if err != nil {
			_ = dst.Remove(part)
			return err
		}
	}
	if !ent.ModTime.IsZero() {
		if err := dst.Chtimes(dstPath, ent.ModTime); err != nil {
			log.Debug("preserving mtime", "path", dstPath, "err", err)
		}
	}
	if perm := ent.Mode.Perm(); perm != 0 {
		if err := dst.Chmod(dstPath, perm); err != nil {
			log.Debug("preserving mode", "path", dstPath, "err", err)
		}
	}
	return nil
}

// resolve decides what to do about an existing destination file, asking the
// UI unless the batch already has an apply-to-all answer.
func (e *Engine) resolve(it *item, path string, src, dst vfs.Entry) (Action, error) {
	b := it.batch
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.policy != nil {
		return b.policy.Action, nil
	}
	reply := make(chan Resolution, 1)
	ev := ConflictEvent{Item: it.name, Path: path, Src: src, Dst: dst, Dir: it.dir, Reply: reply, Pending: int(b.left.Load())}
	select {
	case e.events <- ev:
	case <-it.ctx.Done():
		return CancelItem, it.ctx.Err()
	}
	select {
	case res := <-reply:
		if res.ApplyToAll {
			b.policy = &res
		}
		return res.Action, nil
	case <-it.ctx.Done():
		return CancelItem, it.ctx.Err()
	}
}

// uniqueName returns "name (n).ext" for the first n that doesn't exist.
func uniqueName(fsys vfs.FS, p string) string {
	dir, base := fsys.Dir(p), fsys.Base(p)
	stem, ext := base, ""
	if i := strings.LastIndex(base, "."); i > 0 {
		stem, ext = base[:i], base[i:]
	}
	for n := 1; ; n++ {
		cand := fsys.Join(dir, stem+" ("+strconv.Itoa(n)+")"+ext)
		if _, err := fsys.Stat(cand); errors.Is(err, fs.ErrNotExist) {
			return cand
		}
	}
}

// Snapshot returns the current queue state and updates rate estimates.
func (e *Engine) Snapshot() Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now()
	var s Snapshot
	for _, it := range e.items {
		done := it.done.Load()
		if !it.state.Finished() && !it.started.IsZero() {
			if dt := now.Sub(it.lastSample).Seconds(); dt >= 0.25 {
				inst := float64(done-it.lastBytes) / dt
				if it.rate == 0 {
					it.rate = inst
				} else {
					it.rate = 0.6*it.rate + 0.4*inst
				}
				it.lastBytes, it.lastSample = done, now
			}
		}
		snap := ItemSnapshot{
			ID: it.id, Dir: it.dir, Name: it.name, IsDir: it.entry.IsDir, State: it.state,
			Done: done, Total: it.total, Files: it.files, FilesOK: it.filesOK, Skipped: it.skipped,
			Current: it.current, Rate: it.rate, Err: it.err, Failures: it.failures,
		}
		switch {
		case it.state.Finished() && !it.started.IsZero():
			snap.Elapsed = it.finished.Sub(it.started)
			if secs := snap.Elapsed.Seconds(); secs > 0 {
				snap.Rate = float64(done) / secs
			}
		case !it.started.IsZero():
			snap.Elapsed = now.Sub(it.started)
		}
		switch it.state {
		case Queued:
			s.Queued++
		case Scanning, Active:
			s.Active++
			if it.dir == Upload {
				s.UpRate += it.rate
			} else {
				s.DownRate += it.rate
			}
		case Done, Failed, Cancelled:
		}
		s.Items = append(s.Items, snap)
	}
	return s
}

// Busy reports whether anything is queued or running.
func (e *Engine) Busy() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.running > 0 || len(e.pending) > 0
}

// Cancel stops one item. Partially written files are removed.
func (e *Engine) Cancel(id int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, it := range e.items {
		if it.id == id && !it.state.Finished() {
			it.cancel()
		}
	}
}

// CancelAll stops every unfinished item.
func (e *Engine) CancelAll() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, it := range e.items {
		if !it.state.Finished() {
			it.cancel()
		}
	}
}

// Retry requeues a failed or cancelled item from scratch. Files that
// already made it across will be offered as conflicts.
func (e *Engine) Retry(id int) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, it := range e.items {
		if it.id != id || (it.state != Failed && it.state != Cancelled) {
			continue
		}
		ctx, cancel := context.WithCancel(e.ctx)
		it.ctx, it.cancel = ctx, cancel
		it.state, it.err, it.failures = Queued, nil, 0
		it.total, it.files, it.filesOK, it.skipped = 0, 0, 0, 0
		it.done.Store(0)
		it.started, it.finished, it.rate, it.lastBytes = time.Time{}, time.Time{}, 0, 0
		it.batch = &batch{}
		if e.running == 0 && len(e.pending) == 0 {
			e.busySince = time.Now()
			e.idleStats = IdleEvent{}
		}
		e.start(it)
		e.schedule()
		return true
	}
	return false
}

// ClearFinished drops finished items from the list.
func (e *Engine) ClearFinished() {
	e.mu.Lock()
	defer e.mu.Unlock()
	kept := e.items[:0]
	for _, it := range e.items {
		if !it.state.Finished() {
			kept = append(kept, it)
		}
	}
	clear(e.items[len(kept):])
	e.items = kept
}

// prune keeps the finished backlog bounded. Caller holds e.mu.
func (e *Engine) prune() {
	finished := 0
	for _, it := range e.items {
		if it.state.Finished() {
			finished++
		}
	}
	for i := 0; finished > maxFinished && i < len(e.items); {
		if e.items[i].state.Finished() {
			e.items = append(e.items[:i], e.items[i+1:]...)
			finished--
			continue
		}
		i++
	}
}

// SetConcurrency changes how many files transfer at once. Running transfers
// are never interrupted; lowering the limit takes effect as they finish.
func (e *Engine) SetConcurrency(n int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.limit = max(n, 1)
	e.schedule()
}

// SetLimit changes the speed limit for one direction immediately.
func (e *Engine) SetLimit(d Direction, r units.Rate) { setLimit(e.limiters[d], r) }

// Close cancels everything and waits for workers to stop.
func (e *Engine) Close() {
	e.cancel()
	e.wg.Wait()
}
