// (c) Cartesi and individual authors (see AUTHORS)
// SPDX-License-Identifier: Apache-2.0 (see LICENSE)

package service

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/cartesi/rollups-node/internal/advancer"
	"github.com/cartesi/rollups-node/internal/advancer/machines"
	"github.com/cartesi/rollups-node/internal/inspect"
	"github.com/cartesi/rollups-node/internal/repository"
	"github.com/cartesi/rollups-node/pkg/rollupsmachine/cartesimachine"
)

type AdvancerService struct {
	database                *repository.Database
	serveMux                *http.ServeMux
	AdvancerPollingInterval time.Duration
	MachineServerVerbosity  cartesimachine.ServerVerbosity
}

func NewAdvancerService(
	database *repository.Database,
	serveMux *http.ServeMux,
	pollingInterval time.Duration,
	machineServerVerbosity cartesimachine.ServerVerbosity,
) *AdvancerService {
	return &AdvancerService{
		database:                database,
		serveMux:                serveMux,
		AdvancerPollingInterval: pollingInterval,
		MachineServerVerbosity:  machineServerVerbosity,
	}
}

func (s *AdvancerService) Start(
	ctx context.Context,
	ready chan<- struct{},
) error {

	repo := &repository.MachineRepository{Database: s.database}

	machines, err := machines.Load(ctx, repo, s.MachineServerVerbosity)
	if err != nil {
		return fmt.Errorf("failed to load the machines: %w", err)
	}
	defer machines.Close()

	advancer, err := advancer.New(machines, repo)
	if err != nil {
		return fmt.Errorf("failed to create the advancer: %w", err)
	}

	inspector, err := inspect.New(machines)
	if err != nil {
		return fmt.Errorf("failed to create the inspector: %w", err)
	}

	s.serveMux.Handle("/inspect/{dapp}", http.Handler(inspector))
	s.serveMux.Handle("/inspect/{dapp}/{payload}", http.Handler(inspector))

	poller, err := advancer.Poller(s.AdvancerPollingInterval)
	if err != nil {
		return fmt.Errorf("failed to create the advancer service: %w", err)
	}

	ready <- struct{}{}
	return poller.Start(ctx)
}

func (s *AdvancerService) String() string {
	return "advancer"
}
