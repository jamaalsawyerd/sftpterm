// Package transfer copies files and directories between two file systems
// with a bounded worker pool, shared speed limits, conflict resolution, and
// progress snapshots for the UI.
package transfer

import (
	"time"

	"github.com/jamaalsawyerd/sftpterm/internal/vfs"
)

// Direction says which way bytes flow.
type Direction int

// Directions.
const (
	Upload   Direction = iota // local → remote
	Download                  // remote → local
)

func (d Direction) String() string {
	if d == Download {
		return "download"
	}
	return "upload"
}

// State is an item's lifecycle stage.
type State int

// Item states.
const (
	Queued State = iota
	Scanning
	Active
	Done
	Failed
	Cancelled
)

func (s State) String() string {
	return [...]string{"queued", "scanning", "active", "done", "failed", "cancelled"}[s]
}

// Finished reports whether the item will make no further progress.
func (s State) Finished() bool { return s >= Done }

// Action is how to resolve a name conflict at the destination.
type Action int

// Conflict actions.
const (
	Overwrite Action = iota
	Skip
	KeepBoth // write under a new name like "file (1).txt"
	CancelItem
)

// Resolution answers a ConflictEvent.
type Resolution struct {
	Action Action
	// ApplyToAll reuses the action for the rest of the batch.
	ApplyToAll bool
}

// Event is sent on Engine.Events.
type Event interface{ isEvent() }

// ConflictEvent asks what to do about a file that already exists. The engine
// waits for exactly one value on Reply.
type ConflictEvent struct {
	Item    string
	Path    string // destination path
	Src     vfs.Entry
	Dst     vfs.Entry
	Dir     Direction
	Reply   chan<- Resolution
	Pending int // other files in the batch that may still conflict
}

// ItemDoneEvent reports that a top-level item finished.
type ItemDoneEvent struct {
	ID      int
	Name    string
	Dir     Direction
	DstDir  string // directory the item was copied into
	State   State
	Err     error
	Bytes   int64
	Files   int
	Skipped int
	Elapsed time.Duration
}

// IdleEvent reports that the queue drained.
type IdleEvent struct {
	Done, Failed int
	Bytes        int64
	Elapsed      time.Duration
}

func (ConflictEvent) isEvent() {}
func (ItemDoneEvent) isEvent() {}
func (IdleEvent) isEvent()     {}

// ItemSnapshot is a point-in-time view of one item for display.
type ItemSnapshot struct {
	ID       int
	Dir      Direction
	Name     string
	IsDir    bool
	State    State
	Done     int64
	Total    int64
	Files    int
	FilesOK  int
	Skipped  int
	Current  string  // file being transferred, for directories
	Rate     float64 // bytes/second
	Err      error
	Elapsed  time.Duration
	Failures int
}

// Percent returns completion in [0, 1].
func (s ItemSnapshot) Percent() float64 {
	switch {
	case s.State == Done:
		return 1
	case s.Total <= 0:
		return 0
	}
	return min(float64(s.Done)/float64(s.Total), 1)
}

// ETA estimates time remaining, or 0 when unknown.
func (s ItemSnapshot) ETA() time.Duration {
	if s.Rate <= 0 || s.Total <= s.Done {
		return 0
	}
	return time.Duration(float64(s.Total-s.Done) / s.Rate * float64(time.Second))
}

// Snapshot is the whole queue.
type Snapshot struct {
	Items    []ItemSnapshot
	Active   int // items scanning or transferring
	Queued   int
	UpRate   float64
	DownRate float64
}
