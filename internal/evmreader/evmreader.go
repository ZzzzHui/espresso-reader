// (c) Cartesi and individual authors (see AUTHORS)
// SPDX-License-Identifier: Apache-2.0 (see LICENSE)

package evmreader

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"strings"

	. "github.com/ZzzzHui/espresso-reader/internal/model"
	appcontract "github.com/ZzzzHui/espresso-reader/pkg/contracts/iapplication"
	"github.com/ZzzzHui/espresso-reader/pkg/contracts/iconsensus"
	"github.com/ZzzzHui/espresso-reader/pkg/contracts/iinputbox"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
)

// Interface for Input reading
type InputSource interface {
	// Wrapper for FilterInputAdded(), which is automatically generated
	// by go-ethereum and cannot be used for testing
	RetrieveInputs(opts *bind.FilterOpts, appAddresses []Address, index []*big.Int,
	) ([]iinputbox.IInputBoxInputAdded, error)
}

// Interface for the node repository
type EvmReaderRepository interface {
	StoreEpochAndInputsTransaction(
		ctx context.Context, epochInputMap map[*Epoch][]Input, blockNumber uint64,
		appAddress Address,
	) (epochIndexIdMap map[uint64]uint64, epochIndexInputIdsMap map[uint64][]uint64, err error)

	GetAllRunningApplications(ctx context.Context) ([]Application, error)
	GetNodeConfig(ctx context.Context) (*NodePersistentConfig, error)
	GetEpoch(ctx context.Context, indexKey uint64, appAddressKey Address) (*Epoch, error)
	GetPreviousEpochsWithOpenClaims(
		ctx context.Context,
		app Address,
		lastBlock uint64,
	) ([]*Epoch, error)
	UpdateEpochs(ctx context.Context,
		app Address,
		claims []*Epoch,
		mostRecentBlockNumber uint64,
	) error
	GetOutput(
		ctx context.Context, appAddressKey Address, indexKey uint64,
	) (*Output, error)
	UpdateOutputExecutionTransaction(
		ctx context.Context, app Address, executedOutputs []*Output, blockNumber uint64,
	) error
	GetInputIndex(
		ctx context.Context,
		applicationAddress Address,
	) (uint64, error)
	UpdateInputIndex(
		ctx context.Context,
		applicationAddress Address,
	) error
}

// EthClient mimics part of ethclient.Client functions to narrow down the
// interface needed by the EvmReader. It must be bound to an HTTP endpoint
type EthClient interface {
	HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error)
}

// EthWsClient mimics part of ethclient.Client functions to narrow down the
// interface needed by the EvmReader. It must be bound to a WS endpoint
type EthWsClient interface {
	SubscribeNewHead(ctx context.Context, ch chan<- *types.Header) (ethereum.Subscription, error)
}

type ConsensusContract interface {
	GetEpochLength(opts *bind.CallOpts) (*big.Int, error)
	RetrieveClaimAcceptanceEvents(
		opts *bind.FilterOpts,
		appAddresses []Address,
	) ([]*iconsensus.IConsensusClaimAcceptance, error)
}

type ApplicationContract interface {
	GetConsensus(opts *bind.CallOpts) (Address, error)
	RetrieveOutputExecutionEvents(
		opts *bind.FilterOpts,
	) ([]*appcontract.IApplicationOutputExecuted, error)
}

type ContractFactory interface {
	NewApplication(address Address) (ApplicationContract, error)
	NewIConsensus(address Address) (ConsensusContract, error)
}

type SubscriptionError struct {
	Cause error
}

func (e *SubscriptionError) Error() string {
	return fmt.Sprintf("Subscription error : %v", e.Cause)
}

// Internal struct to hold application and it's contracts together
type application struct {
	Application
	ApplicationContract
	ConsensusContract
}

// EvmReader reads Input Added, Claim Submitted and
// Output Executed events from the blockchain
type EvmReader struct {
	client                  EthClient
	wsClient                EthWsClient
	inputSource             InputSource
	repository              EvmReaderRepository
	contractFactory         ContractFactory
	inputBoxDeploymentBlock uint64
	defaultBlock            DefaultBlock
	epochLengthCache        map[Address]uint64
	hasEnabledApps          bool
	shouldModifyIndex       bool // modify index in raw data if the main sequencer is espresso
	IOAbi                   abi.ABI
}

func (r *EvmReader) String() string {
	return "evmreader"
}

// Creates a new EvmReader
func NewEvmReader(
	client EthClient,
	wsClient EthWsClient,
	inputSource InputSource,
	repository EvmReaderRepository,
	inputBoxDeploymentBlock uint64,
	defaultBlock DefaultBlock,
	contractFactory ContractFactory,
	shouldModifyIndex bool,
) EvmReader {
	abiData, err := os.ReadFile("internal/evmreader/abi.json")
	if err != nil {
		panic(err)
	}
	ioABI, err := abi.JSON(strings.NewReader(string(abiData)))
	if err != nil {
		panic(err)
	}
	evmReader := EvmReader{
		client:                  client,
		wsClient:                wsClient,
		inputSource:             inputSource,
		repository:              repository,
		inputBoxDeploymentBlock: inputBoxDeploymentBlock,
		defaultBlock:            defaultBlock,
		contractFactory:         contractFactory,
		hasEnabledApps:          true,
		shouldModifyIndex:       shouldModifyIndex,
		IOAbi:                   ioABI,
	}
	// Initialize epochLength cache
	evmReader.epochLengthCache = make(map[Address]uint64)
	return evmReader
}

func (r *EvmReader) Run(ctx context.Context, ready chan<- struct{}) error {
	for {
		err := r.watchForNewBlocks(ctx, ready)
		// If the error is a SubscriptionError, re run watchForNewBlocks
		// that it will restart the websocket subscription
		if _, ok := err.(*SubscriptionError); !ok {
			return err
		}
		slog.Error(err.Error())
		slog.Info("evmreader: Restarting subscription")
	}
}

// watchForNewBlocks watches for new blocks and reads new inputs based on the
// default block configuration, which have not been processed yet.
func (r *EvmReader) watchForNewBlocks(ctx context.Context, ready chan<- struct{}) error {
	headers := make(chan *types.Header)
	sub, err := r.wsClient.SubscribeNewHead(ctx, headers)
	if err != nil {
		return fmt.Errorf("could not start subscription: %v", err)
	}
	slog.Info("evmreader: Subscribed to new block events")
	ready <- struct{}{}
	defer sub.Unsubscribe()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-sub.Err():
			return &SubscriptionError{Cause: err}
		case header := <-headers:

			// Every time a new block arrives
			slog.Debug("evmreader: New block header received", "blockNumber", header.Number, "blockHash", header.Hash())

			slog.Debug("evmreader: Retrieving enabled applications")
			// Get All Applications
			runningApps, err := r.repository.GetAllRunningApplications(ctx)
			if err != nil {
				slog.Error("evmreader: Error retrieving running applications",
					"error",
					err,
				)
				continue
			}

			if len(runningApps) == 0 {
				if r.hasEnabledApps {
					slog.Info("evmreader: No registered applications enabled")
				}
				r.hasEnabledApps = false
				continue
			}
			if !r.hasEnabledApps {
				slog.Info("evmreader: Found enabled applications")
			}
			r.hasEnabledApps = true

			// Build Contracts
			var apps []application
			for _, app := range runningApps {
				applicationContract, consensusContract, err := r.GetAppContracts(app)
				if err != nil {
					slog.Error("evmreader: Error retrieving application contracts", "app", app, "error", err)
					continue
				}
				apps = append(apps, application{Application: app,
					ApplicationContract: applicationContract,
					ConsensusContract:   consensusContract})
			}

			if len(apps) == 0 {
				slog.Info("evmreader: No correctly configured applications running")
				continue
			}

			blockNumber := header.Number.Uint64()
			if r.defaultBlock != DefaultBlockStatusLatest {
				mostRecentHeader, err := r.fetchMostRecentHeader(
					ctx,
					r.defaultBlock,
				)
				if err != nil {
					slog.Error("evmreader: Error fetching most recent block",
						"default block", r.defaultBlock,
						"error", err)
					continue
				}
				blockNumber = mostRecentHeader.Number.Uint64()

				slog.Debug(fmt.Sprintf("evmreader: Using block %d and not %d because of commitment policy: %s",
					mostRecentHeader.Number.Uint64(), header.Number.Uint64(), r.defaultBlock))
			}

			r.checkForNewInputs(ctx, apps, blockNumber)

			r.CheckForClaimStatus(ctx, apps, blockNumber)

			r.CheckForOutputExecution(ctx, apps, blockNumber)

		}
	}
}

// fetchMostRecentHeader fetches the most recent header up till the
// given default block
func (r *EvmReader) fetchMostRecentHeader(
	ctx context.Context,
	defaultBlock DefaultBlock,
) (*types.Header, error) {

	var defaultBlockNumber int64
	switch defaultBlock {
	case DefaultBlockStatusPending:
		defaultBlockNumber = rpc.PendingBlockNumber.Int64()
	case DefaultBlockStatusLatest:
		defaultBlockNumber = rpc.LatestBlockNumber.Int64()
	case DefaultBlockStatusFinalized:
		defaultBlockNumber = rpc.FinalizedBlockNumber.Int64()
	case DefaultBlockStatusSafe:
		defaultBlockNumber = rpc.SafeBlockNumber.Int64()
	default:
		return nil, fmt.Errorf("default block '%v' not supported", defaultBlock)
	}

	header, err :=
		r.client.HeaderByNumber(
			ctx,
			new(big.Int).SetInt64(defaultBlockNumber))
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve header. %v", err)
	}

	if header == nil {
		return nil, fmt.Errorf("returned header is nil")
	}
	return header, nil
}

// GetAppContracts retrieves the ApplicationContract and ConsensusContract for a given Application.
// Also validates if IConsensus configuration matches the blockchain registered one
func (r *EvmReader) GetAppContracts(app Application,
) (ApplicationContract, ConsensusContract, error) {
	applicationContract, err := r.contractFactory.NewApplication(app.ContractAddress)
	if err != nil {
		return nil, nil, errors.Join(
			fmt.Errorf("error building application contract"),
			err,
		)

	}
	consensusAddress, err := applicationContract.GetConsensus(nil)
	if err != nil {
		return nil, nil, errors.Join(
			fmt.Errorf("error retrieving application consensus"),
			err,
		)
	}

	if app.IConsensusAddress != consensusAddress {
		return nil, nil,
			fmt.Errorf("IConsensus addresses do not match. Deployed: %s. Configured: %s",
				consensusAddress,
				app.IConsensusAddress)
	}

	consensus, err := r.contractFactory.NewIConsensus(consensusAddress)
	if err != nil {
		return nil, nil, errors.Join(
			fmt.Errorf("error building consensus contract"),
			err,
		)

	}
	return applicationContract, consensus, nil
}

func (r *EvmReader) GetEpochLengthCache(a Address) uint64 {
	return r.epochLengthCache[a]
}

func (r *EvmReader) GetEthClient() *EthClient {
	return &r.client
}
