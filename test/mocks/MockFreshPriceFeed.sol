// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/**
 * @title AggregatorV3Interface
 * @notice Chainlink price feed interface
 */
interface AggregatorV3Interface {
    function decimals() external view returns (uint8);
    function description() external view returns (string memory);
    function version() external view returns (uint256);
    function getRoundData(uint80 _roundId)
        external
        view
        returns (uint80 roundId, int256 answer, uint256 startedAt, uint256 updatedAt, uint80 answeredInRound);
    function latestRoundData()
        external
        view
        returns (uint80 roundId, int256 answer, uint256 startedAt, uint256 updatedAt, uint80 answeredInRound);
}

/**
 * @title MockFreshPriceFeed
 * @notice Wraps a Chainlink price feed and returns fresh timestamps
 * @dev Returns the real price from the underlying feed but with updatedAt = block.timestamp - 10
 *      This is useful for testing on testnets where Chainlink feeds may be stale
 */
contract MockFreshPriceFeed is AggregatorV3Interface {
    AggregatorV3Interface public immutable underlyingFeed;

    constructor(address _underlyingFeed) {
        underlyingFeed = AggregatorV3Interface(_underlyingFeed);
    }

    function decimals() external view override returns (uint8) {
        return underlyingFeed.decimals();
    }

    function description() external view override returns (string memory) {
        return string(abi.encodePacked("Mock Fresh: ", underlyingFeed.description()));
    }

    function version() external view override returns (uint256) {
        return underlyingFeed.version();
    }

    function getRoundData(uint80 _roundId)
        external
        view
        override
        returns (uint80 roundId, int256 answer, uint256 startedAt, uint256 updatedAt, uint80 answeredInRound)
    {
        (, answer,,,) = underlyingFeed.getRoundData(_roundId);
        // Return roundId=1 and answeredInRound=1 (must be >= roundId to pass staleness check)
        return (1, answer, 0, block.timestamp - 10, 1);
    }

    function latestRoundData()
        external
        view
        override
        returns (uint80 roundId, int256 answer, uint256 startedAt, uint256 updatedAt, uint80 answeredInRound)
    {
        (, answer,,,) = underlyingFeed.latestRoundData();
        // Return roundId=1 and answeredInRound=1 (must be >= roundId to pass staleness check)
        return (1, answer, 0, block.timestamp - 10, 1);
    }
}
