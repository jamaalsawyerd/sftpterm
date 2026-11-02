package transfer

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/jamaalsawyerd/sftpterm/internal/sshx/sshtest"
	"github.com/jamaalsawyerd/sftpterm/internal/units"
	"github.com/jamaalsawyerd/sftpterm/internal/vfs"
)

// rig runs an engine between two local temp dirs (src plays "local", dst
// plays "remote") and answers conflicts with answer.
type rig struct {
	t      *testing.T
	e      *Engine
	src    string
	dst    string
	answer func(ConflictEvent) Resolution

	mu        sync.Mutex
	done      map[int]ItemDoneEvent
	conflicts []ConflictEvent
	idle      chan IdleEvent
}

func newRig(t *testing.T, opts Options) *rig {
	t.Helper()
	if opts.Local == nil {
		opts.Local, opts.Remote = vfs.NewLocal(), vfs.NewLocal()
	}
	if opts.Concurrency == 0 {
		opts.Concurrency = 3
	}
	r := &rig{
		t: t, e: New(opts), src: t.TempDir(), dst: t.TempDir(),
		done: map[int]ItemDoneEvent{}, idle: make(chan IdleEvent, 16),
		answer: func(ConflictEvent) Resolution { return Resolution{Action: Skip} },
	}
	go r.loop()
	t.Cleanup(r.e.Close)
	return r
}

func (r *rig) loop() {
	for ev := range r.e.Events() {
		switch ev := ev.(type) {
		case ConflictEvent:
			r.mu.Lock()
			r.conflicts = append(r.conflicts, ev)
			answer := r.answer
			r.mu.Unlock()
			ev.Reply <- answer(ev)
		case ItemDoneEvent:
			r.mu.Lock()
			r.done[ev.ID] = ev
			r.mu.Unlock()
		case IdleEvent:
			r.idle <- ev
		}
	}
}

func (r *rig) write(rel string, data []byte, mtime time.Time) {
	r.t.Helper()
	p := filepath.Join(r.src, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		r.t.Fatal(err)
	}
	if !mtime.IsZero() {
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			r.t.Fatal(err)
		}
	}
}

func (r *rig) entries(names ...string) []vfs.Entry {
	r.t.Helper()
	var out []vfs.Entry
	for _, n := range names {
		e, err := vfs.NewLocal().Stat(filepath.Join(r.src, n))
		if err != nil {
			r.t.Fatal(err)
		}
		out = append(out, e)
	}
	return out
}

func (r *rig) upload(names ...string) []int {
	return r.e.Enqueue(Upload, r.src, r.entries(names...), r.dst)
}

func (r *rig) waitIdle() IdleEvent {
	r.t.Helper()
	select {
	case ev := <-r.idle:
		return ev
	case <-time.After(20 * time.Second):
		r.t.Fatalf("queue never went idle: %+v", r.e.Snapshot())
		return IdleEvent{}
	}
}

func (r *rig) result(id int) ItemDoneEvent {
	r.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		ev, ok := r.done[id]
		r.mu.Unlock()
		if ok {
			return ev
		}
		time.Sleep(10 * time.Millisecond)
	}
	r.t.Fatalf("item %d never finished", id)
	return ItemDoneEvent{}
}

func randBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

func readFile(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func assertNoPartFiles(t *testing.T, root string) {
	t.Helper()
	_ = filepath.WalkDir(root, func(p string, _ fs.DirEntry, err error) error {
		if err == nil && strings.HasSuffix(p, partSuffix) {
			t.Errorf("leftover partial file %s", p)
		}
		return nil
	})
}

func TestUploadFilePreservesContentAndMtime(t *testing.T) {
	r := newRig(t, Options{})
	data := randBytes(t, 300_000)
	mtime := time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)
	r.write("photo.jpg", data, mtime)

	id := r.upload("photo.jpg")[0]
	idle := r.waitIdle()
	ev := r.result(id)
	if ev.State != Done || ev.Err != nil || ev.Bytes != int64(len(data)) {
		t.Fatalf("result = %+v", ev)
	}
	if idle.Done != 1 || idle.Bytes != int64(len(data)) {
		t.Errorf("idle = %+v", idle)
	}
	dst := filepath.Join(r.dst, "photo.jpg")
	if !bytes.Equal(readFile(t, dst), data) {
		t.Fatal("content differs")
	}
	st, _ := os.Stat(dst)
	if !st.ModTime().Equal(mtime) {
		t.Errorf("mtime = %v, want %v", st.ModTime(), mtime)
	}
	assertNoPartFiles(t, r.dst)

	snap := r.e.Snapshot()
	if len(snap.Items) != 1 || snap.Items[0].Percent() != 1 || snap.Active != 0 {
		t.Errorf("snapshot = %+v", snap)
	}
}

func TestDirectoryCopyIsRecursive(t *testing.T) {
	r := newRig(t, Options{Concurrency: 4})
	files := map[string]int{
		"album/a.flac": 1000, "album/b.flac": 2000, "album/disc2/c.flac": 3000,
		"album/disc2/deep/d.txt": 10, "album/.hidden": 5,
	}
	for name, n := range files {
		r.write(name, randBytes(t, n), time.Time{})
	}
	if err := os.Mkdir(filepath.Join(r.src, "album", "empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	id := r.upload("album")[0]
	r.waitIdle()
	ev := r.result(id)
	if ev.State != Done || ev.Files != len(files) || ev.Bytes != 6015 {
		t.Fatalf("result = %+v", ev)
	}
	for name := range files {
		if !bytes.Equal(readFile(t, filepath.Join(r.dst, name)), readFile(t, filepath.Join(r.src, name))) {
			t.Errorf("%s differs", name)
		}
	}
	if st, err := os.Stat(filepath.Join(r.dst, "album", "empty")); err != nil || !st.IsDir() {
		t.Error("empty directory not created")
	}
}

func TestConflictResolutions(t *testing.T) {
	tests := []struct {
		action   Action
		wantDst  string
		wantCopy string // content of "notes (1).txt", if any
	}{
		{Skip, "old", ""},
		{Overwrite, "new", ""},
		{KeepBoth, "old", "new"},
	}
	for _, tt := range tests {
		t.Run(strings.ToLower(map[Action]string{Skip: "skip", Overwrite: "overwrite", KeepBoth: "keepboth"}[tt.action]), func(t *testing.T) {
			r := newRig(t, Options{})
			r.answer = func(ConflictEvent) Resolution { return Resolution{Action: tt.action} }
			r.write("notes.txt", []byte("new"), time.Time{})
			if err := os.WriteFile(filepath.Join(r.dst, "notes.txt"), []byte("old"), 0o644); err != nil {
				t.Fatal(err)
			}
			id := r.upload("notes.txt")[0]
			r.waitIdle()
			if ev := r.result(id); ev.State != Done {
				t.Fatalf("result = %+v", ev)
			}
			if got := string(readFile(t, filepath.Join(r.dst, "notes.txt"))); got != tt.wantDst {
				t.Errorf("dst = %q, want %q", got, tt.wantDst)
			}
			copyPath := filepath.Join(r.dst, "notes (1).txt")
			if tt.wantCopy == "" {
				if _, err := os.Stat(copyPath); err == nil {
					t.Error("unexpected copy created")
				}
			} else if got := string(readFile(t, copyPath)); got != tt.wantCopy {
				t.Errorf("copy = %q, want %q", got, tt.wantCopy)
			}
			if len(r.conflicts) != 1 {
				t.Errorf("conflicts = %d, want 1", len(r.conflicts))
			}
		})
	}
}

func TestApplyToAllAsksOnce(t *testing.T) {
	r := newRig(t, Options{Concurrency: 3})
	r.answer = func(ConflictEvent) Resolution { return Resolution{Action: Overwrite, ApplyToAll: true} }
	for _, n := range []string{"1.txt", "2.txt", "3.txt", "4.txt"} {
		r.write("dir/"+n, []byte("new"), time.Time{})
		if err := os.MkdirAll(filepath.Join(r.dst, "dir"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(r.dst, "dir", n), []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	id := r.upload("dir")[0]
	r.waitIdle()
	if ev := r.result(id); ev.State != Done || ev.Files != 4 {
		t.Fatalf("result = %+v", ev)
	}
	r.mu.Lock()
	n := len(r.conflicts)
	r.mu.Unlock()
	if n != 1 {
		t.Errorf("asked %d times, want 1", n)
	}
	for _, f := range []string{"1.txt", "4.txt"} {
		if got := string(readFile(t, filepath.Join(r.dst, "dir", f))); got != "new" {
			t.Errorf("%s = %q, want overwritten", f, got)
		}
	}
}

func TestCancelRemovesPartialFile(t *testing.T) {
	r := newRig(t, Options{UploadLimit: units.Rate(64 * units.KiB)})
	r.write("big.bin", randBytes(t, 2*int(units.MiB)), time.Time{})
	id := r.upload("big.bin")[0]

	deadline := time.Now().Add(5 * time.Second)
	for r.e.Snapshot().Items[0].Done == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	r.e.Cancel(id)
	ev := r.result(id)
	if ev.State != Cancelled {
		t.Fatalf("state = %v (%v), want cancelled", ev.State, ev.Err)
	}
	if _, err := os.Stat(filepath.Join(r.dst, "big.bin")); err == nil {
		t.Error("destination file exists after cancel")
	}
	assertNoPartFiles(t, r.dst)
}

func TestSpeedLimitIsHonored(t *testing.T) {
	limit := 256 * units.KiB
	r := newRig(t, Options{UploadLimit: units.Rate(limit)})
	r.write("f.bin", randBytes(t, int(limit*2)), time.Time{}) // burst + 1s
	start := time.Now()
	r.upload("f.bin")
	r.waitIdle()
	if elapsed := time.Since(start); elapsed < 800*time.Millisecond {
		t.Errorf("finished in %v; limit not applied", elapsed)
	}

	// Lifting the limit takes effect immediately.
	r.e.SetLimit(Upload, 0)
	r.write("g.bin", randBytes(t, int(limit*8)), time.Time{})
	start = time.Now()
	r.upload("g.bin")
	r.waitIdle()
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("unlimited transfer took %v", elapsed)
	}
}

func TestConcurrencyLimit(t *testing.T) {
	r := newRig(t, Options{Concurrency: 2, UploadLimit: units.Rate(512 * units.KiB)})
	for _, n := range []string{"a", "b", "c", "d", "e"} {
		r.write(n, randBytes(t, 200_000), time.Time{})
	}
	r.upload("a", "b", "c", "d", "e")
	maxActive := 0
	for r.e.Busy() {
		maxActive = max(maxActive, r.e.Snapshot().Active)
		time.Sleep(5 * time.Millisecond)
	}
	r.waitIdle()
	if maxActive > 2 {
		t.Errorf("saw %d active at once, limit 2", maxActive)
	}
	if maxActive == 0 {
		t.Error("never observed any activity")
	}
}

func TestFailureAndRetry(t *testing.T) {
	r := newRig(t, Options{})
	r.write("x.txt", []byte("data"), time.Time{})
	entries := r.entries("x.txt")
	if err := os.Remove(filepath.Join(r.src, "x.txt")); err != nil {
		t.Fatal(err)
	}
	id := r.e.Enqueue(Upload, r.src, entries, r.dst)[0]
	idle := r.waitIdle()
	ev := r.result(id)
	if ev.State != Failed || !errors.Is(ev.Err, fs.ErrNotExist) || idle.Failed != 1 {
		t.Fatalf("result = %+v, idle = %+v", ev, idle)
	}

	r.write("x.txt", []byte("data"), time.Time{})
	r.mu.Lock()
	delete(r.done, id)
	r.mu.Unlock()
	if !r.e.Retry(id) {
		t.Fatal("Retry refused")
	}
	r.waitIdle()
	if ev := r.result(id); ev.State != Done {
		t.Fatalf("after retry: %+v", ev)
	}
}

func TestUniqueName(t *testing.T) {
	dir := t.TempDir()
	lfs := vfs.NewLocal()
	for _, n := range []string{"a.txt", "a (1).txt", "noext"} {
		if err := os.WriteFile(filepath.Join(dir, n), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := filepath.Base(uniqueName(lfs, filepath.Join(dir, "a.txt"))); got != "a (2).txt" {
		t.Errorf("got %q", got)
	}
	if got := filepath.Base(uniqueName(lfs, filepath.Join(dir, "noext"))); got != "noext (1)" {
		t.Errorf("got %q", got)
	}
	if got := filepath.Base(uniqueName(lfs, filepath.Join(dir, ".bashrc"))); got != ".bashrc (1)" {
		t.Errorf("got %q", got)
	}
}

func TestSFTPRoundTrip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the test SFTP server exposes Windows paths")
	}
	srv := sshtest.New(t, sshtest.Options{Password: "pw"})
	client, err := ssh.Dial("tcp", srv.Addr, &ssh.ClientConfig{
		User: srv.User, Auth: []ssh.AuthMethod{ssh.Password("pw")},
		HostKeyCallback: ssh.FixedHostKey(srv.HostKey.PublicKey()),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	sc, err := sftp.NewClient(client, sftp.UseConcurrentReads(true), sftp.UseConcurrentWrites(true))
	if err != nil {
		t.Fatal(err)
	}
	defer sc.Close()
	remote, err := vfs.NewSFTP(sc, "test")
	if err != nil {
		t.Fatal(err)
	}

	r := newRig(t, Options{Local: vfs.NewLocal(), Remote: remote})
	data := randBytes(t, 3*int(units.MiB)+123)
	mtime := time.Date(2023, 1, 2, 3, 4, 5, 0, time.UTC)
	r.write("tree/big.bin", data, mtime)
	r.write("tree/sub/small.txt", []byte("small"), time.Time{})

	home, _ := remote.Home()
	r.e.Enqueue(Upload, r.src, r.entries("tree"), home)
	if idle := r.waitIdle(); idle.Done != 1 || idle.Failed != 0 {
		t.Fatalf("upload idle = %+v; snapshot %+v", idle, r.e.Snapshot())
	}
	if !bytes.Equal(readFile(t, filepath.Join(srv.Root, "tree", "big.bin")), data) {
		t.Fatal("uploaded content differs")
	}

	back := t.TempDir()
	treeEntry, err := remote.Stat(remote.Join(home, "tree"))
	if err != nil {
		t.Fatal(err)
	}
	r.e.Enqueue(Download, home, []vfs.Entry{treeEntry}, back)
	if idle := r.waitIdle(); idle.Done != 1 {
		t.Fatalf("download idle = %+v", idle)
	}
	got := readFile(t, filepath.Join(back, "tree", "big.bin"))
	if !bytes.Equal(got, data) {
		t.Fatal("downloaded content differs")
	}
	st, _ := os.Stat(filepath.Join(back, "tree", "big.bin"))
	if !st.ModTime().Equal(mtime) {
		t.Errorf("mtime after round trip = %v, want %v", st.ModTime(), mtime)
	}
	if string(readFile(t, filepath.Join(back, "tree", "sub", "small.txt"))) != "small" {
		t.Error("small file differs")
	}
	assertNoPartFiles(t, srv.Root)
	assertNoPartFiles(t, back)
}
