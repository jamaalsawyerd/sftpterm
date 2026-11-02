package connect

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/jamaalsawyerd/sftpterm/internal/sshx"
)

// Events flow from the connecting goroutine to the UI over a channel. The UI
// reads one event at a time; prompts block the goroutine until answered.

type progressEvent struct{ stage string }

type askEvent struct {
	q     sshx.Question
	reply chan<- answer
}

type hostKeyEvent struct {
	info  sshx.HostKeyInfo
	reply chan<- bool
}

type doneEvent struct {
	conn *sshx.Conn
	err  error
}

type answer struct {
	text string
	ok   bool
}

// eventMsg wraps an event with the attempt it belongs to, so events from a
// cancelled attempt can be told apart.
type eventMsg struct {
	attempt int
	event   any
}

func waitForEvent(attempt int, ch <-chan any) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return eventMsg{attempt: attempt, event: ev}
	}
}

// bridge implements sshx.Prompter by sending events to the UI.
type bridge struct {
	ch chan<- any
}

func (b bridge) send(ctx context.Context, ev any) bool {
	select {
	case b.ch <- ev:
		return true
	case <-ctx.Done():
		return false
	}
}

func (b bridge) progress(ctx context.Context) func(string) {
	return func(stage string) { b.send(ctx, progressEvent{stage: stage}) }
}

// Ask implements sshx.Prompter.
func (b bridge) Ask(ctx context.Context, q sshx.Question) (string, error) {
	reply := make(chan answer, 1)
	if !b.send(ctx, askEvent{q: q, reply: reply}) {
		return "", ctx.Err()
	}
	select {
	case a := <-reply:
		if !a.ok {
			return "", sshx.ErrCancelled
		}
		return a.text, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// ConfirmHostKey implements sshx.Prompter.
func (b bridge) ConfirmHostKey(ctx context.Context, info sshx.HostKeyInfo) (bool, error) {
	reply := make(chan bool, 1)
	if !b.send(ctx, hostKeyEvent{info: info, reply: reply}) {
		return false, ctx.Err()
	}
	select {
	case ok := <-reply:
		return ok, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// run connects in the background, reporting through ch, and closes ch when
// finished. If the attempt is cancelled after succeeding, the connection is
// closed rather than leaked.
func run(ctx context.Context, t sshx.Target, ch chan any) {
	defer close(ch)
	b := bridge{ch: ch}
	conn, err := sshx.Connect(ctx, t, sshx.Options{Prompter: b, Progress: b.progress(ctx)})
	if !b.send(ctx, doneEvent{conn: conn, err: err}) && conn != nil {
		_ = conn.Close()
	}
}
