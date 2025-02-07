package lifecycle

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/bnb-chain/greenfield-storage-provider/pkg/log"
)

const StopTimeout = 30

// Service provides abstract methods to control the lifecycle of a service
//
//go:generate mockgen -source=./lifecycle.go -destination=./mock/lifecycle_mock.go -package=mock
type Service interface {
	// Name describe service name
	Name() string
	// Start a service, this method should be used in non-block form
	Start(ctx context.Context) error
	// Stop a service, this method should be used in non-block form
	Stop(ctx context.Context) error
}

// ServiceLifecycle manages services' lifecycle
type ServiceLifecycle struct {
	innerCtx    context.Context
	innerCancel context.CancelFunc
	services    []Service
	timeout     time.Duration
}

var (
	slc  *ServiceLifecycle
	once sync.Once
)

// NewServiceLifecycle returns a singleton instance of ServiceLifecycle
func NewServiceLifecycle() *ServiceLifecycle {
	once.Do(func() {
		innerCtx, innerCancel := context.WithCancel(context.Background())
		slc = &ServiceLifecycle{
			innerCtx:    innerCtx,
			innerCancel: innerCancel,
			timeout:     time.Duration(StopTimeout) * time.Second,
		}
	})
	return slc
}

// RegisterServices register services of an application
func (s *ServiceLifecycle) RegisterServices(services ...Service) {
	s.services = append(s.services, services...)
}

// StartServices starts running services
func (s *ServiceLifecycle) StartServices(ctx context.Context) *ServiceLifecycle {
	s.start(ctx)
	return s
}

func (s *ServiceLifecycle) start(ctx context.Context) {
	for i, service := range s.services {
		if err := service.Start(ctx); err != nil {
			log.Errorf("service %s starts error: %v", service.Name(), err)
			s.services = s.services[:i]
			s.innerCancel()
			break
		} else {
			log.Infof("service %s starts successfully", service.Name())
		}
	}
}

// Signals registers monitor signals
func (s *ServiceLifecycle) Signals(sigs ...os.Signal) *ServiceLifecycle {
	go s.signals(sigs...)
	return s
}

func (s *ServiceLifecycle) signals(sigs ...os.Signal) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, sigs...)
	for {
		select {
		case <-s.innerCtx.Done():
			return
		case sig := <-sigCh:
			for _, j := range sigs {
				if j == sig {
					s.innerCancel()
					return
				}
			}
		}
	}
}

// Wait blocks until context is done
func (s *ServiceLifecycle) Wait(ctx context.Context) {
	<-s.innerCtx.Done()
	s.StopServices(ctx)
}

// StopServices stop services when context is done or timeout
func (s *ServiceLifecycle) StopServices(ctx context.Context) {
	gCtx, cancel := context.WithTimeout(context.Background(), s.timeout)
	s.stop(ctx, cancel)

	<-gCtx.Done()
	if errors.Is(gCtx.Err(), context.Canceled) {
		log.Infow("services stop working", "stop service timeout", s.timeout)
	} else if errors.Is(gCtx.Err(), context.DeadlineExceeded) {
		log.Error("timeout while stopping service, killing instance manually")
	}
}

func (s *ServiceLifecycle) stop(ctx context.Context, cancel context.CancelFunc) {
	for _, service := range s.services {
		if err := service.Stop(ctx); err != nil {
			log.Errorf("service %s stops failure: %v", service.Name(), err)
		} else {
			log.Infof("service %s stops successfully!", service.Name())
		}
	}
	cancel()
}

// Done check context is done
func (s *ServiceLifecycle) Done() <-chan struct{} {
	return s.innerCtx.Done()
}
