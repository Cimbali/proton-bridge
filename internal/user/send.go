package user

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ProtonMail/gluon/rfc822"
	"github.com/sirupsen/logrus"
)

const sendHashExpiry = 5 * time.Minute

type sendRecorder struct {
	hasher func([]byte) (string, error)
	expiry time.Duration

	entries     map[string]*sendEntry
	entriesLock sync.Mutex
}

func newSendRecorder(expiry time.Duration) *sendRecorder {
	return &sendRecorder{
		hasher:  getMessageHash,
		expiry:  expiry,
		entries: make(map[string]*sendEntry),
	}
}

type sendEntry struct {
	msgID  string
	exp    time.Time
	waitCh chan struct{}
}

// tryInsertWait tries to insert the given message into the send recorder.
// If an entry already exists but it was not sent yet, it waits.
// It returns whether an entry could be inserted and an error if it times out while waiting.
func (h *sendRecorder) tryInsertWait(ctx context.Context, b []byte, deadline time.Time) (string, bool, error) {
	hash, err := h.hasher(b)
	if err != nil {
		return "", false, fmt.Errorf("failed to hash message: %w", err)
	}

	// If we successfully inserted the hash, we can return true.
	if h.tryInsert(hash) {
		return hash, true, nil
	}

	// A message with this hash is already being sent; wait for it.
	_, wasSent, err := h.wait(ctx, hash, deadline)
	if err != nil {
		return "", false, fmt.Errorf("failed to wait for message to be sent: %w", err)
	}

	// If the message failed to send, try to insert it again.
	if !wasSent {
		return h.tryInsertWait(ctx, b, deadline)
	}

	return hash, false, nil
}

// hasEntryWait returns whether the given message already exists in the send recorder.
// If it does, it waits for its ID to be known, then returns it and true.
// If no entry exists, or it times out while waiting for its ID to be known, it returns false.
func (h *sendRecorder) hasEntryWait(ctx context.Context, b []byte, deadline time.Time) (string, bool, error) {
	hash, err := h.hasher(b)
	if err != nil {
		return "", false, fmt.Errorf("failed to hash message: %w", err)
	}

	if !h.hasEntry(hash) {
		return "", false, nil
	}

	messageID, wasSent, err := h.wait(ctx, hash, deadline)
	if errors.Is(err, context.DeadlineExceeded) {
		return "", false, nil
	} else if err != nil {
		return "", false, fmt.Errorf("failed to wait for message to be sent: %w", err)
	}

	if wasSent {
		return messageID, true, nil
	}

	return h.hasEntryWait(ctx, b, deadline)
}

func (h *sendRecorder) tryInsert(hash string) bool {
	h.entriesLock.Lock()
	defer h.entriesLock.Unlock()

	for hash, entry := range h.entries {
		if entry.exp.Before(time.Now()) {
			delete(h.entries, hash)
		}
	}

	if _, ok := h.entries[hash]; ok {
		return false
	}

	h.entries[hash] = &sendEntry{
		exp:    time.Now().Add(h.expiry),
		waitCh: make(chan struct{}),
	}

	return true
}

func (h *sendRecorder) hasEntry(hash string) bool {
	h.entriesLock.Lock()
	defer h.entriesLock.Unlock()

	for hash, entry := range h.entries {
		if entry.exp.Before(time.Now()) {
			delete(h.entries, hash)
		}
	}

	if _, ok := h.entries[hash]; ok {
		return true
	}

	return false
}

func (h *sendRecorder) addMessageID(hash, msgID string) {
	h.entriesLock.Lock()
	defer h.entriesLock.Unlock()

	entry, ok := h.entries[hash]
	if ok {
		entry.msgID = msgID
	} else {
		logrus.Warn("Cannot add message ID to send hash entry, it may have expired")
	}

	close(entry.waitCh)
}

func (h *sendRecorder) removeOnFail(hash string) {
	h.entriesLock.Lock()
	defer h.entriesLock.Unlock()

	entry, ok := h.entries[hash]
	if !ok || entry.msgID != "" {
		return
	}

	close(entry.waitCh)

	delete(h.entries, hash)
}

func (h *sendRecorder) wait(ctx context.Context, hash string, deadline time.Time) (string, bool, error) {
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	waitCh, err := h.getWaitCh(hash)
	if err != nil {
		return "", false, nil
	}

	select {
	case <-ctx.Done():
		return "", false, ctx.Err()

	case <-waitCh:
		// ...
	}

	h.entriesLock.Lock()
	defer h.entriesLock.Unlock()

	if entry, ok := h.entries[hash]; ok {
		return entry.msgID, true, nil
	}

	return "", false, nil
}

func (h *sendRecorder) getWaitCh(hash string) (<-chan struct{}, error) {
	h.entriesLock.Lock()
	defer h.entriesLock.Unlock()

	if entry, ok := h.entries[hash]; ok {
		return entry.waitCh, nil
	}

	return nil, fmt.Errorf("no entry with hash %s", hash)
}

// getMessageHash returns the hash of the given message.
// This takes into account:
// - the Subject header
// - the From/To/Cc/Bcc headers
// - the Content-Type header of each (leaf) part
// - the Content-Disposition header of each (leaf) part
// - the (decoded) body of each part
func getMessageHash(b []byte) (string, error) {
	section := rfc822.Parse(b)

	header, err := section.ParseHeader()
	if err != nil {
		return "", err
	}

	h := sha256.New()

	if _, err := h.Write([]byte(header.Get("Subject"))); err != nil {
		return "", err
	}

	if _, err := h.Write([]byte(header.Get("From"))); err != nil {
		return "", err
	}

	if _, err := h.Write([]byte(header.Get("To"))); err != nil {
		return "", err
	}

	if _, err := h.Write([]byte(header.Get("Cc"))); err != nil {
		return "", err
	}

	if _, err := h.Write([]byte(header.Get("Bcc"))); err != nil {
		return "", err
	}

	if err := section.Walk(func(section *rfc822.Section) error {
		children, err := section.Children()
		if err != nil {
			return err
		} else if len(children) > 0 {
			return nil
		}

		header, err := section.ParseHeader()
		if err != nil {
			return err
		}

		if _, err := h.Write([]byte(header.Get("Content-Type"))); err != nil {
			return err
		}

		if _, err := h.Write([]byte(header.Get("Content-Disposition"))); err != nil {
			return err
		}

		body, err := section.DecodedBody()
		if err != nil {
			return err
		}

		if _, err := h.Write(bytes.TrimSpace(body)); err != nil {
			return err
		}

		return nil
	}); err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(h.Sum(nil)), nil
}