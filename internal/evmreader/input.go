// (c) Cartesi and individual authors (see AUTHORS)
// SPDX-License-Identifier: Apache-2.0 (see LICENSE)

package evmreader

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/big"

	. "github.com/ZzzzHui/espresso-reader/internal/model"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
)

// checkForNewInputs checks if is there new Inputs for all running Applications
func (r *EvmReader) checkForNewInputs(
	ctx context.Context,
	apps []application,
	mostRecentBlockNumber uint64,
) {

	slog.Debug("evmreader: Checking for new inputs")

	groupedApps := indexApps(byLastProcessedBlock, apps)

	for lastProcessedBlock, apps := range groupedApps {

		appAddresses := appsToAddresses(apps)

		// Safeguard: Only check blocks starting from the block where the InputBox
		// contract was deployed as Inputs can be added to that same block
		if lastProcessedBlock < r.inputBoxDeploymentBlock {
			lastProcessedBlock = r.inputBoxDeploymentBlock - 1
		}

		if mostRecentBlockNumber > lastProcessedBlock {

			slog.Debug("evmreader: Checking inputs for applications",
				"apps", appAddresses,
				"last processed block", lastProcessedBlock,
				"most recent block", mostRecentBlockNumber,
			)

			err := r.ReadAndStoreInputs(ctx,
				lastProcessedBlock+1,
				mostRecentBlockNumber,
				apps,
			)
			if err != nil {
				slog.Error("Error reading inputs",
					"apps", appAddresses,
					"last processed block", lastProcessedBlock,
					"most recent block", mostRecentBlockNumber,
					"error", err,
				)
				continue
			}
		} else if mostRecentBlockNumber < lastProcessedBlock {
			slog.Warn(
				"evmreader: Not reading inputs: most recent block is lower than the last processed one",
				"apps", appAddresses,
				"last processed block", lastProcessedBlock,
				"most recent block", mostRecentBlockNumber,
			)
		} else {
			slog.Info("evmreader: Not reading inputs: already checked the most recent blocks",
				"apps", appAddresses,
				"last processed block", lastProcessedBlock,
				"most recent block", mostRecentBlockNumber,
			)
		}
	}
}

type TypeExportApplication = application

// ReadAndStoreInputs reads, inputs from the InputSource given specific filter options, indexes
// them into epochs and store the indexed inputs and epochs
func (r *EvmReader) ReadAndStoreInputs(
	ctx context.Context,
	startBlock uint64,
	endBlock uint64,
	apps []TypeExportApplication,
) error {
	appsToProcess := []common.Address{}

	for _, app := range apps {

		// Get App EpochLength
		err := r.AddAppEpochLengthIntoCache(app)
		if err != nil {
			slog.Error("evmreader: Error adding epoch length into cache",
				"app", app.ContractAddress,
				"error", err)
			continue
		}

		appsToProcess = append(appsToProcess, app.ContractAddress)

	}

	if len(appsToProcess) == 0 {
		slog.Warn("evmreader: No valid running applications")
		return nil
	}

	// Retrieve Inputs from blockchain
	appInputsMap, err := r.readInputsFromBlockchain(ctx, appsToProcess, startBlock, endBlock)
	if err != nil {
		return fmt.Errorf("failed to read inputs from block %v to block %v. %w",
			startBlock,
			endBlock,
			err)
	}

	// Index Inputs into epochs and handle epoch finalization
	for address, inputs := range appInputsMap {

		epochLength := r.epochLengthCache[address]

		// Retrieves last open epoch from DB
		currentEpoch, err := r.repository.GetEpoch(ctx,
			CalculateEpochIndex(epochLength, startBlock), address)
		if err != nil {
			slog.Error("evmreader: Error retrieving existing current epoch",
				"app", address,
				"error", err,
			)
			continue
		}

		// Check current epoch status
		if currentEpoch != nil && currentEpoch.Status != EpochStatusOpen {
			slog.Error("evmreader: Current epoch is not open",
				"app", address,
				"epoch_index", currentEpoch.Index,
				"status", currentEpoch.Status,
			)
			continue
		}

		// Initialize epochs inputs map
		var epochInputMap = make(map[*Epoch][]Input)

		// Index Inputs into epochs
		for _, input := range inputs {

			inputEpochIndex := CalculateEpochIndex(epochLength, input.BlockNumber)

			// If input belongs into a new epoch, close the previous known one
			if currentEpoch != nil && currentEpoch.Index != inputEpochIndex {
				currentEpoch.Status = EpochStatusClosed
				slog.Info("evmreader: Closing epoch",
					"app", currentEpoch.AppAddress,
					"epoch_index", currentEpoch.Index,
					"start", currentEpoch.FirstBlock,
					"end", currentEpoch.LastBlock)
				currentEpoch = nil
			}
			if currentEpoch == nil {
				currentEpoch = &Epoch{
					Index:      inputEpochIndex,
					FirstBlock: inputEpochIndex * epochLength,
					LastBlock:  (inputEpochIndex * epochLength) + epochLength - 1,
					Status:     EpochStatusOpen,
					AppAddress: address,
				}
				epochInputMap[currentEpoch] = []Input{}
			}

			slog.Info("evmreader: Found new Input",
				"app", address,
				"index", input.Index,
				"block", input.BlockNumber,
				"epoch_index", inputEpochIndex)

			currentInputs, ok := epochInputMap[currentEpoch]
			if !ok {
				currentInputs = []Input{}
			}
			// overriding input index
			combinedIndex, err := r.repository.GetInputIndex(ctx, address)
			if err != nil {
				slog.Error("evmreader: failed to read index", "app", address, "error", err)
			}
			if combinedIndex != input.Index && r.shouldModifyIndex == true {
				slog.Info("evmreader: Overriding input index", "onchain-index", input.Index, "new-index", combinedIndex)
				input.Index = combinedIndex
				modifiedRawData, err := r.modifyIndexInRaw(ctx, input.RawData, input.AppAddress, combinedIndex)
				if err == nil {
					input.RawData = modifiedRawData
				}
			}
			err = r.repository.UpdateInputIndex(ctx, address)
			if err != nil {
				slog.Error("evmreader: failed to update index", "app", address, "error", err)
			}
			epochInputMap[currentEpoch] = append(currentInputs, *input)

		}

		// Indexed all inputs. Check if it is time to close this epoch
		if currentEpoch != nil && endBlock >= currentEpoch.LastBlock {
			currentEpoch.Status = EpochStatusClosed
			slog.Info("evmreader: Closing epoch",
				"app", currentEpoch.AppAddress,
				"epoch_index", currentEpoch.Index,
				"start", currentEpoch.FirstBlock,
				"end", currentEpoch.LastBlock)
			// Add to inputMap so it is stored
			_, ok := epochInputMap[currentEpoch]
			if !ok {
				epochInputMap[currentEpoch] = []Input{}
			}
		}

		_, _, err = r.repository.StoreEpochAndInputsTransaction(
			ctx,
			epochInputMap,
			endBlock,
			address,
		)
		if err != nil {
			slog.Error("evmreader: Error storing inputs and epochs",
				"app", address,
				"error", err,
			)
			continue
		}

		// Store everything
		if len(epochInputMap) > 0 {

			slog.Debug("evmreader: Inputs and epochs stored successfully",
				"app", address,
				"start-block", startBlock,
				"end-block", endBlock,
				"total epochs", len(epochInputMap),
				"total inputs", len(inputs),
			)
		} else {
			slog.Debug("evmreader: No inputs or epochs to store")
		}

	}

	return nil
}

// AddAppEpochLengthIntoCache checks the epoch length cache and read epoch length from IConsensus
// contract and add it to the cache if needed
func (r *EvmReader) AddAppEpochLengthIntoCache(app application) error {

	epochLength, ok := r.epochLengthCache[app.ContractAddress]
	if !ok {

		epochLength, err := getEpochLength(app.ConsensusContract)
		if err != nil {
			return errors.Join(
				fmt.Errorf("error retrieving epoch length from contracts for app %s",
					app.ContractAddress),
				err)
		}
		r.epochLengthCache[app.ContractAddress] = epochLength
		slog.Info("evmreader: Got epoch length from IConsensus",
			"app", app.ContractAddress,
			"epoch length", epochLength)
	} else {
		slog.Debug("evmreader: Got epoch length from cache",
			"app", app.ContractAddress,
			"epoch length", epochLength)
	}

	return nil
}

// readInputsFromBlockchain read the inputs from the blockchain ordered by Input index
func (r *EvmReader) readInputsFromBlockchain(
	ctx context.Context,
	appsAddresses []Address,
	startBlock, endBlock uint64,
) (map[Address][]*Input, error) {

	// Initialize app input map
	var appInputsMap = make(map[Address][]*Input)
	for _, appsAddress := range appsAddresses {
		appInputsMap[appsAddress] = []*Input{}
	}

	opts := bind.FilterOpts{
		Context: ctx,
		Start:   startBlock,
		End:     &endBlock,
	}
	inputsEvents, err := r.inputSource.RetrieveInputs(&opts, appsAddresses, nil)
	if err != nil {
		return nil, err
	}

	// Order inputs as order is not enforced by RetrieveInputs method nor the APIs
	for _, event := range inputsEvents {
		slog.Debug("evmreader: Received input",
			"app", event.AppContract,
			"index", event.Index,
			"block", event.Raw.BlockNumber)
		input := &Input{
			Index:            event.Index.Uint64(),
			CompletionStatus: InputStatusNone,
			RawData:          event.Input,
			BlockNumber:      event.Raw.BlockNumber,
			AppAddress:       event.AppContract,
			TransactionId:    event.Index.Bytes(),
		}

		// Insert Sorted
		appInputsMap[event.AppContract] = insertSorted(
			sortByInputIndex, appInputsMap[event.AppContract], input)
	}
	return appInputsMap, nil
}

// byLastProcessedBlock key extractor function intended to be used with `indexApps` function
func byLastProcessedBlock(app application) uint64 {
	return app.LastProcessedBlock
}

// getEpochLength reads the application epoch length given it's consensus contract
func getEpochLength(consensus ConsensusContract) (uint64, error) {

	epochLengthRaw, err := consensus.GetEpochLength(nil)
	if err != nil {
		return 0, errors.Join(
			fmt.Errorf("error retrieving application epoch length"),
			err,
		)
	}

	return epochLengthRaw.Uint64(), nil
}

func (r *EvmReader) modifyIndexInRaw(ctx context.Context, rawData []byte, appAddress common.Address, currentIndex uint64) ([]byte, error) {
	// load contract ABI
	abiObject := r.IOAbi
	values, err := abiObject.Methods["EvmAdvance"].Inputs.Unpack(rawData[4:])
	if err != nil {
		slog.Error("Error unpacking abi", "err", err)
		return []byte{}, err
	}

	type EvmAdvance struct {
		ChainId        *big.Int
		AppContract    common.Address
		MsgSender      common.Address
		BlockNumber    *big.Int
		BlockTimestamp *big.Int
		PrevRandao     *big.Int
		Index          *big.Int
		Payload        []byte
	}

	data := EvmAdvance{
		ChainId:        values[0].(*big.Int),
		AppContract:    values[1].(common.Address),
		MsgSender:      values[2].(common.Address),
		BlockNumber:    values[3].(*big.Int),
		BlockTimestamp: values[4].(*big.Int),
		PrevRandao:     values[5].(*big.Int),
		Index:          values[6].(*big.Int),
		Payload:        values[7].([]byte),
	}

	// modify index
	data.Index = big.NewInt(int64(currentIndex))

	// abi encode again
	dataAbi, err := abiObject.Pack("EvmAdvance", data.ChainId, data.AppContract, data.MsgSender, data.BlockNumber, data.BlockTimestamp, data.PrevRandao, data.Index, data.Payload)
	if err != nil {
		slog.Error("failed to abi encode", "error", err)
		return []byte{}, err
	}

	return dataAbi, nil
}
