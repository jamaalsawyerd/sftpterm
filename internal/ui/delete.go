package ui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/log/v2"

	"github.com/jamaalsawyerd/sftpterm/internal/ui/pane"
	"github.com/jamaalsawyerd/sftpterm/internal/units"
	"github.com/jamaalsawyerd/sftpterm/internal/vfs"
)

// deleteTickInterval is how often a running delete redraws its progress.
const deleteTickInterval = 100 * time.Millisecond

// spinnerFrames animate the delete status while it runs.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// deleteJob deletes entries from one directory in the background. The
// worker records progress under mu; the UI reads it on each tick, so a fast
// local delete of thousands of files doesn't flood the event loop.
type deleteJob struct {
	id      int
	fsys    vfs.FS
	dir     string
	what    string   // "notes.txt" or "5 items", for messages
	names   []string // the entries being deleted, in order
	started time.Time

	mu       sync.Mutex
	removed  int             // files and folders deleted so far, at any depth
	current  string          // last path deleted, relative to dir
	finished []string        // names fully deleted and not yet dropped from the panes
	pending  map[string]bool // names not yet finished, successfully or not
}

type deleteTickMsg struct{ id int }

type deleteDoneMsg struct {
	id   int
	errs []error
}

func newDeleteJob(id int, fsys vfs.FS, dir, what string, entries []vfs.Entry) *deleteJob {
	j := &deleteJob{id: id, fsys: fsys, dir: dir, what: what, started: time.Now(), pending: map[string]bool{}}
	for _, e := range entries {
		j.names = append(j.names, e.Name)
		j.pending[e.Name] = true
	}
	return j
}

// run deletes every entry, carrying on past failures so one unreadable
// folder doesn't stop the rest.
func (j *deleteJob) run() tea.Msg {
	var errs []error
	for _, name := range j.names {
		err := vfs.RemoveAllFunc(j.fsys, j.fsys.Join(j.dir, name), j.onRemoved)
		j.mu.Lock()
		delete(j.pending, name)
		if err == nil {
			j.finished = append(j.finished, name)
		}
		j.mu.Unlock()
		if err != nil {
			errs = append(errs, err)
		}
	}
	log.Debug("deleted", "count", len(j.names), "dir", j.dir, "errors", len(errs))
	return deleteDoneMsg{id: j.id, errs: errs}
}

func (j *deleteJob) onRemoved(p string) {
	rel := strings.TrimLeft(strings.TrimPrefix(p, j.dir), `/\`)
	j.mu.Lock()
	j.removed++
	j.current = rel
	j.mu.Unlock()
}

// progress returns the counts so far and takes the names finished since the
// last call.
func (j *deleteJob) progress() (removed int, finished, pending []string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	finished, j.finished = j.finished, nil
	for _, n := range j.names {
		if j.pending[n] {
			pending = append(pending, n)
		}
	}
	return j.removed, finished, pending
}

func (j *deleteJob) tick() tea.Cmd {
	id := j.id
	return tea.Tick(deleteTickInterval, func(time.Time) tea.Msg { return deleteTickMsg{id: id} })
}

// startDeleteJob begins deleting entries from dir and shows them as busy.
func (m Model) startDeleteJob(fsys vfs.FS, dir, what string, entries []vfs.Entry) (tea.Model, tea.Cmd) {
	m.deleteSeq++
	j := newDeleteJob(m.deleteSeq, fsys, dir, what, entries)
	m.deleting = j
	for _, p := range m.panesShowing(fsys) {
		p.ClearMarks()
		p.SetBusy(dir, j.names...)
	}
	return m, tea.Batch(j.run, j.tick())
}

// panesShowing returns the panes browsing fsys.
func (m *Model) panesShowing(fsys vfs.FS) []*pane.Model {
	var out []*pane.Model
	for _, p := range []*pane.Model{&m.local, &m.remote} {
		if p.FS() != nil && p.FS() == fsys {
			out = append(out, p)
		}
	}
	return out
}

// applyDeleteProgress drops finished entries from the panes, so they vanish
// one by one as they go, and refreshes the busy markers.
func (m *Model) applyDeleteProgress(j *deleteJob) {
	_, finished, pending := j.progress()
	for _, p := range m.panesShowing(j.fsys) {
		p.Drop(j.dir, finished...)
		p.SetBusy(j.dir, pending...)
	}
}

func (m Model) onDeleteTick(msg deleteTickMsg) (tea.Model, tea.Cmd) {
	j := m.deleting
	if j == nil || j.id != msg.id {
		return m, nil
	}
	m.applyDeleteProgress(j)
	return m, j.tick()
}

func (m Model) onDeleteDone(msg deleteDoneMsg) (tea.Model, tea.Cmd) {
	j := m.deleting
	if j == nil || j.id != msg.id {
		return m, nil
	}
	m.applyDeleteProgress(j)
	m.deleting = nil
	removed, _, _ := j.progress()

	var cmds []tea.Cmd
	for _, p := range m.panesShowing(j.fsys) {
		p.SetBusy("")
		if p.Path() == j.dir { // pick up anything a failure left behind
			cmds = append(cmds, p.Navigate(j.dir, ""))
		}
	}

	if len(msg.errs) > 0 {
		text := vfs.Sanitize(errors.Join(msg.errs...).Error())
		if len(j.names) > 1 {
			text = fmt.Sprintf("couldn't delete %d of %d: %s", len(msg.errs), len(j.names), vfs.Sanitize(msg.errs[0].Error()))
		}
		return m, tea.Batch(append(cmds, m.setNotice(noticeError, text, errorNoticeTTL))...)
	}
	text := "✓ deleted " + j.what
	if removed > len(j.names) {
		text += " (" + strconv.Itoa(removed) + " files and folders)"
	}
	if elapsed := time.Since(j.started); elapsed >= time.Second {
		text += " in " + units.FormatDuration(elapsed)
	}
	return m, tea.Batch(append(cmds, m.setNotice(noticeInfo, text, noticeTTL))...)
}

// deleteStatus describes a running delete for the status bar, e.g.
// "⠹ deleting 2 of 5 · 143 removed · photos/2019/img.jpg".
func (m Model) deleteStatus() string {
	j := m.deleting
	if j == nil {
		return ""
	}
	j.mu.Lock()
	removed, current, left := j.removed, j.current, len(j.pending)
	j.mu.Unlock()

	frame := int(time.Since(j.started)/deleteTickInterval) % len(spinnerFrames)
	parts := []string{spinnerFrames[frame] + " deleting " + j.what}
	if total := len(j.names); total > 1 {
		parts[0] = fmt.Sprintf("%s deleting %d of %d", spinnerFrames[frame], min(total-left+1, total), total)
	}
	if removed > 0 {
		parts = append(parts, strconv.Itoa(removed)+" removed")
	}
	if current != "" {
		parts = append(parts, vfs.Sanitize(current))
	}
	return strings.Join(parts, " · ")
}
