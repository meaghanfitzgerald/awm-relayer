// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// SPDX-License-Identifier: Ecosystem

pragma solidity ^0.8.18;

import "https://github.com/ava-labs/teleporter/blob/main/contracts/src/Teleporter/ITeleporterMessenger.sol";

/**
 * @dev ExampleCrossChainMessenger is an example contract that demonstrates how to send a messages cross chain.
 */
contract SenderOnC {
    ITeleporterMessenger public immutable teleporterMessenger =
        ITeleporterMessenger(0xF7cBd95f1355f0d8d659864b92e2e9fbfaB786f7);

    /**
     * @dev Sends a message to another chain.
     */
    function sendMessage(
        address destinationAddress,
        string calldata message,
        address[] calldata allowedRelayerAddresses
    ) external {
        teleporterMessenger.sendCrossChainMessage(
            TeleporterMessageInput({
                destinationBlockchainID: 0x9f3be606497285d0ffbb5ac9ba24aa60346a9b1812479ed66cb329f394a4b1c7,
                destinationAddress: destinationAddress,
                feeInfo: TeleporterFeeInfo({
                    feeTokenAddress: address(0),
                    amount: 0
                }),
                requiredGasLimit: 100000,
                allowedRelayerAddresses: allowedRelayerAddresses,
                message: abi.encode(message)
            })
        );
    }
}
