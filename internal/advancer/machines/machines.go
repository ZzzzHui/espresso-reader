// (c) Cartesi and individual authors (see AUTHORS)
// SPDX-License-Identifier: Apache-2.0 (see LICENSE)

package machines

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"

	. "github.com/cartesi/rollups-node/internal/model"

	nm "github.com/cartesi/rollups-node/internal/nodemachine"
	"github.com/cartesi/rollups-node/pkg/emulator"
	rm "github.com/cartesi/rollups-node/pkg/rollupsmachine"
	cm "github.com/cartesi/rollups-node/pkg/rollupsmachine/cartesimachine"
)

type Repository interface {
	// GetMachineConfigurations retrieves a machine configuration for each application.
	GetMachineConfigurations(context.Context) ([]*MachineConfig, error)

	// GetProcessedInputs retrieves the processed inputs of an application with indexes greater or
	// equal to the given input index.
	GetProcessedInputs(_ context.Context, app Address, index uint64) ([]*Input, error)
}

// AdvanceMachine masks nodemachine.NodeMachine to only expose methods required by the Advancer.
type AdvanceMachine interface {
	Advance(_ context.Context, input []byte, index uint64) (*nm.AdvanceResult, error)
}

// InspectMachine masks nodemachine.NodeMachine to only expose methods required by the Inspector.
type InspectMachine interface {
	Inspect(_ context.Context, query []byte) (*nm.InspectResult, error)
}

// Machines is a thread-safe type that manages the pool of cartesi machines being used by the node.
// It contains a map of applications to machines.
type Machines struct {
	mutex      sync.RWMutex
	machines   map[Address]*nm.NodeMachine
	repository Repository
	verbosity  cm.ServerVerbosity
}

// Load initializes the cartesi machines.
// Load advances a machine to the last processed input stored in the database.
//
// Load does not fail when one of those machines fail to initialize.
// It stores the error to be returned later and continues to initialize the other machines.
func Load(ctx context.Context, repo Repository, verbosity cm.ServerVerbosity) (*Machines, error) {
	configs, err := repo.GetMachineConfigurations(ctx)
	if err != nil {
		return nil, err
	}

	machines := map[Address]*nm.NodeMachine{}
	var errs error

	for _, config := range configs {
		// Creates the machine.
		machine, err := createMachine(ctx, verbosity, config)
		if err != nil {
			err = fmt.Errorf("failed to create machine from snapshot (%v): %w", config, err)
			errs = errors.Join(errs, err)
			continue
		}

		// Advances the machine until it catches up with the state of the database (if necessary).
		err = catchUp(ctx, repo, config.AppAddress, machine, config.ProcessedInputs)
		if err != nil {
			err = fmt.Errorf("failed to advance cartesi machine (%v): %w", config, err)
			errs = errors.Join(errs, err, machine.Close())
			continue
		}

		machines[config.AppAddress] = machine
	}

	return &Machines{machines: machines, repository: repo, verbosity: verbosity}, errs
}

func (m *Machines) UpdateMachines(ctx context.Context) error {
	configs, err := m.repository.GetMachineConfigurations(ctx)
	if err != nil {
		return err
	}

	for _, config := range configs {
		if m.Exists(config.AppAddress) {
			continue
		}

		machine, err := createMachine(ctx, m.verbosity, config)
		if err != nil {
			slog.Error("advancer: Failed to create machine", "app", config.AppAddress, "error", err)
			continue
		}

		err = catchUp(ctx, m.repository, config.AppAddress, machine, config.ProcessedInputs)
		if err != nil {
			slog.Error("Failed to sync the machine", "app", config.AppAddress, "error", err)
			machine.Close()
			continue
		}

		m.Add(config.AppAddress, machine)
	}

	m.RemoveAbsent(configs)

	return nil
}

// GetAdvanceMachine gets the machine associated with the application from the map.
func (m *Machines) GetAdvanceMachine(app Address) (AdvanceMachine, bool) {
	return m.getMachine(app)
}

// GetInspectMachine gets the machine associated with the application from the map.
func (m *Machines) GetInspectMachine(app Address) (InspectMachine, bool) {
	return m.getMachine(app)
}

// Add maps a new application to a machine.
// It does nothing if the application is already mapped to some machine.
// It returns true if it was able to add the machine and false otherwise.
func (m *Machines) Add(app Address, machine *nm.NodeMachine) bool {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if _, ok := m.machines[app]; ok {
		return false
	} else {
		m.machines[app] = machine
		return true
	}
}

func (m *Machines) Exists(app Address) bool {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	_, exists := m.machines[app]
	return exists
}

func (m *Machines) RemoveAbsent(configs []*MachineConfig) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	configMap := make(map[Address]bool)
	for _, config := range configs {
		configMap[config.AppAddress] = true
	}
	for address, machine := range m.machines {
		if !configMap[address] {
			slog.Info("advancer: Application was disabled, shutting down machine", "application", address)
			machine.Close()
			delete(m.machines, address)
		}
	}
}

// Delete deletes an application from the map.
// It returns the associated machine, if any.
func (m *Machines) Delete(app Address) *nm.NodeMachine {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if machine, ok := m.machines[app]; ok {
		return nil
	} else {
		delete(m.machines, app)
		return machine
	}
}

// Apps returns the addresses of the applications for which there are machines.
func (m *Machines) Apps() []Address {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	keys := make([]Address, len(m.machines))
	i := 0
	for k := range m.machines {
		keys[i] = k
		i++
	}
	return keys
}

// Close closes all the machines and erases them from the map.
func (m *Machines) Close() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	err := closeMachines(m.machines)
	if err != nil {
		slog.Error(fmt.Sprintf("failed to close some machines: %v", err))
	}
	return err
}

// ------------------------------------------------------------------------------------------------

func (m *Machines) getMachine(app Address) (*nm.NodeMachine, bool) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	machine, exists := m.machines[app]
	return machine, exists
}

func closeMachines(machines map[Address]*nm.NodeMachine) (err error) {
	for _, machine := range machines {
		err = errors.Join(err, machine.Close())
	}
	for app := range machines {
		delete(machines, app)
	}
	return
}

func createMachine(ctx context.Context,
	verbosity cm.ServerVerbosity,
	config *MachineConfig,
) (*nm.NodeMachine, error) {
	slog.Info("advancer: creating machine", "application", config.AppAddress,
		"template-path", config.SnapshotPath)
	slog.Debug("advancer: instantiating remote machine server", "application", config.AppAddress)
	// Starts the server.
	address, err := cm.StartServer(verbosity, 0, os.Stdout, os.Stderr)
	if err != nil {
		return nil, err
	}

	slog.Info("advancer: loading machine on server", "application", config.AppAddress,
		"remote-machine", address, "template-path", config.SnapshotPath)
	// Creates a CartesiMachine from the snapshot.
	runtimeConfig := &emulator.MachineRuntimeConfig{}
	cartesiMachine, err := cm.Load(ctx, config.SnapshotPath, address, runtimeConfig)
	if err != nil {
		return nil, errors.Join(err, cm.StopServer(address))
	}

	slog.Debug("advancer: machine loaded on server", "application", config.AppAddress,
		"remote-machine", address, "template-path", config.SnapshotPath)

	// Creates a RollupsMachine from the CartesiMachine.
	rollupsMachine, err := rm.New(ctx,
		cartesiMachine,
		config.AdvanceIncCycles,
		config.AdvanceMaxCycles)
	if err != nil {
		return nil, errors.Join(err, cartesiMachine.Close(ctx))
	}

	// Creates a NodeMachine from the RollupsMachine.
	nodeMachine, err := nm.NewNodeMachine(rollupsMachine,
		config.ProcessedInputs,
		config.AdvanceMaxDeadline,
		config.InspectMaxDeadline,
		config.MaxConcurrentInspects)
	if err != nil {
		return nil, errors.Join(err, rollupsMachine.Close(ctx))
	}

	return nodeMachine, err
}

func catchUp(ctx context.Context,
	repo Repository,
	app Address,
	machine *nm.NodeMachine,
	processedInputs uint64,
) error {

	slog.Info("advancer: catching up unprocessed inputs", "app", app)

	inputs, err := repo.GetProcessedInputs(ctx, app, processedInputs)
	if err != nil {
		return err
	}

	for _, input := range inputs {
		// FIXME epoch id to epoch index
		slog.Info("advancer: advancing", "app", app, "epochId", input.EpochId,
			"input_index", input.Index)
		_, err := machine.Advance(ctx, input.RawData, input.Index)
		if err != nil {
			return err
		}
	}

	return nil
}
