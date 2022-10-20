package user

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSendHasher_Insert(t *testing.T) {
	h := newSendRecorder(sendHashExpiry)

	// Insert a message into the hasher.
	hash1, ok, err := h.tryInsertWait(context.Background(), []byte(literal1), time.Now().Add(time.Second))
	require.NoError(t, err)
	require.True(t, ok)
	require.NotEmpty(t, hash1)

	// Simulate successfully sending the message.
	h.addMessageID(hash1, "abc")

	// Inserting a message with the same hash should return false.
	_, ok, err = h.tryInsertWait(context.Background(), []byte(literal1), time.Now().Add(time.Second))
	require.NoError(t, err)
	require.False(t, ok)

	// Inserting a message with a different hash should return true.
	hash2, ok, err := h.tryInsertWait(context.Background(), []byte(literal2), time.Now().Add(time.Second))
	require.NoError(t, err)
	require.True(t, ok)
	require.NotEmpty(t, hash2)
}

func TestSendHasher_Insert_Expired(t *testing.T) {
	h := newSendRecorder(time.Second)

	// Insert a message into the hasher.
	hash1, ok, err := h.tryInsertWait(context.Background(), []byte(literal1), time.Now().Add(time.Second))
	require.NoError(t, err)
	require.True(t, ok)
	require.NotEmpty(t, hash1)

	// Simulate successfully sending the message.
	h.addMessageID(hash1, "abc")

	// Wait for the entry to expire.
	time.Sleep(time.Second)

	// Inserting a message with the same hash should return true because the previous entry has since expired.
	hash2, ok, err := h.tryInsertWait(context.Background(), []byte(literal1), time.Now().Add(time.Second))
	require.NoError(t, err)
	require.True(t, ok)

	// The hashes should be the same.
	require.Equal(t, hash1, hash2)
}

func TestSendHasher_Wait_SendSuccess(t *testing.T) {
	h := newSendRecorder(sendHashExpiry)

	// Insert a message into the hasher.
	hash, ok, err := h.tryInsertWait(context.Background(), []byte(literal1), time.Now().Add(time.Second))
	require.NoError(t, err)
	require.True(t, ok)
	require.NotEmpty(t, hash)

	// Simulate successfully sending the message after half a second.
	go func() {
		time.Sleep(time.Millisecond * 500)
		h.addMessageID(hash, "abc")
	}()

	// Inserting a message with the same hash should fail.
	_, ok, err = h.tryInsertWait(context.Background(), []byte(literal1), time.Now().Add(time.Second))
	require.NoError(t, err)
	require.False(t, ok)
}

func TestSendHasher_Wait_SendFail(t *testing.T) {
	h := newSendRecorder(sendHashExpiry)

	// Insert a message into the hasher.
	hash, ok, err := h.tryInsertWait(context.Background(), []byte(literal1), time.Now().Add(time.Second))
	require.NoError(t, err)
	require.True(t, ok)
	require.NotEmpty(t, hash)

	// Simulate failing to send the message after half a second.
	go func() {
		time.Sleep(time.Millisecond * 500)
		h.removeOnFail(hash)
	}()

	// Inserting a message with the same hash should succeed because the first message failed to send.
	hash2, ok, err := h.tryInsertWait(context.Background(), []byte(literal1), time.Now().Add(time.Second))
	require.NoError(t, err)
	require.True(t, ok)

	// The hashes should be the same.
	require.Equal(t, hash, hash2)
}

func TestSendHasher_Wait_Timeout(t *testing.T) {
	h := newSendRecorder(sendHashExpiry)

	// Insert a message into the hasher.
	hash, ok, err := h.tryInsertWait(context.Background(), []byte(literal1), time.Now().Add(time.Second))
	require.NoError(t, err)
	require.True(t, ok)
	require.NotEmpty(t, hash)

	// We should fail to insert because the message is not sent within the timeout period.
	_, _, err = h.tryInsertWait(context.Background(), []byte(literal1), time.Now().Add(time.Second))
	require.Error(t, err)
}

func TestSendHasher_HasEntry(t *testing.T) {
	h := newSendRecorder(sendHashExpiry)

	// Insert a message into the hasher.
	hash, ok, err := h.tryInsertWait(context.Background(), []byte(literal1), time.Now().Add(time.Second))
	require.NoError(t, err)
	require.True(t, ok)
	require.NotEmpty(t, hash)

	// Simulate successfully sending the message.
	h.addMessageID(hash, "abc")

	// The message was already sent; we should find it in the hasher.
	messageID, ok, err := h.hasEntryWait(context.Background(), []byte(literal1), time.Now().Add(time.Second))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "abc", messageID)
}

func TestSendHasher_HasEntry_SendSuccess(t *testing.T) {
	h := newSendRecorder(sendHashExpiry)

	// Insert a message into the hasher.
	hash, ok, err := h.tryInsertWait(context.Background(), []byte(literal1), time.Now().Add(time.Second))
	require.NoError(t, err)
	require.True(t, ok)
	require.NotEmpty(t, hash)

	// Simulate successfully sending the message after half a second.
	go func() {
		time.Sleep(time.Millisecond * 500)
		h.addMessageID(hash, "abc")
	}()

	// The message was already sent; we should find it in the hasher.
	messageID, ok, err := h.hasEntryWait(context.Background(), []byte(literal1), time.Now().Add(time.Second))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "abc", messageID)
}

func TestSendHasher_HasEntry_SendFail(t *testing.T) {
	h := newSendRecorder(sendHashExpiry)

	// Insert a message into the hasher.
	hash, ok, err := h.tryInsertWait(context.Background(), []byte(literal1), time.Now().Add(time.Second))
	require.NoError(t, err)
	require.True(t, ok)
	require.NotEmpty(t, hash)

	// Simulate failing to send the message after half a second.
	go func() {
		time.Sleep(time.Millisecond * 500)
		h.removeOnFail(hash)
	}()

	// The message failed to send; we should not find it in the hasher.
	_, ok, err = h.hasEntryWait(context.Background(), []byte(literal1), time.Now().Add(time.Second))
	require.NoError(t, err)
	require.False(t, ok)
}

func TestSendHasher_HasEntry_Timeout(t *testing.T) {
	h := newSendRecorder(sendHashExpiry)

	// Insert a message into the hasher.
	hash, ok, err := h.tryInsertWait(context.Background(), []byte(literal1), time.Now().Add(time.Second))
	require.NoError(t, err)
	require.True(t, ok)
	require.NotEmpty(t, hash)

	// The message is never sent; we should not find it in the hasher.
	_, ok, err = h.hasEntryWait(context.Background(), []byte(literal1), time.Now().Add(time.Second))
	require.NoError(t, err)
	require.False(t, ok)
}

func TestSendHasher_HasEntry_Expired(t *testing.T) {
	h := newSendRecorder(time.Second)

	// Insert a message into the hasher.
	hash, ok, err := h.tryInsertWait(context.Background(), []byte(literal1), time.Now().Add(time.Second))
	require.NoError(t, err)
	require.True(t, ok)
	require.NotEmpty(t, hash)

	// Simulate successfully sending the message.
	h.addMessageID(hash, "abc")

	// Wait for the entry to expire.
	time.Sleep(time.Second)

	// The entry has expired; we should not find it in the hasher.
	_, ok, err = h.hasEntryWait(context.Background(), []byte(literal1), time.Now().Add(time.Second))
	require.NoError(t, err)
	require.False(t, ok)
}

const literal1 = `From: Sender <sender@pm.me>
To: Receiver <receiver@pm.me>
Content-Type: multipart/mixed; boundary=longrandomstring

--longrandomstring

body
--longrandomstring
Content-Disposition: attachment; filename="attname.txt"

attachment
--longrandomstring--
`

const literal2 = `From: Sender <sender@pm.me>
To: Receiver <receiver@pm.me>
Content-Type: multipart/mixed; boundary=longrandomstring

--longrandomstring

body
--longrandomstring
Content-Disposition: attachment; filename="attname2.txt"

attachment
--longrandomstring--
`

func TestGetMessageHash(t *testing.T) {
	tests := []struct {
		name       string
		lit1, lit2 []byte
		wantEqual  bool
	}{
		{
			name:      "empty",
			lit1:      []byte{},
			lit2:      []byte{},
			wantEqual: true,
		},
		{
			name:      "same to",
			lit1:      []byte("To: someone@pm.me\r\n\r\nHello world!"),
			lit2:      []byte("To: someone@pm.me\r\n\r\nHello world!"),
			wantEqual: true,
		},
		{
			name:      "different to",
			lit1:      []byte("To: someone@pm.me\r\n\r\nHello world!"),
			lit2:      []byte("To: another@pm.me\r\n\r\nHello world!"),
			wantEqual: false,
		},
		{
			name:      "same from",
			lit1:      []byte("From: someone@pm.me\r\n\r\nHello world!"),
			lit2:      []byte("From: someone@pm.me\r\n\r\nHello world!"),
			wantEqual: true,
		},
		{
			name:      "different from",
			lit1:      []byte("From: someone@pm.me\r\n\r\nHello world!"),
			lit2:      []byte("From: another@pm.me\r\n\r\nHello world!"),
			wantEqual: false,
		},
		{
			name:      "same subject",
			lit1:      []byte("Subject: Hello world!\r\n\r\nHello world!"),
			lit2:      []byte("Subject: Hello world!\r\n\r\nHello world!"),
			wantEqual: true,
		},
		{
			name:      "different subject",
			lit1:      []byte("Subject: Hello world!\r\n\r\nHello world!"),
			lit2:      []byte("Subject: Goodbye world!\r\n\r\nHello world!"),
			wantEqual: false,
		},
		{
			name:      "same plaintext body",
			lit1:      []byte("To: someone@pm.me\r\nContent-Type: text/plain\r\n\r\nHello world!"),
			lit2:      []byte("To: someone@pm.me\r\nContent-Type: text/plain\r\n\r\nHello world!"),
			wantEqual: true,
		},
		{
			name:      "different plaintext body",
			lit1:      []byte("To: someone@pm.me\r\nContent-Type: text/plain\r\n\r\nHello world!"),
			lit2:      []byte("To: someone@pm.me\r\nContent-Type: text/plain\r\n\r\nGoodbye world!"),
			wantEqual: false,
		},
		{
			name:      "different attachment filenames",
			lit1:      []byte(literal1),
			lit2:      []byte(literal2),
			wantEqual: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash1, err := getMessageHash(tt.lit1)
			require.NoError(t, err)

			hash2, err := getMessageHash(tt.lit2)
			require.NoError(t, err)

			if tt.wantEqual {
				require.Equal(t, hash1, hash2)
			} else {
				require.NotEqual(t, hash1, hash2)
			}
		})
	}
}