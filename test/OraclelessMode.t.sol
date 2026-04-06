// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import {Test} from "forge-std/Test.sol";
import {DeFiInteractorModule} from "../src/DeFiInteractorModule.sol";
import {MockSafe} from "./mocks/MockSafe.sol";
import {MockERC20} from "./mocks/MockERC20.sol";
import {MockProtocol} from "./mocks/MockProtocol.sol";
import {MockChainlinkPriceFeed} from "./mocks/MockChainlinkPriceFeed.sol";
import {MockParser} from "./mocks/MockParser.sol";
import {Clones} from "@openzeppelin/contracts/proxy/Clones.sol";

/**
 * @title OraclelessModeTest
 * @notice Tests for oracleless mode (authorizedOracle == address(0))
 */
contract OraclelessModeTest is Test {
    DeFiInteractorModule public module;
    MockSafe public safe;
    MockERC20 public token;
    MockProtocol public protocol;
    MockChainlinkPriceFeed public priceFeed;
    MockParser public parser;

    address public owner;
    address public agent;

    bytes4 constant DEPOSIT_SELECTOR = bytes4(keccak256("deposit(uint256,address)"));
    bytes4 constant WITHDRAW_SELECTOR = bytes4(keccak256("withdraw(uint256,address)"));
    bytes4 constant SWAP_SELECTOR = bytes4(keccak256("swap(uint256,address)"));
    bytes4 constant APPROVE_SELECTOR = bytes4(keccak256("approve(address,uint256)"));

    function setUp() public {
        owner = address(this);
        agent = makeAddr("agent");

        // Deploy mock Safe
        address[] memory owners = new address[](1);
        owners[0] = owner;
        safe = new MockSafe(owners, 1);

        // Deploy module in oracleless mode (oracle = address(0))
        module = new DeFiInteractorModule(address(safe), owner, address(0));

        // Deploy mocks
        token = new MockERC20();
        protocol = new MockProtocol();
        priceFeed = new MockChainlinkPriceFeed(1_00000000, 8); // $1.00
        parser = new MockParser(address(token));

        // Enable module on Safe
        safe.enableModule(address(module));

        // Transfer tokens to Safe
        token.transfer(address(safe), 100000 * 10 ** 18);

        // Configure module (no oracle needed)
        module.setTokenPriceFeed(address(token), address(priceFeed));
        module.registerSelector(DEPOSIT_SELECTOR, DeFiInteractorModule.OperationType.DEPOSIT);
        module.registerSelector(WITHDRAW_SELECTOR, DeFiInteractorModule.OperationType.WITHDRAW);
        module.registerSelector(SWAP_SELECTOR, DeFiInteractorModule.OperationType.SWAP);
        module.registerSelector(APPROVE_SELECTOR, DeFiInteractorModule.OperationType.APPROVE);
        module.registerParser(address(protocol), address(parser));
    }

    function _setupAgent() internal {
        module.grantRole(agent, module.DEFI_EXECUTE_ROLE());
        address[] memory targets = new address[](2);
        targets[0] = address(protocol);
        targets[1] = address(token);
        module.setAllowedAddresses(agent, targets, true);
        // Oracleless requires USD mode
        module.setSubAccountLimits(agent, 0, 1000e18, 1 days); // $1000/day
    }

    // ============ Deployment ============

    function testIsOracleless() public view {
        assertTrue(module.isOracleless());
        assertEq(module.authorizedOracle(), address(0));
    }

    function testDeployWithOracle() public {
        DeFiInteractorModule m2 = new DeFiInteractorModule(address(safe), owner, makeAddr("oracle"));
        assertFalse(m2.isOracleless());
    }

    // ============ Oracleless requires USD mode ============

    function testSetSubAccountLimitsRevertsBPSInOracleless() public {
        module.grantRole(agent, module.DEFI_EXECUTE_ROLE());
        vm.expectRevert(DeFiInteractorModule.OraclelessRequiresUSDMode.selector);
        module.setSubAccountLimits(agent, 500, 0, 1 days); // BPS mode should revert
    }

    function testSetSubAccountLimitsUSDModeSucceeds() public {
        module.grantRole(agent, module.DEFI_EXECUTE_ROLE());
        module.setSubAccountLimits(agent, 0, 5000e18, 1 days);
        (uint256 bps, uint256 usd, uint256 window) = module.getSubAccountLimits(agent);
        assertEq(bps, 0);
        assertEq(usd, 5000e18);
        assertEq(window, 1 days);
    }

    // ============ Execute operations without oracle ============

    function testExecuteDepositSucceeds() public {
        _setupAgent();
        bytes memory data = abi.encodeWithSelector(DEPOSIT_SELECTOR, 100e18, address(safe));
        vm.prank(agent);
        module.executeOnProtocol(address(protocol), data);

        // Spending tracked
        assertEq(module.cumulativeSpent(agent), 100e18);
    }

    function testExecuteDepositNoOracleUpdateNeeded() public {
        _setupAgent();
        // No oracle update was ever sent — in normal mode this would revert with StaleOracleData
        // In oracleless mode it succeeds
        bytes memory data = abi.encodeWithSelector(DEPOSIT_SELECTOR, 50e18, address(safe));
        vm.prank(agent);
        module.executeOnProtocol(address(protocol), data);
        assertEq(module.cumulativeSpent(agent), 50e18);
    }

    function testExecuteRevertsWhenExceedingUSDLimit() public {
        _setupAgent(); // $1000/day limit

        // First deposit: $900 — should succeed
        bytes memory data1 = abi.encodeWithSelector(DEPOSIT_SELECTOR, 900e18, address(safe));
        vm.prank(agent);
        module.executeOnProtocol(address(protocol), data1);

        // Second deposit: $200 — total $1100, exceeds $1000 limit
        bytes memory data2 = abi.encodeWithSelector(DEPOSIT_SELECTOR, 200e18, address(safe));
        vm.prank(agent);
        vm.expectRevert();
        module.executeOnProtocol(address(protocol), data2);
    }

    // ============ Transfer operations ============

    function testTransferSucceeds() public {
        _setupAgent();
        module.grantRole(agent, module.DEFI_TRANSFER_ROLE());
        address recipient = makeAddr("recipient");

        vm.prank(agent);
        module.transferToken(address(token), recipient, 100e18);

        assertEq(module.cumulativeSpent(agent), 100e18);
    }

    // ============ Tier 1 acquired balance (on-chain swap marking) ============

    function testSwapMarksOutputAsAcquired() public {
        _setupAgent();

        // Create a second token for swap output
        MockERC20 tokenOut = new MockERC20();
        tokenOut.transfer(address(safe), 1000e18);

        // Create parser that returns tokenOut
        MockParser swapParser = new MockParser(address(token));

        // Register swap on protocol
        module.registerParser(address(protocol), address(swapParser));

        // Execute swap (spending cost = 100 tokens @ $1 = $100)
        bytes memory data = abi.encodeWithSelector(SWAP_SELECTOR, 100e18, address(safe));
        vm.prank(agent);
        module.executeOnProtocol(address(protocol), data);

        // Spending tracked
        assertEq(module.cumulativeSpent(agent), 100e18);
    }

    function testAcquiredTokensAreFreeToUse() public {
        // Deploy a module WITH oracle so we can set up acquired balance, then switch to oracleless
        address tempOracle = makeAddr("tempOracle");
        DeFiInteractorModule m2 = new DeFiInteractorModule(address(safe), owner, tempOracle);
        safe.enableModule(address(m2));
        m2.setTokenPriceFeed(address(token), address(priceFeed));
        m2.registerSelector(DEPOSIT_SELECTOR, DeFiInteractorModule.OperationType.DEPOSIT);
        m2.registerParser(address(protocol), address(parser));

        // Configure agent
        m2.grantRole(agent, m2.DEFI_EXECUTE_ROLE());
        address[] memory targets = new address[](2);
        targets[0] = address(protocol);
        targets[1] = address(token);
        m2.setAllowedAddresses(agent, targets, true);
        m2.setSubAccountLimits(agent, 0, 1000e18, 1 days);

        // Set safe value + spending allowance + acquired balance via oracle
        vm.startPrank(tempOracle);
        m2.updateSafeValue(1_000_000e18);
        m2.updateSpendingAllowance(agent, 0, 1000e18);
        m2.updateAcquiredBalance(agent, address(token), 0, 500e18);
        vm.stopPrank();

        // Switch to oracleless
        m2.setAuthorizedOracle(address(0));
        assertTrue(m2.isOracleless());

        // Deposit 500 tokens — all acquired, so $0 spending cost
        bytes memory data = abi.encodeWithSelector(DEPOSIT_SELECTOR, 500e18, address(safe));
        vm.prank(agent);
        m2.executeOnProtocol(address(protocol), data);

        // No spending cost because all tokens were acquired
        assertEq(m2.cumulativeSpent(agent), 0);
    }

    // ============ Window reset ============

    function testWindowResetsAfterDuration() public {
        _setupAgent(); // $1000/day

        // Spend $900
        bytes memory data = abi.encodeWithSelector(DEPOSIT_SELECTOR, 900e18, address(safe));
        vm.prank(agent);
        module.executeOnProtocol(address(protocol), data);
        assertEq(module.cumulativeSpent(agent), 900e18);

        // Advance time past window
        vm.warp(block.timestamp + 1 days + 1);
        // Refresh price feed so it's not stale after warp
        priceFeed.setPrice(1_00000000);

        // Spend again — window resets, cumulative starts fresh
        bytes memory data2 = abi.encodeWithSelector(DEPOSIT_SELECTOR, 800e18, address(safe));
        vm.prank(agent);
        module.executeOnProtocol(address(protocol), data2);
        assertEq(module.cumulativeSpent(agent), 800e18);
    }

    // ============ Switch to/from oracleless mode ============

    function testSwitchToOraclelessMode() public {
        // Deploy with oracle
        DeFiInteractorModule m2 = new DeFiInteractorModule(address(safe), owner, owner);
        assertFalse(m2.isOracleless());

        // Switch to oracleless
        m2.setAuthorizedOracle(address(0));
        assertTrue(m2.isOracleless());
    }

    function testSwitchFromOraclelessToOracle() public {
        assertTrue(module.isOracleless());

        address newOracle = makeAddr("newOracle");
        module.setAuthorizedOracle(newOracle);
        assertFalse(module.isOracleless());
        assertEq(module.authorizedOracle(), newOracle);
    }

    // ============ Withdraw is free ============

    function testWithdrawIsFreeInOracleless() public {
        _setupAgent();

        bytes memory data = abi.encodeWithSelector(WITHDRAW_SELECTOR, 500e18, address(safe));
        vm.prank(agent);
        module.executeOnProtocol(address(protocol), data);

        // Withdraw has no spending cost
        assertEq(module.cumulativeSpent(agent), 0);
    }

    // ============ Approve cap uses cumulative budget ============

    function testApproveCappedByCumulativeBudget() public {
        _setupAgent(); // $1000/day

        // Approve $500 worth of tokens — should succeed
        bytes memory data = abi.encodeWithSelector(APPROVE_SELECTOR, address(protocol), 500e18);
        vm.prank(agent);
        module.executeOnProtocol(address(token), data);
    }

    function testApproveRevertsWhenExceedingBudget() public {
        _setupAgent(); // $1000/day

        // Approve $1500 worth of tokens — exceeds $1000 budget
        bytes memory data = abi.encodeWithSelector(APPROVE_SELECTOR, address(protocol), 1500e18);
        vm.prank(agent);
        vm.expectRevert(DeFiInteractorModule.ApprovalExceedsLimit.selector);
        module.executeOnProtocol(address(token), data);
    }

    // ============ WITHDRAW does NOT auto-mark acquired in oracleless ============

    function testWithdrawOutputNotMarkedAcquiredInOracleless() public {
        _setupAgent();

        // First deposit $500 (costs spending)
        bytes memory depositData = abi.encodeWithSelector(DEPOSIT_SELECTOR, 500e18, address(safe));
        vm.prank(agent);
        module.executeOnProtocol(address(protocol), depositData);
        assertEq(module.cumulativeSpent(agent), 500e18);

        // Withdraw — output is NOT marked acquired in oracleless (same as oracle mode)
        // This prevents the deposit-withdraw "laundering" cycle
        bytes memory withdrawData = abi.encodeWithSelector(WITHDRAW_SELECTOR, 500e18, address(safe));
        vm.prank(agent);
        module.executeOnProtocol(address(protocol), withdrawData);

        // Withdraw is free (no spending cost) but tokens are NOT acquired
        assertEq(module.cumulativeSpent(agent), 500e18);
        assertEq(module.acquiredBalance(agent, address(token)), 0);
    }

    // ============ Fix #3: Approve cap handles expired window ============

    function testApproveCapResetsOnExpiredWindow() public {
        _setupAgent(); // $1000/day

        // Spend $900 in window 1
        bytes memory data = abi.encodeWithSelector(DEPOSIT_SELECTOR, 900e18, address(safe));
        vm.prank(agent);
        module.executeOnProtocol(address(protocol), data);

        // Advance past window
        vm.warp(block.timestamp + 1 days + 1);
        priceFeed.setPrice(1_00000000);

        // Approve $800 — should succeed because window expired (fresh $1000 budget)
        bytes memory approveData = abi.encodeWithSelector(APPROVE_SELECTOR, address(protocol), 800e18);
        vm.prank(agent);
        module.executeOnProtocol(address(token), approveData);
    }

    // ============ Fix #5: setAuthorizedOracle rejects BPS sub-accounts ============

    function testSwitchToOraclelessRevertsWithBPSSubAccounts() public {
        // Deploy with oracle
        DeFiInteractorModule m2 = new DeFiInteractorModule(address(safe), owner, makeAddr("oracle2"));

        // Configure sub-account with BPS limits
        address agent2 = makeAddr("agent2");
        m2.grantRole(agent2, m2.DEFI_EXECUTE_ROLE());
        m2.setSubAccountLimits(agent2, 500, 0, 1 days); // BPS mode

        // Switching to oracleless should revert
        vm.expectRevert(abi.encodeWithSelector(DeFiInteractorModule.SubAccountHasBPSLimits.selector, agent2));
        m2.setAuthorizedOracle(address(0));
    }

    function testSwitchToOraclelessSucceedsWithUSDSubAccounts() public {
        // Deploy with oracle
        DeFiInteractorModule m2 = new DeFiInteractorModule(address(safe), owner, makeAddr("oracle3"));

        // Configure sub-account with USD limits
        address agent2 = makeAddr("agent2");
        m2.grantRole(agent2, m2.DEFI_EXECUTE_ROLE());
        m2.setSubAccountLimits(agent2, 0, 1000e18, 1 days); // USD mode

        // Switching to oracleless should succeed
        m2.setAuthorizedOracle(address(0));
        assertTrue(m2.isOracleless());
    }

    // ============ Fix #5b: Unconfigured sub-accounts also blocked ============

    function testSwitchToOraclelessRevertsWithUnconfiguredSubAccounts() public {
        // Deploy with oracle
        DeFiInteractorModule m2 = new DeFiInteractorModule(address(safe), owner, makeAddr("oracle4"));

        // Grant role but do NOT configure limits (defaults to BPS mode)
        address agent2 = makeAddr("agent2");
        m2.grantRole(agent2, m2.DEFI_EXECUTE_ROLE());

        // Switching to oracleless should revert (unconfigured = default BPS)
        vm.expectRevert(abi.encodeWithSelector(DeFiInteractorModule.SubAccountHasBPSLimits.selector, agent2));
        m2.setAuthorizedOracle(address(0));
    }

    // ============ Clone initialization defaults ============

    function testCloneInitializationSetsDefaults() public {
        // Deploy implementation
        DeFiInteractorModule impl = new DeFiInteractorModule(address(safe), owner, owner);

        // Clone it (simulating what the factory does)
        address cloneAddr = Clones.cloneDeterministic(address(impl), bytes32(uint256(42)));
        DeFiInteractorModule clone = DeFiInteractorModule(cloneAddr);
        clone.initialize(address(safe), owner, address(0)); // oracleless clone

        // Verify defaults are set correctly
        assertEq(clone.maxOracleAge(), 60 minutes);
        assertEq(clone.maxSafeValueAge(), 60 minutes);
        assertEq(clone.maxPriceFeedAge(), 24 hours);
        assertEq(clone.absoluteMaxSpendingBps(), 2000);
        assertEq(clone.maxOracleAcquiredBps(), 2000);
    }

    // ============ Staleness setters ============

    function testSetMaxOracleAge() public {
        module.setMaxOracleAge(30 minutes);
        assertEq(module.maxOracleAge(), 30 minutes);
    }

    function testSetMaxSafeValueAge() public {
        module.setMaxSafeValueAge(2 hours);
        assertEq(module.maxSafeValueAge(), 2 hours);
    }

    function testSetMaxPriceFeedAge() public {
        module.setMaxPriceFeedAge(12 hours);
        assertEq(module.maxPriceFeedAge(), 12 hours);
    }
}
