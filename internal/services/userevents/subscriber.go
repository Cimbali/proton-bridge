// Copyright (c) 2023 Proton AG
//
// This file is part of Proton Mail Bridge.
//
// Proton Mail Bridge is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Proton Mail Bridge is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with Proton Mail Bridge.  If not, see <https://www.gnu.org/licenses/>.

package userevents

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"time"

	"github.com/ProtonMail/gluon/async"
	"github.com/ProtonMail/go-proton-api"
	"github.com/bradenaw/juniper/parallel"
	"github.com/bradenaw/juniper/xslices"
	"golang.org/x/exp/slices"
)

type AddressChanneledSubscriber = ChanneledSubscriber[[]proton.AddressEvent]
type LabelChanneledSubscriber = ChanneledSubscriber[[]proton.LabelEvent]
type MessageChanneledSubscriber = ChanneledSubscriber[[]proton.MessageEvent]
type UserChanneledSubscriber = ChanneledSubscriber[proton.User]
type RefreshChanneledSubscriber = ChanneledSubscriber[proton.RefreshFlag]
type UserUsedSpaceChanneledSubscriber = ChanneledSubscriber[int]

func NewMessageSubscriber(name string) *MessageChanneledSubscriber {
	return newChanneledSubscriber[[]proton.MessageEvent](name)
}

func NewAddressSubscriber(name string) *AddressChanneledSubscriber {
	return newChanneledSubscriber[[]proton.AddressEvent](name)
}

func NewLabelSubscriber(name string) *LabelChanneledSubscriber {
	return newChanneledSubscriber[[]proton.LabelEvent](name)
}

func NewRefreshSubscriber(name string) *RefreshChanneledSubscriber {
	return newChanneledSubscriber[proton.RefreshFlag](name)
}

func NewUserSubscriber(name string) *UserChanneledSubscriber {
	return newChanneledSubscriber[proton.User](name)
}

func NewUserUsedSpaceSubscriber(name string) *UserUsedSpaceChanneledSubscriber {
	return newChanneledSubscriber[int](name)
}

type AddressSubscriber = subscriber[[]proton.AddressEvent]
type LabelSubscriber = subscriber[[]proton.LabelEvent]
type MessageSubscriber = subscriber[[]proton.MessageEvent]
type RefreshSubscriber = subscriber[proton.RefreshFlag]
type UserSubscriber = subscriber[proton.User]
type UserUsedSpaceSubscriber = subscriber[int]

// Subscriber is the main entry point of interacting with user generated events.
type subscriber[T any] interface {
	// Name returns the identifier for this subscriber
	name() string
	// Handle the event list.
	handle(context.Context, T) error
	// cancel is behavior extension for channel based subscribers so that they can ensure that
	// if a subscriber unsubscribes, it doesn't cause pending events on the channel to time-out as there is no one to handle
	// them.
	cancel()
	// close release all associated resources
	close()
}

type subscriberList[T any] struct {
	subscribers []subscriber[T]
}

type addressSubscriberList = subscriberList[[]proton.AddressEvent]
type labelSubscriberList = subscriberList[[]proton.LabelEvent]
type messageSubscriberList = subscriberList[[]proton.MessageEvent]
type refreshSubscriberList = subscriberList[proton.RefreshFlag]
type userSubscriberList = subscriberList[proton.User]
type userUsedSpaceSubscriberList = subscriberList[int]

func (s *subscriberList[T]) Add(subscriber subscriber[T]) {
	if !slices.Contains(s.subscribers, subscriber) {
		s.subscribers = append(s.subscribers, subscriber)
	}
}

func (s *subscriberList[T]) Remove(subscriber subscriber[T]) {
	index := slices.Index(s.subscribers, subscriber)
	if index < 0 {
		return
	}

	s.subscribers[index].close()
	s.subscribers = xslices.Remove(s.subscribers, index, 1)
}

type publishError[T any] struct {
	subscriber subscriber[T]
	error      error
}

var ErrPublishTimeoutExceeded = errors.New("event publish timed out")

type addressPublishError = publishError[[]proton.AddressEvent]
type labelPublishError = publishError[[]proton.LabelEvent]
type messagePublishError = publishError[[]proton.MessageEvent]
type refreshPublishError = publishError[proton.RefreshFlag]
type userPublishError = publishError[proton.User]
type userUsedEventPublishError = publishError[int]

func (p publishError[T]) Error() string {
	return fmt.Sprintf("Event publish failed on (%v): %v", p.subscriber.name(), p.error.Error())
}

func (s *subscriberList[T]) Publish(ctx context.Context, event T, timeout time.Duration) error {
	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(timeout))
	defer cancel()

	for _, subscriber := range s.subscribers {
		if err := subscriber.handle(ctx, event); err != nil {
			return &publishError[T]{
				subscriber: subscriber,
				error:      mapContextTimeoutError(err),
			}
		}

		if err := ctx.Err(); err != nil {
			return &publishError[T]{
				subscriber: subscriber,
				error:      mapContextTimeoutError(err),
			}
		}
	}

	return nil
}

func mapContextTimeoutError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return ErrPublishTimeoutExceeded
	}

	return err
}

func (s *subscriberList[T]) PublishParallel(
	ctx context.Context,
	event T,
	panicHandler async.PanicHandler,
	timeout time.Duration,
) error {
	if len(s.subscribers) <= 1 {
		return s.Publish(ctx, event, timeout)
	}

	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(timeout))
	defer cancel()

	err := parallel.DoContext(ctx, runtime.NumCPU()/2, len(s.subscribers), func(ctx context.Context, index int) error {
		defer async.HandlePanic(panicHandler)
		if err := s.subscribers[index].handle(ctx, event); err != nil {
			return &publishError[T]{
				subscriber: s.subscribers[index],
				error:      mapContextTimeoutError(err),
			}
		}

		return nil
	})

	return mapContextTimeoutError(err)
}

type ChanneledSubscriber[T any] struct {
	id     string
	sender chan *ChanneledSubscriberEvent[T]
}

func newChanneledSubscriber[T any](name string) *ChanneledSubscriber[T] {
	return &ChanneledSubscriber[T]{
		id:     name,
		sender: make(chan *ChanneledSubscriberEvent[T]),
	}
}

type ChanneledSubscriberEvent[T any] struct {
	data     T
	response chan error
}

func (c ChanneledSubscriberEvent[T]) Consume(f func(T) error) {
	if err := f(c.data); err != nil {
		c.response <- err
	}
	close(c.response)
}

func (c *ChanneledSubscriber[T]) name() string { //nolint:unused
	return c.id
}

func (c *ChanneledSubscriber[T]) handle(ctx context.Context, event T) error { //nolint:unused
	data := &ChanneledSubscriberEvent[T]{
		data:     event,
		response: make(chan error),
	}
	// Send Event
	select {
	case <-ctx.Done():
		return fmt.Errorf("failed to send event: %w", ctx.Err())
	case c.sender <- data:
		//
	}

	// Wait on Reply
	select {
	case <-ctx.Done():
		return fmt.Errorf("failed to receive event reply: %w", ctx.Err())
	case reply := <-data.response:
		return reply
	}
}

func (c *ChanneledSubscriber[T]) OnEventCh() <-chan *ChanneledSubscriberEvent[T] {
	return c.sender
}

func (c *ChanneledSubscriber[T]) close() { //nolint:unused
	close(c.sender)
}

func (c *ChanneledSubscriber[T]) cancel() { //nolint:unused
	go func() {
		for {
			e, ok := <-c.sender
			if !ok {
				return
			}

			e.Consume(func(_ T) error { return nil })
		}
	}()
}