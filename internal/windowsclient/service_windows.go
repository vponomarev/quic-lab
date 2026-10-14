package windowsclient

import (
	"context"
	"golang.org/x/sys/windows/svc"
	"quiclab/internal/vpnclient"
	"time"
)

const ServiceName = "QuicLabWindowsPreview"
const ServicePipe = `\\.\pipe\quic-lab-windows-preview-v1`

// FixtureBackend deliberately opens no socket and changes no system network state.
// The W2 service/UI explicitly advertise fixture mode until a verified W3 backend replaces it.
type FixtureBackend struct{}

func (FixtureBackend) RuntimeInfo() vpnclient.RuntimeInfo {
	return vpnclient.RuntimeInfo{Mode: "fixture"}
}

func (FixtureBackend) Start(context.Context, vpnclient.Config) error { return nil }
func (FixtureBackend) Stop(context.Context) error                    { return nil }

type Service struct {
	OwnerSID, DataDir string
	Engine            bool
}

func (s *Service) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	store, e := NewEncryptedStore(s.DataDir)
	if e != nil {
		return false, 1
	}
	l, e := Listen(ServicePipe, s.OwnerSID)
	if e != nil {
		return false, 2
	}
	defer l.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var handler *Handler
	var stop func(context.Context) error
	if s.Engine {
		manager, err := NewSettingsManager(NewSettingsFileStore(store), newSettingsBackend)
		if err != nil {
			return false, 5
		}
		handler = NewSettingsHandler(manager)
		stop = manager.Stop
	} else {
		controller := vpnclient.NewController(FixtureBackend{})
		handler = NewHandler(controller, store)
		stop = controller.Stop
	}
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		stop(cleanup)
	}()
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, l, s.OwnerSID, handler) }()
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case <-done:
			return false, 3
		case r := <-requests:
			switch r.Cmd {
			case svc.Interrogate:
				status <- r.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				cancel()
				stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
				e = stop(stopCtx)
				stopCancel()
				<-done
				if e != nil {
					return false, 4
				}
				return false, 0
			}
		}
	}
}
