// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import {IContinuousClearingAuction} from 'continuous-clearing-auction/interfaces/IContinuousClearingAuction.sol';
import {Script} from 'forge-std/Script.sol';
import {console2} from 'forge-std/console2.sol';

/// @title SubmitBid
/// @notice Submits a bid on a CCA auction contract, triggering CheckpointUpdated.
///         Expects AUCTION_ADDRESS env var to be set.
///         The auction must use currency = address(0) (ETH).
///
///         Usage:
///           AUCTION_ADDRESS=0x... forge script script/SubmitBid.s.sol:SubmitBid \
///             --rpc-url http://127.0.0.1:8545 \
///             --private-key 0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80 \
///             --broadcast
contract SubmitBid is Script {
    function run() public {
        address auctionAddr = vm.envAddress("AUCTION_ADDRESS");
        IContinuousClearingAuction auction = IContinuousClearingAuction(auctionAddr);

        // Read the floor price so we bid just above it.
        uint256 floorPrice = auction.clearingPrice();
        uint256 tickSpacing = auction.tickSpacing();
        uint256 bidPrice = floorPrice + tickSpacing;

        // Bid a moderate amount of ETH (1 ETH worth).
        uint128 bidAmount = 1 ether;

        console2.log('Auction:', auctionAddr);
        console2.log('Floor price:', floorPrice);
        console2.log('Tick spacing:', tickSpacing);
        console2.log('Bid price:', bidPrice);
        console2.log('Bid amount:', bidAmount);

        vm.startBroadcast();

        // submitBid(maxPrice, amount, owner, hookData) — 4-arg overload, no prevTickPrice needed
        // currency = address(0) means ETH, so we send msg.value = bidAmount
        uint256 bidId = auction.submitBid{value: bidAmount}(bidPrice, bidAmount, msg.sender, bytes(""));

        console2.log('Bid submitted, ID:', bidId);

        vm.stopBroadcast();
    }
}
