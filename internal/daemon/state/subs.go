package state

import (
	"iter"
	"slices"
)

// Subs is a list of subscriber callbacks keyed by id, so a canceled one is
// removed instead of leaving a nil slot behind (every client connection
// subscribes; nil slots made the lists grow for the daemon's lifetime and
// every broadcast walk them). It has no lock of its own: the owner guards
// it with the mutex that already orders its subscribe, cancel and
// broadcast.
type Subs[F any] struct {
	next uint64
	list []sub[F]
}

type sub[F any] struct {
	id uint64
	fn F
}

// Add registers fn and returns the id that removes it.
func (s *Subs[F]) Add(fn F) uint64 {
	s.next++
	s.list = append(s.list, sub[F]{id: s.next, fn: fn})
	return s.next
}

// Remove drops the subscriber with id; an unknown id is a no-op.
func (s *Subs[F]) Remove(id uint64) {
	s.list = slices.DeleteFunc(s.list, func(e sub[F]) bool { return e.id == id })
}

// Each calls f with every subscriber, in subscription order.
func (s *Subs[F]) Each(f func(F)) {
	for fn := range s.All() {
		f(fn)
	}
}

// All yields every subscriber, in subscription order.
func (s *Subs[F]) All() iter.Seq[F] {
	return func(yield func(F) bool) {
		if s == nil {
			return
		}
		for _, e := range s.list {
			if !yield(e.fn) {
				return
			}
		}
	}
}

// Clone returns the subscribers, for a caller that calls them after
// releasing its lock.
func (s *Subs[F]) Clone() []F {
	out := make([]F, len(s.list))
	for i, e := range s.list {
		out[i] = e.fn
	}
	return out
}

// Len is the number of subscribers; 0 for a nil list.
func (s *Subs[F]) Len() int {
	if s == nil {
		return 0
	}
	return len(s.list)
}
