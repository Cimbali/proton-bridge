// Copyright (c) 2022 Proton AG
//
// This file is part of Proton Mail Bridge.Bridge.
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
// along with Proton Mail Bridge. If not, see <https://www.gnu.org/licenses/>.

package grpc

import (
	"context"

	"github.com/pkg/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// StartEventStream implement the gRPC server->Client event stream.
func (s *Service) StartEventStream(request *EventStreamRequest, server Bridge_StartEventStreamServer) error {
	s.log.Info("Starting Event stream")

	if s.eventStreamCh != nil {
		return status.Errorf(codes.AlreadyExists, "the service is already streaming") // TO-DO GODT-1667 decide if we want to kill the existing stream.
	}

	s.userAgent.SetPlatform(request.ClientPlatform)

	s.eventStreamCh = make(chan *StreamEvent)
	s.eventStreamDoneCh = make(chan struct{})

	// TO-DO GODT-1667 We should have a safer we to close this channel? What if an event occur while we are closing?
	defer func() {
		close(s.eventStreamCh)
		s.eventStreamCh = nil
		close(s.eventStreamDoneCh)
		s.eventStreamDoneCh = nil
	}()

	for {
		select {
		case <-s.eventStreamDoneCh:
			s.log.Info("Stop Event stream")
			return nil

		case event := <-s.eventStreamCh:
			s.log.WithField("event", event).Info("Sending event")
			if err := server.Send(event); err != nil {
				s.log.Info("Stop Event stream")
				return err
			}
		}
	}
}

// StopEventStream stops the event stream.
func (s *Service) StopEventStream(_ context.Context, _ *emptypb.Empty) (*emptypb.Empty, error) {
	if s.eventStreamCh == nil {
		return nil, status.Errorf(codes.NotFound, "The service is not streaming")
	}

	s.eventStreamDoneCh <- struct{}{}

	return &emptypb.Empty{}, nil
}

// SendEvent sends an event to the via the gRPC event stream.
func (s *Service) SendEvent(event *StreamEvent) error {
	if s.eventStreamCh == nil {
		return errors.New("gRPC service is not streaming")
	}

	s.eventStreamCh <- event

	return nil
}

// StartEventTest sends all the known event via gRPC.
func (s *Service) StartEventTest() error { //nolint:funlen
	const dummyAddress = "dummy@proton.me"
	events := []*StreamEvent{
		// app
		NewInternetStatusEvent(true),
		NewToggleAutostartFinishedEvent(),
		NewResetFinishedEvent(),
		NewReportBugFinishedEvent(),
		NewReportBugSuccessEvent(),
		NewReportBugErrorEvent(),
		NewShowMainWindowEvent(),

		// login
		NewLoginError(LoginErrorType_FREE_USER, "error"),
		NewLoginTfaRequestedEvent(dummyAddress),
		NewLoginTwoPasswordsRequestedEvent(),
		NewLoginFinishedEvent("userID"),
		NewLoginAlreadyLoggedInEvent("userID"),

		// update
		NewUpdateErrorEvent(UpdateErrorType_UPDATE_SILENT_ERROR),
		NewUpdateManualReadyEvent("2.0"),
		NewUpdateManualRestartNeededEvent(),
		NewUpdateForceEvent("2.0"),
		NewUpdateSilentRestartNeededEvent(),
		NewUpdateIsLatestVersionEvent(),
		NewUpdateCheckFinishedEvent(),

		// cache
		NewCacheErrorEvent(CacheErrorType_CACHE_UNAVAILABLE_ERROR),
		NewCacheLocationChangeSuccessEvent(),
		NewCacheChangeLocalCacheFinishedEvent(),
		NewIsCacheOnDiskEnabledChanged(true),
		NewDiskCachePathChanged("/dummy/path"),

		// mail settings
		NewMailSettingsErrorEvent(MailSettingsErrorType_IMAP_PORT_ISSUE),
		NewMailSettingsUseSslForSmtpFinishedEvent(),
		NewMailSettingsChangePortFinishedEvent(),

		// keychain
		NewKeychainChangeKeychainFinishedEvent(),
		NewKeychainHasNoKeychainEvent(),
		NewKeychainRebuildKeychainEvent(),

		// mail
		NewMailNoActiveKeyForRecipientEvent(dummyAddress),
		NewMailAddressChangeEvent(dummyAddress),
		NewMailAddressChangeLogoutEvent(dummyAddress),
		NewMailApiCertIssue(),

		// user
		NewUserToggleSplitModeFinishedEvent("userID"),
		NewUserDisconnectedEvent("username"),
		NewUserChangedEvent("userID"),
	}

	for _, event := range events {
		if err := s.SendEvent(event); err != nil {
			return err
		}
	}

	return nil
}