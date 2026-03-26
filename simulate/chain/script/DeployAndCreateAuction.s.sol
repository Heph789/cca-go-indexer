// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import {ContinuousClearingAuctionFactory} from 'continuous-clearing-auction/ContinuousClearingAuctionFactory.sol';
import {IContinuousClearingAuctionFactory} from 'continuous-clearing-auction/interfaces/IContinuousClearingAuctionFactory.sol';
import {AuctionParameters} from 'continuous-clearing-auction/interfaces/IContinuousClearingAuction.sol';
import {AuctionStepsBuilder} from 'test/utils/AuctionStepsBuilder.sol';
import {Script} from 'forge-std/Script.sol';
import {console2} from 'forge-std/console2.sol';

import {ERC20Mock} from './ERC20Mock.sol';

/// @title DeployAndCreateAuction
/// @notice Deploys the CCA factory on a local Anvil chain and creates a test auction.
///         This emits an AuctionCreated event that the Go indexer can pick up.
contract DeployAndCreateAuction is Script {
    function run() public {
        vm.startBroadcast();

        // --- Step 1: Deploy the factory ---
        ContinuousClearingAuctionFactory factory = new ContinuousClearingAuctionFactory();
        console2.log('Factory deployed to:', address(factory));

        // --- Step 2: Deploy a mock ERC20 token for the auction ---
        ERC20Mock token = new ERC20Mock('Test Token', 'TEST', 18);
        uint128 totalSupply = 1_000_000e18;
        token.mint(msg.sender, totalSupply);
        console2.log('Token deployed to:', address(token));

        // --- Step 3: Approve the factory to spend tokens ---
        token.approve(address(factory), totalSupply);

        // --- Step 4: Build auction parameters ---
        // Use simple defaults suitable for local testing.
        // startBlock = current block + 1, endBlock = startBlock + 100, claimBlock = endBlock + 10
        uint64 startBlock = uint64(block.number + 1);
        uint64 endBlock = startBlock + 100;
        uint64 claimBlock = endBlock + 10;

        bytes memory auctionStepsData = AuctionStepsBuilder.init();
        // Single step: 100% of supply (1_000_000 mps = 100%), over the full auction duration
        auctionStepsData = AuctionStepsBuilder.addStep(auctionStepsData, 1_000_000, uint40(endBlock - startBlock));

        AuctionParameters memory params = AuctionParameters({
            currency: address(0), // ETH
            tokensRecipient: msg.sender,
            fundsRecipient: msg.sender,
            startBlock: startBlock,
            endBlock: endBlock,
            claimBlock: claimBlock,
            tickSpacing: 1,
            validationHook: address(0),
            floorPrice: 1e15, // 0.001 ETH per token
            requiredCurrencyRaised: 0,
            auctionStepsData: auctionStepsData
        });

        // --- Step 5: Create the auction (emits AuctionCreated) ---
        bytes memory configData = abi.encode(params);
        address auction = address(
            factory.initializeDistribution(address(token), totalSupply, configData, bytes32(0))
        );
        console2.log('Auction created at:', auction);

        vm.stopBroadcast();
    }
}
