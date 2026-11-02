package transfer

import (
	"context"
	"io"
	"math"
	"sync/atomic"

	"golang.org/x/time/rate"

	"github.com/jamaalsawyerd/sftpterm/internal/units"
)

// chunkSize bounds each limiter reservation; it matches the SFTP packet size.
const chunkSize = 32 * 1024

func newLimiter(r units.Rate) *rate.Limiter {
	l := rate.NewLimiter(rate.Inf, chunkSize)
	setLimit(l, r)
	return l
}

func setLimit(l *rate.Limiter, r units.Rate) {
	if r.Unlimited() {
		l.SetLimit(rate.Inf)
		return
	}
	// A small burst (a tenth of a second's worth) keeps throughput smooth
	// and stops small files from slipping through above the limit.
	l.SetBurst(int(min(max(int64(r)/10, chunkSize), math.MaxInt32)))
	l.SetLimit(rate.Limit(r))
}

// meter counts bytes into an item and applies the shared speed limit. It
// also counts this file's bytes separately, so a failed file's partial
// progress can be taken back out of the item total.
type meter struct {
	ctx     context.Context
	limiter *rate.Limiter
	done    *atomic.Int64 // item total, shared by concurrent files
	file    *atomic.Int64 // this file only
}

func (m meter) add(n int) {
	m.done.Add(int64(n))
	m.file.Add(int64(n))
}

func (m meter) wait(n int) error {
	if err := m.ctx.Err(); err != nil {
		return err
	}
	if m.limiter.Limit() == rate.Inf {
		return nil
	}
	return m.limiter.WaitN(m.ctx, n)
}

// meteredReader wraps a source. Size lets pkg/sftp's File.ReadFrom pick a
// concurrent upload strategy.
type meteredReader struct {
	meter
	r      io.Reader
	remain int64
}

func (r *meteredReader) Read(p []byte) (int, error) {
	if len(p) > chunkSize {
		p = p[:chunkSize]
	}
	if err := r.wait(len(p)); err != nil {
		return 0, err
	}
	n, err := r.r.Read(p)
	r.add(n)
	r.remain -= int64(n)
	return n, err
}

func (r *meteredReader) Size() int64 { return max(r.remain, 0) }

// meteredWriter wraps a destination.
type meteredWriter struct {
	meter
	w io.Writer
}

func (w *meteredWriter) Write(p []byte) (int, error) {
	written := 0
	for len(p) > 0 {
		chunk := p[:min(len(p), chunkSize)]
		if err := w.wait(len(chunk)); err != nil {
			return written, err
		}
		n, err := w.w.Write(chunk)
		written += n
		w.add(n)
		if err != nil {
			return written, err
		}
		p = p[n:]
	}
	return written, nil
}

// copyData moves src to dst while metering, using whichever side has a fast
// path: pkg/sftp's File.WriteTo for downloads and File.ReadFrom for uploads
// both pipeline many requests at once instead of one round trip per chunk.
func copyData(dst io.Writer, src io.Reader, dir Direction, size int64, m meter) error {
	var err error
	switch wt, rf := src.(io.WriterTo), dst.(io.ReaderFrom); {
	case dir == Download && wt != nil:
		_, err = wt.WriteTo(&meteredWriter{meter: m, w: dst})
	case dir == Upload && rf != nil:
		_, err = rf.ReadFrom(&meteredReader{meter: m, r: src, remain: size})
	default:
		_, err = io.Copy(&meteredWriter{meter: m, w: dst}, src)
	}
	if err == nil {
		err = m.ctx.Err()
	}
	return err
}
