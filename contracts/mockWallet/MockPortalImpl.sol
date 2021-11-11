// Copyright 2021 Cartesi Pte. Ltd.

// SPDX-License-Identifier: Apache-2.0
// Licensed under the Apache License, Version 2.0 (the "License"); you may not use
// this file except in compliance with the License. You may obtain a copy of the
// License at http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software distributed
// under the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR
// CONDITIONS OF ANY KIND, either express or implied. See the License for the
// specific language governing permissions and limitations under the License.

/// @title Validator Manager
pragma solidity ^0.8.0;

import "@openzeppelin/contracts/token/ERC20/IERC20.sol";

import "./MockPortal.sol";
import "./MockInput.sol";

contract MockPortalImpl is MockPortal {
    address immutable voucherContract;
    MockInput immutable inputContract;
    bool lock;

    modifier onlyVoucherContract {
        require(msg.sender == voucherContract, "msg.sender != voucherContract");
        _;
    }

    /// TODO: this is also defined on InputImpl
    /// @notice functions modified by noReentrancy are not subject to recursion
    modifier noReentrancy() {
        require(!lock, "reentrancy not allowed");
        lock = true;
        _;
        lock = false;
    }

    constructor(address _inputContract, address _voucherContract) {
        inputContract = MockInput(_inputContract);
        voucherContract = _voucherContract;
    }

    /// @notice deposits ether in portal contract and create ether in L2
    /// @param _L2receivers array with receivers addresses
    /// @param _amounts array of amounts of ether to be distributed
    /// @param _data information to be interpreted by L2
    /// @return hash of input generated by deposit
    /// @dev  receivers[i] receive amounts[i]
    function etherDeposit(
        address[] calldata _L2receivers,
        uint256[] calldata _amounts,
        bytes calldata _data
    ) public payable override noReentrancy() returns (bytes32) {
        require(
            _L2receivers.length == _amounts.length,
            "receivers array length != amounts array length"
        );

        uint256 totalAmount;

        for (uint256 i = 0; i < _amounts.length; i++) {
            totalAmount = totalAmount + _amounts[i];
        }
        require(msg.value >= totalAmount, "msg.value < totalAmount");

        bytes memory input =
            abi.encode(
                Operation.EtherOp,
                Transaction.Deposit,
                _L2receivers,
                _amounts,
                _data
            );

        emit EtherDeposited(_L2receivers, _amounts, _data);
        return inputContract.addInput(input, uint256(Operation.EtherOp));
    }

    /// @notice deposits ERC20 in portal contract and create tokens in L2
    /// @param _ERC20 address of ERC20 token to be deposited
    /// @param _L1Sender address on L1 that authorized the transaction
    /// @param _L2receivers array with receivers addresses
    /// @param _amounts array of amounts of ether to be distributed
    /// @param _data information to be interpreted by L2
    /// @return hash of input generated by deposit
    /// @dev  receivers[i] receive amounts[i]
    function erc20Deposit(
        address _ERC20,
        address _L1Sender,
        address[] calldata _L2receivers,
        uint256[] calldata _amounts,
        bytes calldata _data
    ) public override noReentrancy() returns (bytes32) {
        require(
            _L2receivers.length == _amounts.length,
            "receivers array length != amounts array length"
        );

        uint256 totalAmount;

        for (uint256 i = 0; i < _amounts.length; i++) {
            totalAmount = totalAmount + _amounts[i];
        }

        IERC20 token = IERC20(_ERC20);

        require(
            token.transferFrom(_L1Sender, address(this), totalAmount),
            "erc20 transferFrom failed"
        );

        bytes memory input =
            abi.encode(
                Operation.ERC20Op,
                Transaction.Deposit,
                _L2receivers,
                _amounts,
                address(token),
                _data
            );

        emit ERC20Deposited(_ERC20, _L1Sender, _L2receivers, _amounts, _data);
        return inputContract.addInput(input, uint256(Operation.ERC20Op));
    }

    /// @notice executes a rollups voucher
    /// @param _data data with information necessary to execute voucher
    /// @return status of voucher execution
    /// @dev can only be called by Voucher contract
    function executeRollupsVoucher(bytes calldata _data)
        public
        override
        onlyVoucherContract
        returns (bool)
    {
        // TODO: should use assembly to figure out where the first
        //       relevant word of _data begins and figure out the type
        //       of Operation. That way we don't have to encode wasteful
        //       information on data (i.e tokenAddr for ether transfer)
        (
            Operation op,
            address tokenAddr,
            address payable receiver,
            uint256 value
        ) = abi.decode(_data, (Operation, address, address, uint256));

        if (op == Operation.EtherOp) {
            return etherWithdrawal(receiver, value);
        }

        if (op == Operation.ERC20Op) {
            return erc20Withdrawal(tokenAddr, receiver, value);
        }

        // Operation is not supported
        return false;
    }

    /// @notice withdrawal ether
    /// @param _receiver array with receivers addresses
    /// @param _amount array of amounts of ether to be distributed
    /// @return status of withdrawal
    function etherWithdrawal(address payable _receiver, uint256 _amount)
        internal
        onlyVoucherContract
        returns (bool)
    {
        // transfer reverts on failure
        _receiver.transfer(_amount);

        emit EtherWithdrawn(_receiver, _amount);
        return true;
    }

    /// @notice withdrawal ERC20
    /// @param _ERC20 address of ERC20 token to be deposited
    /// @param _receiver array with receivers addresses
    /// @param _amount array of amounts of ether to be distributed
    /// @return status of withdrawal
    function erc20Withdrawal(
        address _ERC20,
        address payable _receiver,
        uint256 _amount
    ) internal onlyVoucherContract returns (bool) {
        IERC20 token = IERC20(_ERC20);

        // transfer reverts on failure
        token.transfer(_receiver, _amount);

        emit ERC20Withdrawn(_ERC20, _receiver, _amount);
        return true;
    }
}
