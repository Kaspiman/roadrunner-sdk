package events

import (
	"fmt"
	"strings"
	"sync"

	"github.com/roadrunner-server/errors"
)

type sub struct {
	pattern string
	w       *wildcard
	events  chan<- Event
}

type eventsBus struct {
	sync.RWMutex
	subscribers  sync.Map
	internalEvCh chan Event
	stop         chan struct{}
}

func newEventsBus() *eventsBus {
	return &eventsBus{
		internalEvCh: make(chan Event, 100),
		stop:         make(chan struct{}),
	}
}

/*
http.* <-
*/

// SubscribeAll for all RR events
// returns subscriptionID
func (eb *eventsBus) SubscribeAll(subID string, ch chan<- Event) error {
	if ch == nil {
		return errors.Str("nil channel provided")
	}

	subIDTr := strings.Trim(subID, " ")

	if subIDTr == "" {
		return errors.Str("subscriberID can't be empty")
	}

	return eb.subscribe(subID, "*", ch)
}

// SubscribeP pattern like "pluginName.EventType"
func (eb *eventsBus) SubscribeP(subID string, pattern string, ch chan<- Event) error {
	if ch == nil {
		return errors.Str("nil channel provided")
	}

	subIDTr := strings.Trim(subID, " ")
	patternTr := strings.Trim(pattern, " ")

	if subIDTr == "" || patternTr == "" {
		return errors.Str("subscriberID or pattern can't be empty")
	}

	return eb.subscribe(subID, pattern, ch)
}

func (eb *eventsBus) Unsubscribe(subID string) {
	eb.subscribers.Delete(subID)
}

func (eb *eventsBus) UnsubscribeP(subID, pattern string) {
	if sb, ok := eb.subscribers.Load(subID); ok {
		eb.Lock()
		defer eb.Unlock()

		sbArr := sb.([]*sub)

		for i := 0; i < len(sbArr); i++ {
			if sbArr[i].pattern == pattern {
				sbArr[i] = sbArr[len(sbArr)-1]
				sbArr = sbArr[:len(sbArr)-1]
				// replace with new array
				eb.subscribers.Store(subID, sbArr)
				return
			}
		}
	}
}

// Send sends event to the events bus
func (eb *eventsBus) Send(ev Event) {
	// do not accept nil events
	if ev == nil {
		return
	}

	eb.internalEvCh <- ev
}

func (eb *eventsBus) Len() uint {
	var ln uint

	eb.subscribers.Range(func(key, value interface{}) bool {
		ln++
		return true
	})

	return ln
}

func (eb *eventsBus) subscribe(subID string, pattern string, ch chan<- Event) error {
	eb.Lock()
	defer eb.Unlock()
	w, err := newWildcard(pattern)
	if err != nil {
		return err
	}

	if sb, ok := eb.subscribers.Load(subID); ok {
		// at this point we are confident that sb is a []*sub type
		subArr := sb.([]*sub)
		subArr = append(subArr, &sub{
			pattern: pattern,
			w:       w,
			events:  ch,
		})

		eb.subscribers.Store(subID, subArr)

		return nil
	}

	subArr := make([]*sub, 0, 1)
	subArr = append(subArr, &sub{
		pattern: pattern,
		w:       w,
		events:  ch,
	})

	eb.subscribers.Store(subID, subArr)

	return nil
}

func (eb *eventsBus) handleEvents() {
	for { //nolint:gosimple
		select {
		case ev := <-eb.internalEvCh:
			// http.WorkerError for example
			wc := fmt.Sprintf("%s.%s", ev.Plugin(), ev.Type().String())

			eb.subscribers.Range(func(key, value interface{}) bool {
				vsub := value.([]*sub)

				for i := 0; i < len(vsub); i++ {
					if vsub[i].w.match(wc) {
						select {
						case vsub[i].events <- ev:
							return true
						default:
							return true
						}
					}
				}

				return true
			})
		}
	}
}
