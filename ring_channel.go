package channels

import "github.com/eapache/queue"

// RingChannel implements the Channel interface in a way that never blocks the writer.
// Specifically, if a value is written to a RingChannel when its buffer is full then the oldest
// value in the buffer is discarded to make room (just like a standard ring-buffer).
// Note that Go's scheduler can cause discarded values when they could be avoided, simply by scheduling
// the writer before the reader, so caveat emptor.
// For the opposite behaviour (discarding the newest element, not the oldest) see OverflowingChannel.
type RingChannel struct {
	input, output chan interface{}
	buffer        *queue.Queue
	size          BufferCap
}

func NewRingChannel(size BufferCap) *RingChannel {
	if size < 0 && size != Infinity {
		panic("channels: invalid negative size in NewRingChannel")
	}
	ch := &RingChannel{make(chan interface{}), make(chan interface{}), queue.New(), size}
	if size == None {
		go ch.overflowingDirect()
	} else {
		go ch.ringBuffer()
	}
	return ch
}

func (ch *RingChannel) In() chan<- interface{} {
	return ch.input
}

func (ch *RingChannel) Out() <-chan interface{} {
	return ch.output
}

func (ch *RingChannel) Len() int {
	return ch.buffer.Length()
}

func (ch *RingChannel) Cap() BufferCap {
	return ch.size
}

func (ch *RingChannel) Close() {
	close(ch.input)
}

func (ch *RingChannel) shutdown() {
	for ch.buffer.Length() > 0 {
		ch.output <- ch.buffer.Peek()
		ch.buffer.Remove()
	}
	close(ch.output)
}

// for entirely unbuffered cases
func (ch *RingChannel) overflowingDirect() {
	for elem := range ch.input {
		// if we can't write it immediately, drop it and move on
		select {
		case ch.output <- elem:
		default:
		}
	}
	close(ch.output)
}

// for all buffered cases
func (ch *RingChannel) ringBuffer() {
	for {
		if ch.buffer.Length() == 0 {
			elem, open := <-ch.input
			if open {
				ch.buffer.Add(elem)
			} else {
				ch.shutdown()
				return
			}
		} else {
			select {
			// Prefer to write if possible, which is surprisingly effective in reducing
			// dropped elements due to overflow. The naive read/write select chooses randomly
			// when both channels are ready, which produces unnecessary drops 50% of the time.
			case ch.output <- ch.buffer.Peek():
				ch.buffer.Remove()
			default:
				select {
				case elem, open := <-ch.input:
					if open {
						ch.buffer.Add(elem)
						if ch.size != Infinity && ch.buffer.Length() > int(ch.size) {
							ch.buffer.Remove()
						}
					} else {
						ch.shutdown()
						return
					}
				case ch.output <- ch.buffer.Peek():
					ch.buffer.Remove()
				}
			}
		}
	}
}
